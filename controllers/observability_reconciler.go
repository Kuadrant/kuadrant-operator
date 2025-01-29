package controllers

import (
	"context"
	"sync"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/utils"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monclient "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
)

type ObservabilityReconciler struct {
	Client     *dynamic.DynamicClient
	restMapper meta.RESTMapper
	MonClient  monclient.Interface
}

func NewObservabilityReconciler(client *dynamic.DynamicClient, rm meta.RESTMapper, mc monclient.Interface) *ObservabilityReconciler {
	return &ObservabilityReconciler{
		Client:     client,
		restMapper: rm,
		MonClient:  mc,
	}
}

func (r *ObservabilityReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{ReconcileFunc: r.Reconcile, Events: []controller.ResourceEventMatcher{
		{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind)},
	},
	}
}

func (r *ObservabilityReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ObservabilityReconciler")
	logger.Info("reconciling observability", "status", "started")
	defer logger.Info("reconciling observability", "status", "completed")

	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil {
		return nil
	}

	// Check if observability is enabled
	if !kObj.Spec.Observability.Enable {
		logger.Info("observability enable is false")
		// TODO: Remove monitors if they exist
		return nil
	}
	logger.Info("observability enable is true")

	// Check if monitoring CRDs exist
	if ok, err := utils.IsCRDInstalled(r.restMapper, monitoringv1.SchemeGroupVersion.Group, monitoringv1.ServiceMonitorsKind, monitoringv1.SchemeGroupVersion.Version); !ok || err != nil {
		logger.Info("ServiceMonitor CRD not installed")
		return nil
	}
	logger.Info("ServiceMonitor CRD is installed")

	if ok, err := utils.IsCRDInstalled(r.restMapper, monitoringv1.SchemeGroupVersion.Group, monitoringv1.PodMonitorsKind, monitoringv1.SchemeGroupVersion.Version); !ok || err != nil {
		logger.Info("PodMonitor CRD not installed")
		return nil
	}
	logger.Info("PodMonitor CRD is installed")

	// TODO: Create monitors for kuadrant operator
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant-operator-metrics",
			Namespace: "kuadrant-system",
			Labels: map[string]string{
				"control-plane": "controller-manager",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{{
				Port:   "metrics",
				Path:   "/metrics",
				Scheme: "http",
			}},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"control-plane": "controller-manager",
					"app":           "kuadrant",
				},
			},
		},
	}

	// TODO: namespace
	_, err := r.MonClient.MonitoringV1().ServiceMonitors("kuadrant-system").Create(ctx, sm, metav1.CreateOptions{})
	if err != nil {
		logger.Error(err, "failed to create ServiceMonitor")
		return nil
	}

	// TODO: Create monitors for dns operator

	// TODO: Create monitors for Authorino, if installed

	// TODO: Create monitors for Limitador, if installed

	// TODO: Create monitors for gateway provider

	return nil
}
