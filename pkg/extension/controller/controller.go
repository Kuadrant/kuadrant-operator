package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"k8s.io/utils/env"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntimectrl "sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlruntimeevent "sigs.k8s.io/controller-runtime/pkg/event"
	ctrlruntimehandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	ctrlruntimesrc "sigs.k8s.io/controller-runtime/pkg/source"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	extutils "github.com/kuadrant/kuadrant-operator/pkg/extension/utils"
)

var (
	logLevel, _ = zapcore.ParseLevel(env.GetString("LOG_LEVEL", "info"))
	logMode     = env.GetString("LOG_MODE", "production") != "production"
)

type ExtensionController struct {
	name            string
	logger          logr.Logger
	manager         ctrlruntime.Manager
	client          *dynamic.DynamicClient
	reconcile       exttypes.ReconcileFn
	watchSources    []ctrlruntimesrc.Source
	extensionClient *extensionClient
}

func (ec *ExtensionController) Start(ctx context.Context) error {
	stopCh := make(chan struct{})
	// todo(adam-cattermole): how big do we make the reconcile event channel?
	//	 how many should we queue before we block?
	reconcileChan := make(chan ctrlruntimeevent.GenericEvent, 50)
	ec.watchSources = append(ec.watchSources, ctrlruntimesrc.Channel(reconcileChan, &ctrlruntimehandler.EnqueueRequestForObject{}))

	if ec.manager != nil {
		ctrl, err := ctrlruntimectrl.New(ec.name, ec.manager, ctrlruntimectrl.Options{Reconciler: ec})
		if err != nil {
			return fmt.Errorf("error creating controller: %w", err)
		}

		for _, source := range ec.watchSources {
			err := ctrl.Watch(source)
			if err != nil {
				return fmt.Errorf("error watching resource: %w", err)
			}
		}

		go ec.Subscribe(ctx, reconcileChan)
		err = ec.manager.Start(ctx)
		if err != nil {
			return fmt.Errorf("error starting manager: %w", err)
		}
		return nil
	}

	// keep the thread alive
	ec.logger.Info("waiting until stop signal is received")
	wait.Until(func() {
		<-ctx.Done()
		close(stopCh)
	}, time.Second, stopCh)
	ec.logger.Info("stop signal received. finishing controller...")

	return nil
}

func (ec *ExtensionController) Subscribe(ctx context.Context, reconcileChan chan ctrlruntimeevent.GenericEvent) {
	err := ec.extensionClient.subscribe(ctx, func(response *extpb.SubscribeResponse) {
		ec.logger.Info("received response", "response", response)
		// todo(adam-cattermole): how might we inform of an error from subscribe responses?
		if response.Error != nil && response.Error.Code != 0 {
			ec.logger.Error(fmt.Errorf("got error from stream: code=%d msg=%s", response.Error.Code, response.Error.Message), "error", response.Error.Message)
			return
		}
		trigger := &unstructured.Unstructured{}
		if response.Event != nil && response.Event.Metadata != nil {
			trigger.SetName(response.Event.Metadata.Name)
			trigger.SetNamespace(response.Event.Metadata.Namespace)
			trigger.SetKind(response.Event.Metadata.Kind)
			reconcileChan <- ctrlruntimeevent.GenericEvent{Object: trigger}
		}
	})
	if err != nil {
		ec.logger.Error(err, "grpc subscribe failed")
	}
}

func (ec *ExtensionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// todo(adam-cattermole): the ctx passed here is a different one created by ctrlruntime for each reconcile so we
	//  have to inject here instead of in Start(). Is there any benefit to us storing this in the context for it be
	//  retrieved by the user in their Reconcile method, or should it just pass them as parameters?
	// update ctx to hold our logger and client
	ctx = context.WithValue(ctx, logr.Logger{}, ec.logger)
	ctx = context.WithValue(ctx, extutils.SchemeKey, ec.manager.GetScheme())
	ctx = context.WithValue(ctx, extutils.ClientKey, ec.manager.GetClient())

	// overrides reconcile method
	ec.logger.Info("reconciling request", "namespace", request.Namespace, "name", request.Name)
	return ec.reconcile(ctx, request, ec)
}

func (ec *ExtensionController) Resolve(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
	resp, err := ec.extensionClient.client.Resolve(ctx, &extpb.ResolveRequest{
		Policy:     extutils.MapToExtPolicy(policy),
		Expression: expression,
		Subscribe:  subscribe,
	})
	if err != nil {
		return ref.Val(nil), fmt.Errorf("error resolving expression: %w", err)
	}

	if resp == nil || resp.GetCelResult() == nil {
		return celtypes.NullValue, nil
	}
	val, err := cel.ValueToRefValue(celtypes.DefaultTypeAdapter, resp.GetCelResult())
	if err != nil {
		return ref.Val(nil), fmt.Errorf("error converting cel result: %w", err)
	}

	return val, nil
}

