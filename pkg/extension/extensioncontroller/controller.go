package extensioncontroller

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/go-logr/logr"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	ctrlruntimectrl "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrlruntimehandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	ctrlruntimesrc "sigs.k8s.io/controller-runtime/pkg/source"
)

type KuadrantCtx struct{}

type MetaReconciler = TypedMetaReconciler[reconcile.Request]

type TypedMetaReconciler[request comparable] interface {
	Reconcile(context.Context, request, *KuadrantCtx) (reconcile.Result, error)
}

type ReconcileFn func(ctx context.Context, request reconcile.Request, kuadrant *KuadrantCtx) (reconcile.Result, error)

type ExtensionController struct {
	name            string
	logger          logr.Logger
	manager         ctrlruntime.Manager
	client          *dynamic.DynamicClient
	reconcile       ReconcileFn
	WatchSource     []ctrlruntimesrc.Source
	kuadrantCtx     *KuadrantCtx
	extensionClient *extensionClient
}

func (ec *ExtensionController) Start(ctx context.Context) error {
	stopCh := make(chan struct{})
	reconcileChan := make(chan event.GenericEvent, 10)
	ec.WatchSource = append(ec.WatchSource, ctrlruntimesrc.Channel(reconcileChan, &ctrlruntimehandler.EnqueueRequestForObject{}))

	if ec.manager != nil {
		ctrl, err := ctrlruntimectrl.New(ec.name, ec.manager, ctrlruntimectrl.Options{Reconciler: ec})
		if err != nil {
			return fmt.Errorf("error creating controller: %v", err)
		}

		for _, source := range ec.WatchSource {
			err := ctrl.Watch(source)
			if err != nil {
				return fmt.Errorf("error watching resource: %v", err)
			}
		}

		go ec.Subscribe(ctx, reconcileChan)
		err = ec.manager.Start(ctx)
		if err != nil {
			return fmt.Errorf("error starting manager: %v", err)
		}
		return nil
	}

	// keep the thread alive
	ec.logger.Info("waiting until stop signal is received")
	wait.Until(func() {
		select {
		case <-ctx.Done():
			close(stopCh)
		}
	}, time.Second, stopCh)
	ec.logger.Info("stop signal received. finishing controller...")

	return nil
}

func (ec *ExtensionController) Subscribe(ctx context.Context, reconcileChan chan event.GenericEvent) {
	err := ec.extensionClient.subscribe(ctx, func(pong *extpb.PongResponse) {
		ec.logger.Info("received pong", "timestamp", pong.In.AsTime())
		//todo(adam-cattermole): temporarily generating unstructured
		us := &unstructured.Unstructured{}
		us.SetNamespace("default")
		us.SetName("pong-trigger")

		reconcileChan <- event.GenericEvent{Object: us}
	})
	if err != nil {
		ec.logger.Error(err, "grpc subscribe failed")
	}
}

func (ec *ExtensionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// overrides reconcile method
	ec.logger.Info("reconciling request", "namespace", request.Namespace, "name", request.Name)
	return ec.reconcile(ctx, request, ec.kuadrantCtx)
}

// TODO(adam-cattermole): replace with builder pattern
func NewExtensionController(name string, manager ctrlruntime.Manager, client *dynamic.DynamicClient, logger logr.Logger, reconcile ReconcileFn, socketPath string) (*ExtensionController, error) {
	extClient, err := newExtensionClient(socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create extension client: %v", err)
	}
	return &ExtensionController{
		name:            name,
		logger:          logger,
		manager:         manager,
		client:          client,
		reconcile:       reconcile,
		WatchSource:     []ctrlruntimesrc.Source{},
		kuadrantCtx:     &KuadrantCtx{},
		extensionClient: extClient,
	}, nil
}

type extensionClient struct {
	conn   *grpc.ClientConn
	client extpb.ExtensionServiceClient
}

func newExtensionClient(socketPath string) (*extensionClient, error) {
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
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

func (ec *extensionClient) ping(ctx context.Context) (*extpb.PongResponse, error) {
	return ec.client.Ping(ctx, &extpb.PingRequest{
		Out: timestamppb.New(time.Now()),
	})
}

func (ec *extensionClient) subscribe(ctx context.Context, callback func(*extpb.PongResponse)) error {
	stream, err := ec.client.Subscribe(ctx, &extpb.PingRequest{})
	if err != nil {
		return err
	}
	for {
		pong, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		callback(pong)
	}
	return nil
}

func (ec *extensionClient) close() error {
	return ec.conn.Close()
}
