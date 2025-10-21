package controller

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	ctrlruntimehandler "sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/utils/env"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlruntimesrc "sigs.k8s.io/controller-runtime/pkg/source"

	basereconciler "github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

var (
	logLevel, _ = zapcore.ParseLevel(env.GetString("LOG_LEVEL", "info"))
	logMode     = env.GetString("LOG_MODE", "production") != "production"
)

// Builder constructs an ExtensionController with a fluent API similar to
// controller-runtime's builder, adding extension specific concerns (gRPC
// client, event cache, unix socket path).
type Builder struct {
	name       string
	scheme     *runtime.Scheme
	logger     logr.Logger
	reconcile  exttypes.ReconcileFn
	forType    client.Object
	watchTypes []client.Object
	ownTypes   []client.Object
}

// NewBuilder creates a new Builder for a given controller name and returns it
// alongside the configured logger.
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

// WithScheme sets the runtime Scheme used by the manager.
func (b *Builder) WithScheme(scheme *runtime.Scheme) *Builder {
	b.scheme = scheme
	return b
}

// WithReconciler sets the user reconcile function.
func (b *Builder) WithReconciler(fn exttypes.ReconcileFn) *Builder {
	b.reconcile = fn
	return b
}

// For sets the primary object type (policy) reconciled by the controller.
func (b *Builder) For(obj client.Object) *Builder {
	if b.forType != nil {
		panic("For() can only be called once")
	}
	b.forType = obj
	return b
}

// Watches registers additional object types whose events should enqueue the
// primary object's reconcile requests.
func (b *Builder) Watches(obj client.Object) *Builder {
	b.watchTypes = append(b.watchTypes, obj)
	return b
}

// Owns registers owned object types so that owner references are resolved and
// reconcile requests enqueued for the owning policy kind.
func (b *Builder) Owns(obj client.Object) *Builder {
	b.ownTypes = append(b.ownTypes, obj)
	return b
}

// Build validates the configuration, creates the underlying manager, gRPC
// client and returns a ready to Start ExtensionController.
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
	if b.forType == nil {
		return nil, fmt.Errorf("for type must be set")
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

	eventCache := newEventTypeCache()
	eventHandler := newEventCachingHandler(eventCache)

	watchSources := make([]ctrlruntimesrc.Source, 0)
	forSource := ctrlruntimesrc.Kind(mgr.GetCache(), b.forType, eventHandler)
	watchSources = append(watchSources, forSource)

	for _, obj := range b.watchTypes {
		source := ctrlruntimesrc.Kind(mgr.GetCache(), obj, eventHandler)
		watchSources = append(watchSources, source)
	}

	for _, obj := range b.ownTypes {
		var hdler ctrlruntimehandler.EventHandler
		reflect.ValueOf(&hdler).Elem().Set(reflect.ValueOf(ctrlruntimehandler.EnqueueRequestForOwner(
			mgr.GetScheme(), mgr.GetRESTMapper(),
			b.forType,
		)))
		source := ctrlruntimesrc.Kind(mgr.GetCache(), obj, hdler)
		watchSources = append(watchSources, source)
	}

	objType := reflect.TypeOf(b.forType)
	if objType.Kind() == reflect.Ptr {
		objType = objType.Elem()
	}
	policyKind := objType.Name()

	config := ExtensionConfig{
		Name:         b.name,
		PolicyKind:   policyKind,
		ForType:      b.forType,
		Reconcile:    b.reconcile,
		WatchSources: watchSources,
	}

	return &ExtensionController{
		config:          config,
		manager:         mgr,
		logger:          b.logger,
		extensionClient: extClient,
		eventCache:      eventCache,
		BaseReconciler:  basereconciler.NewBaseReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader()),
	}, nil
}
