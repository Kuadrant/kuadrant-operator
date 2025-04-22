package extensioncontroller

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/go-logr/logr"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntimectrl "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrlruntimehandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	ctrlruntimesrc "sigs.k8s.io/controller-runtime/pkg/source"
)

type KuadrantCtx interface{}

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
	watchSources    []ctrlruntimesrc.Source
	kuadrantCtx     *KuadrantCtx
	extensionClient *extensionClient
	// map of namespaced names
	// todo(adam-cattermole): This could change to be keyed on the cel expression, value of the set of namespaced names
	//   that should be triggered. This would let us pick which set of resources require update for which expression
	resources   map[types.NamespacedName]struct{}
	resourcesMu sync.Mutex
}

func (ec *ExtensionController) Start(ctx context.Context) error {
	stopCh := make(chan struct{})
	// todo(adam-cattermole): how big do we make the reconcile event channel?
	//	 how many should we queue before we block?
	reconcileChan := make(chan event.GenericEvent, 50)
	ec.watchSources = append(ec.watchSources, ctrlruntimesrc.Channel(reconcileChan, &ctrlruntimehandler.EnqueueRequestForObject{}))

	if ec.manager != nil {
		ctrl, err := ctrlruntimectrl.New(ec.name, ec.manager, ctrlruntimectrl.Options{Reconciler: ec})
		if err != nil {
			return fmt.Errorf("error creating controller: %v", err)
		}

		for _, source := range ec.watchSources {
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
		// lock the resources map
		ec.resourcesMu.Lock()
		for nn := range ec.resources {
			us := &unstructured.Unstructured{}
			us.SetNamespace(nn.Namespace)
			us.SetName(nn.Name)
			reconcileChan <- event.GenericEvent{Object: us}
		}
		ec.resourcesMu.Unlock()
	})
	if err != nil {
		ec.logger.Error(err, "grpc subscribe failed")
	}
}

func (ec *ExtensionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// overrides reconcile method
	ec.logger.Info("reconciling request", "namespace", request.Namespace, "name", request.Name)
	ec.resourcesMu.Lock()
	ec.resources[request.NamespacedName] = struct{}{}
	ec.resourcesMu.Unlock()
	return ec.reconcile(ctx, request, ec.kuadrantCtx)
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

type ExtensionControllerBuilder struct {
	name       string
	manager    ctrlruntime.Manager
	client     *dynamic.DynamicClient
	logger     logr.Logger
	reconcile  ReconcileFn
	socketPath string
	watchTypes []client.Object
}

func NewExtensionControllerBuilder() *ExtensionControllerBuilder {
	return &ExtensionControllerBuilder{
		watchTypes: make([]client.Object, 0),
	}
}

func (b *ExtensionControllerBuilder) WithName(name string) *ExtensionControllerBuilder {
	b.name = name
	return b
}

func (b *ExtensionControllerBuilder) WithManager(mgr ctrlruntime.Manager) *ExtensionControllerBuilder {
	b.manager = mgr
	return b
}

func (b *ExtensionControllerBuilder) WithClient(client *dynamic.DynamicClient) *ExtensionControllerBuilder {
	b.client = client
	return b
}

// todo(adam-cattermole): we could choose not to expose this and use this to enforce the logger is using the env vars
func (b *ExtensionControllerBuilder) WithLogger(logger logr.Logger) *ExtensionControllerBuilder {
	b.logger = logger
	return b
}

func (b *ExtensionControllerBuilder) WithReconciler(fn ReconcileFn) *ExtensionControllerBuilder {
	b.reconcile = fn
	return b
}

// todo(adam-cattermole): we could rework this to be either unix socket path or host etc and configure appropriately
func (b *ExtensionControllerBuilder) WithSocketPath(path string) *ExtensionControllerBuilder {
	b.socketPath = path
	return b
}

func (b *ExtensionControllerBuilder) Watches(obj client.Object) *ExtensionControllerBuilder {
	b.watchTypes = append(b.watchTypes, obj)
	return b
}

func (b *ExtensionControllerBuilder) Build() (*ExtensionController, error) {
	if b.name == "" {
		return nil, fmt.Errorf("controller name must be set")
	}
	if b.manager == nil {
		return nil, fmt.Errorf("manager must be set")
	}
	if b.client == nil {
		return nil, fmt.Errorf("dynamic client must be set")
	}
	if b.reconcile == nil {
		return nil, fmt.Errorf("reconcile function must be set")
	}
	if b.socketPath == "" {
		return nil, fmt.Errorf("socket path must be set")
	}
	if len(b.watchTypes) == 0 {
		return nil, fmt.Errorf("watch sources must be set")
	}
	extClient, err := newExtensionClient(b.socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create extension client: %v", err)
	}

	watchSources := make([]ctrlruntimesrc.Source, 0)
	for _, obj := range b.watchTypes {
		source := ctrlruntimesrc.Kind(b.manager.GetCache(), obj, &ctrlruntimehandler.EnqueueRequestForObject{})
		watchSources = append(watchSources, source)
	}

	return &ExtensionController{
		name:            b.name,
		manager:         b.manager,
		client:          b.client,
		logger:          b.logger,
		reconcile:       b.reconcile,
		watchSources:    watchSources,
		kuadrantCtx:     nil,
		extensionClient: extClient,
		resources:       make(map[types.NamespacedName]struct{}),
	}, nil
}