func (ec *ExtensionController) Manager() ctrlruntime.Manager {
	return ec.manager
}

type extensionClient struct {
	conn   *grpc.ClientConn
	client extpb.ExtensionServiceClient
}

func newExtensionClient(socketPath string) (*extensionClient, error) {
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}

	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	return &extensionClient{
		conn:   conn,
		client: extpb.NewExtensionServiceClient(conn),
	}, nil
}

//lint:ignore U1000
func (ec *extensionClient) ping(ctx context.Context) (*extpb.PongResponse, error) {
	return ec.client.Ping(ctx, &extpb.PingRequest{
		Out: timestamppb.New(time.Now()),
	})
}

func (ec *extensionClient) subscribe(ctx context.Context, callback func(response *extpb.SubscribeResponse)) error {
	stream, err := ec.client.Subscribe(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		callback(response)
	}
	return nil
}

//lint:ignore U1000
func (ec *extensionClient) close() error {
	return ec.conn.Close()
}

type Builder struct {
	name       string
	scheme     *runtime.Scheme
	logger     logr.Logger
	reconcile  exttypes.ReconcileFn
	watchTypes []client.Object
}

func NewBuilder(name string) (*Builder, logr.Logger) {
	logger := zap.New(
		zap.Level(logLevel),
		zap.UseDevMode(logMode),
		zap.WriteTo(os.Stderr),
	).WithName(name)
	ctrlruntime.SetLogger(logger)
	klog.SetLogger(logger)

	return &Builder{
		name:       name,
		logger:     logger,
		watchTypes: make([]client.Object, 0),
	}, logger
}

func (b *Builder) WithScheme(scheme *runtime.Scheme) *Builder {
	b.scheme = scheme
	return b
}

func (b *Builder) WithReconciler(fn exttypes.ReconcileFn) *Builder {
	b.reconcile = fn
	return b
}

func (b *Builder) Watches(obj client.Object) *Builder {
	b.watchTypes = append(b.watchTypes, obj)
	return b
}

func (b *Builder) Build() (*ExtensionController, error) {
	if b.name == "" {
		return nil, fmt.Errorf("controller name must be set")
	}
	if b.scheme == nil {
		return nil, fmt.Errorf("scheme must be set")
	}
	if b.reconcile == nil {
		return nil, fmt.Errorf("reconcile function must be set")
	}
	if len(b.watchTypes) == 0 {
		return nil, fmt.Errorf("watch sources must be set")
	}

	// todo(adam-cattermole): we could rework this to be either unix socket path or host etc and configure appropriately
	if len(os.Args) < 2 {
		return nil, errors.New("missing socket path argument")
	}
	socketPath := os.Args[1]

	extClient, err := newExtensionClient(socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create extension client: %w", err)
	}

	options := ctrlruntime.Options{
		Scheme:  b.scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	}

	mgr, err := ctrlruntime.NewManager(ctrlruntime.GetConfigOrDie(), options)
	if err != nil {
		return nil, fmt.Errorf("unable to construct manager: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("unable to create client for manager: %w", err)
	}

	watchSources := make([]ctrlruntimesrc.Source, 0)
	for _, obj := range b.watchTypes {
		source := ctrlruntimesrc.Kind(mgr.GetCache(), obj, &ctrlruntimehandler.EnqueueRequestForObject{})
		watchSources = append(watchSources, source)
	}

	return &ExtensionController{
		name:            b.name,
		manager:         mgr,
		client:          dynamicClient,
		logger:          b.logger,
		reconcile:       b.reconcile,
		watchSources:    watchSources,
		extensionClient: extClient,
	}, nil
}

func Resolve[T any](ctx context.Context, kuadrantCtx exttypes.KuadrantCtx, policy exttypes.Policy, expression string, subscribe bool) (T, error) {
	var zero T

	celValue, err := kuadrantCtx.Resolve(ctx, policy, expression, subscribe)
	if err != nil {
		return zero, err
	}

	nativeValue, err := celValue.ConvertToNative(reflect.TypeOf(zero))
	if err != nil {
		return zero, err
	}

	result, ok := nativeValue.(T)
	if !ok {
		return zero, fmt.Errorf("value is not type: %T", zero)
	}
	return result, nil
}
