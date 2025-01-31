package controllers

import (
	"context"
	"sync"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
)

var serviceMonitorGVR = schema.GroupVersionResource{
	Group:    "monitoring.coreos.com",
	Version:  "v1",
	Resource: "servicemonitors",
}

type ObservabilityReconciler struct {
	Client     *dynamic.DynamicClient
	restMapper meta.RESTMapper
}

func NewObservabilityReconciler(client *dynamic.DynamicClient, rm meta.RESTMapper) *ObservabilityReconciler {
	return &ObservabilityReconciler{
		Client:     client,
		restMapper: rm,
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

	// TODO: deletion and finalizer handling
	// TODO: review log levels of all log statements
	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil {
		return nil
	}

	obsEnabled := kObj.Spec.Observability.Enable

	// Kuadrant Operator monitors
	smObjs := lo.FilterMap(topology.Objects().Objects().Children(kObj), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().Kind == monitoringv1.ServiceMonitorsKind && item.GetName() == "kuadrant-operator-monitor" {
			return item, true
		}
		return nil, false
	})
	smExists := len(smObjs) > 0
	if obsEnabled && smExists {
		logger.Info("observability enable is true, kuadrant operator monitor exists (no action)")
		return nil
	} else if !obsEnabled && !smExists {
		logger.Info("observability enable is false. kuadrant operator monitor doesn't exist (no action)")
		return nil
	} else if obsEnabled && !smExists {
		logger.Info("observability enable is true. kuadrant operator monitor doesn't exist (to be created)")
		err := createServiceMonitor(ctx, r.Client, kObj)
		if err != nil {
			logger.Error(err, "failed to create servicemonitor", "status", "error")
			return err
		}
		logger.Info("kuadrant operator monitor created")
	} else {
		logger.Info("observability enable is false, kuadrant operator monitor exists (to be deleted)")
		// TODO: assume 1, or only care about 1?
		err := deleteServiceMonitor(ctx, r.Client, smObjs[0])
		if err != nil {
			logger.Error(err, "failed to delete servicemonitor", "status", "error")
			return err
		}
		logger.Info("kuadrant operator monitor deleted")
	}

	// TODO: pointer vs non pointer?
	// 	if kObj.Spec.Observability == nil {
	//     // Observability section isn’t set at all
	// } else if kObj.Spec.Observability.Enable {
	//     // Observability is set and enabled
	// }

	// TODO: Create monitors for dns operator

	// TODO: Create monitors for Authorino, if installed

	// TODO: Create monitors for Limitador, if installed

	// TODO: Create monitors for gateway provider

	return nil
}

func createServiceMonitor(ctx context.Context, client *dynamic.DynamicClient, kObj *kuadrantv1beta1.Kuadrant) error {
	sm := &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.ServiceMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			// TODO: constants for labels & name
			Name: "kuadrant-operator-monitor",
			Labels: map[string]string{
				"kuadrant-observability": "true",
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
	obj, err := controller.Destruct(sm)
	if err != nil {
		return err
	}

	_, err = client.Resource(monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")).Namespace(kObj.Namespace).Create(ctx, obj, metav1.CreateOptions{})
	return err
}

func deleteServiceMonitor(ctx context.Context, client *dynamic.DynamicClient, sm machinery.Object) error {
	err := client.Resource(monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")).Namespace(sm.GetNamespace()).Delete(ctx, sm.GetName(), metav1.DeleteOptions{})
	return err
}
