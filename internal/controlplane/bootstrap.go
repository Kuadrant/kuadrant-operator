package controlplane

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

// BootstrapRunnable executes one-time startup tasks after the manager starts
// and leader election is acquired. It ensures the default KuadrantControlPlane
// CR exists and cleans up orphaned OLM resources from pre-consolidation installs.
type BootstrapRunnable struct {
	restConfig *rest.Config
	scheme     *runtime.Scheme
	deployer   *Deployer
	recorder   events.EventRecorder
	namespace  string
	logger     logr.Logger
}

func NewBootstrapRunnable(restConfig *rest.Config, scheme *runtime.Scheme, deployer *Deployer, recorder events.EventRecorder, namespace string, logger logr.Logger) *BootstrapRunnable {
	return &BootstrapRunnable{
		restConfig: restConfig,
		scheme:     scheme,
		deployer:   deployer,
		recorder:   recorder,
		namespace:  namespace,
		logger:     logger.WithName("bootstrap"),
	}
}

func (r *BootstrapRunnable) Start(ctx context.Context) error {
	directClient, err := client.New(r.restConfig, client.Options{Scheme: r.scheme})
	if err != nil {
		return fmt.Errorf("creating direct client: %w", err)
	}

	if err := ensureDefaultControlPlane(ctx, directClient, r.recorder, r.logger); err != nil {
		return fmt.Errorf("ensuring default KuadrantControlPlane: %w", err)
	}

	result := RunOLMCleanup(ctx, r.deployer, r.namespace, r.logger)
	if r.recorder != nil {
		r.emitCleanupEvent(ctx, directClient, result)
	}
	return nil
}

func (r *BootstrapRunnable) emitCleanupEvent(ctx context.Context, c client.Client, result OLMCleanupResult) {
	if result.Skipped {
		return
	}

	cp := &kuadrantv1alpha1.KuadrantControlPlane{}
	if err := c.Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp); err != nil {
		r.logger.V(1).Info("unable to fetch KuadrantControlPlane for cleanup event", "error", err)
		return
	}

	if result.Error != "" {
		r.recorder.Eventf(cp, cp, corev1.EventTypeWarning, "OLMMigrationIncomplete", "OLMMigration", result.Error)
	} else {
		r.recorder.Eventf(cp, cp, corev1.EventTypeNormal, "OLMMigrationComplete", "OLMMigration", result.Summary)
	}

	for _, comp := range result.Components {
		r.recorder.Eventf(cp, componentReference(comp.Package), corev1.EventTypeNormal, "OLMComponentCleaned", "OLMMigration", componentCleanupMessage(comp))
	}
}

// componentCleanupMessage describes the orphaned OLM resources removed for a
// single component, e.g. "removed Subscription dns-operator and CSV
// dns-operator.v0.8.0 (stripped OLM metadata from 3 resources)".
func componentCleanupMessage(comp ComponentCleanupResult) string {
	var parts []string
	if comp.SubscriptionName != "" {
		parts = append(parts, fmt.Sprintf("Subscription %s", comp.SubscriptionName))
	}
	if comp.CSVName != "" {
		parts = append(parts, fmt.Sprintf("CSV %s", comp.CSVName))
	}

	var msg string
	switch len(parts) {
	case 0:
		msg = "stripped OLM metadata from resources"
	case 1:
		msg = "removed " + parts[0]
	default:
		msg = "removed " + parts[0] + " and " + parts[1]
	}

	if comp.MetadataCount > 0 {
		msg += fmt.Sprintf(" (stripped OLM metadata from %d resources)", comp.MetadataCount)
	}
	return msg
}

func (r *BootstrapRunnable) NeedLeaderElection() bool {
	return true
}
