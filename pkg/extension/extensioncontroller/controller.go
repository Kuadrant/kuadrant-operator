package extensioncontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	ctrlruntimectrl "sigs.k8s.io/controller-runtime/pkg/controller"
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
	name        string
	logger      logr.Logger
	manager     ctrlruntime.Manager
	client      *dynamic.DynamicClient
	reconcile   ReconcileFn
	WatchSource []ctrlruntimesrc.Source
	kuadrantCtx *KuadrantCtx
}

func (ec *ExtensionController) Start(ctx context.Context) error {
	stopCh := make(chan struct{})

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

func (ec *ExtensionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// overrides reconcile method

	return ec.reconcile(ctx, request, ec.kuadrantCtx)
}

// TODO(adam-cattermole): replace with builder pattern
func NewExtensionController(name string, manager ctrlruntime.Manager, client *dynamic.DynamicClient, logger logr.Logger, reconcile ReconcileFn) *ExtensionController {
	return &ExtensionController{
		name:        name,
		logger:      logger,
		manager:     manager,
		client:      client,
		reconcile:   reconcile,
		WatchSource: []ctrlruntimesrc.Source{},
		kuadrantCtx: &KuadrantCtx{},
	}
}
