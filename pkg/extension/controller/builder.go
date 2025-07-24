package controller

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
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

type Builder struct {
	name       string
	scheme     *runtime.Scheme
	logger     logr.Logger
	reconcile  exttypes.ReconcileFn
	forType    client.Object
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

func (b *Builder) For(obj client.Object) *Builder {
	if b.forType != nil {
		panic("For() can only be called once")
	}
	b.forType = obj
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

	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("unable to create client for manager: %w", err)
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

	objType := reflect.TypeOf(b.forType)
	if objType.Kind() == reflect.Ptr {
		objType = objType.Elem()
	}
	policyKind := objType.Name()

	return &ExtensionController{
		name:            b.name,
		manager:         mgr,
		client:          dynamicClient,
		logger:          b.logger,
		reconcile:       b.reconcile,
		watchSources:    watchSources,
		extensionClient: extClient,
		eventCache:      eventCache,
		policyKind:      policyKind,
		BaseReconciler:  basereconciler.NewBaseReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader()),
	}, nil
}
