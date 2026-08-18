package controlplane

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BootstrapRunnable executes one-time startup tasks after the manager starts
// and leader election is acquired. It ensures the default KuadrantControlPlane
// CR exists and cleans up orphaned OLM resources from pre-consolidation installs.
type BootstrapRunnable struct {
	restConfig *rest.Config
	scheme     *runtime.Scheme
	deployer   *Deployer
	namespace  string
	logger     logr.Logger
}

func NewBootstrapRunnable(restConfig *rest.Config, scheme *runtime.Scheme, deployer *Deployer, namespace string, logger logr.Logger) *BootstrapRunnable {
	return &BootstrapRunnable{
		restConfig: restConfig,
		scheme:     scheme,
		deployer:   deployer,
		namespace:  namespace,
		logger:     logger.WithName("bootstrap"),
	}
}

func (r *BootstrapRunnable) Start(ctx context.Context) error {
	directClient, err := client.New(r.restConfig, client.Options{Scheme: r.scheme})
	if err != nil {
		return fmt.Errorf("creating direct client: %w", err)
	}

	if err := ensureDefaultControlPlane(ctx, directClient, r.logger); err != nil {
		return fmt.Errorf("ensuring default KuadrantControlPlane: %w", err)
	}

	RunOLMCleanup(ctx, r.deployer, r.namespace, r.logger)
	return nil
}

func (r *BootstrapRunnable) NeedLeaderElection() bool {
	return true
}
