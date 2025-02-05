package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
)

const kuadrantObservabilityLabel = "kuadrant-observability"
const kOpMonitorName = "kuadrant-operator-monitor"
const dnsOpMonitorName = "dns-operator-monitor"
const authOpMonitorName = "authorino-operator-monitor"
const limitOpMonitorName = "limitador-operator-monitor"

var kOpMonitorSpec = &monitoringv1.ServiceMonitor{
	TypeMeta: metav1.TypeMeta{
		Kind:       monitoringv1.ServiceMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: kOpMonitorName,
		Labels: map[string]string{
			"control-plane":            "controller-manager",
			kuadrantObservabilityLabel: "true",
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

var dnsOpMonitorSpec = &monitoringv1.ServiceMonitor{
	TypeMeta: metav1.TypeMeta{
		Kind:       monitoringv1.ServiceMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: dnsOpMonitorName,
		Labels: map[string]string{
			kuadrantObservabilityLabel:     "true",
			"control-plane":                "controller-manager",
			"app.kubernetes.io/name":       "servicemonitor",
			"app.kubernetes.io/instance":   "controller-manager-metrics-monitor",
			"app.kubernetes.io/component":  "metrics",
			"app.kubernetes.io/created-by": "dns-operator",
			"app.kubernetes.io/part-of":    "dns-operator",
			"app.kubernetes.io/managed-by": "kustomize",
		},
	},
	Spec: monitoringv1.ServiceMonitorSpec{
		Endpoints: []monitoringv1.Endpoint{{
			Path:   "/metrics",
			Port:   "metrics",
			Scheme: "http",
		}},
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"control-plane": "dns-operator-controller-manager",
			},
		},
	},
}

var authOpMonitorSpec = &monitoringv1.ServiceMonitor{
	TypeMeta: metav1.TypeMeta{
		Kind:       monitoringv1.ServiceMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: authOpMonitorName,
		Labels: map[string]string{
			"control-plane":            "controller-manager",
			kuadrantObservabilityLabel: "true",
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
				"control-plane": "authorino-operator",
			},
		},
	},
}
var limitOpMonitorSpec = &monitoringv1.ServiceMonitor{
	TypeMeta: metav1.TypeMeta{
		Kind:       monitoringv1.ServiceMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: limitOpMonitorName,
		Labels: map[string]string{
			"control-plane":            "controller-manager",
			kuadrantObservabilityLabel: "true",
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
			},
		},
	},
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
	logger.V(1).Info("reconciling observability", "status", "started")
	defer logger.V(1).Info("reconciling observability", "status", "completed")

	// Get all monitors first, if any exist
	monitorObjs := lo.FilterMap(topology.Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().Kind == monitoringv1.ServiceMonitorsKind || item.GroupVersionKind().Kind == monitoringv1.PodMonitorsKind {
			return item, true
		}
		return nil, false
	})

	// Check that a kuadrant resource exists, and observability enabled,
	// otherwise delete all monitors
	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil || !kObj.Spec.Observability.Enable {
		logger.V(1).Info("deleting any existing monitors", "kuadrant", kObj != nil)
		r.deleteAllMonitors(ctx, monitorObjs, logger)
		return nil
	}

	// Create all monitors
	logger.V(1).Info("observability enabled, creating monitors")

	// Kuadrant Operator monitor
	err := r.createServiceMonitor(ctx, monitorObjs, kOpMonitorName, kOpMonitorSpec, kObj, logger)
	if err != nil {
		logger.Error(err, "failed to create kuadrant operator monitor", "status", "error")
		return err
	}
	logger.V(1).Info("kuadrant operator monitor created")

	// DNS Operator monitor
	err = r.createServiceMonitor(ctx, monitorObjs, dnsOpMonitorName, dnsOpMonitorSpec, kObj, logger)
	if err != nil {
		logger.Error(err, "failed to create dns operator monitor", "status", "error")
		return err
	}
	logger.V(1).Info("dns operator monitor created")

	// Authorino operator monitor
	err = r.createServiceMonitor(ctx, monitorObjs, authOpMonitorName, authOpMonitorSpec, kObj, logger)
	if err != nil {
		logger.Error(err, "failed to create authorino operator monitor", "status", "error")
		return err
	}
	logger.V(1).Info("authorino operator monitor created")

	// Limitador operator monitor
	err = r.createServiceMonitor(ctx, monitorObjs, limitOpMonitorName, limitOpMonitorSpec, kObj, logger)
	if err != nil {
		logger.Error(err, "failed to create limitador operator monitor", "status", "error")
		return err
	}
	logger.V(1).Info("limitador operator monitor created")

	// TODO: Create monitors for gateway provider

	return nil
}

func (r *ObservabilityReconciler) createServiceMonitor(ctx context.Context, monitorObjs []machinery.Object, smName string, sm *monitoringv1.ServiceMonitor, kObj *kuadrantv1beta1.Kuadrant, logger logr.Logger) error {
	_, monitor := lo.Find(monitorObjs, func(item machinery.Object) bool {
		return item.GroupVersionKind().Kind == monitoringv1.ServiceMonitorsKind && item.GetName() == smName
	})
	if monitor {
		logger.V(1).Info(fmt.Sprintf("monitor already exists %s, skipping create", smName))
		return nil
	} else {
		logger.V(1).Info(fmt.Sprintf("creating monitor %s", smName))
		obj, err := controller.Destruct(sm)
		if err != nil {
			return err
		}

		_, err = r.Client.Resource(monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")).Namespace(kObj.Namespace).Create(ctx, obj, metav1.CreateOptions{})
		return err
	}
}

func (r *ObservabilityReconciler) deleteAllMonitors(ctx context.Context, monitorObjs []machinery.Object, logger logr.Logger) {
	lo.ForEach(monitorObjs, func(monitor machinery.Object, index int) {
		logger.V(1).Info(fmt.Sprintf("deleting monitor %s %s/%s", monitor.GroupVersionKind().Kind, monitor.GetNamespace(), monitor.GetName()))
		mapping, err := r.restMapper.RESTMapping(monitor.GroupVersionKind().GroupKind())
		if err != nil {
			logger.Error(err, "failed to get monitor restmapping", "status", "error")
			return
		}
		err = r.Client.Resource(mapping.Resource).Namespace(monitor.GetNamespace()).Delete(ctx, monitor.GetName(), metav1.DeleteOptions{})
		if err != nil {
			logger.Error(err, "failed to delete monitor", "status", "error")
		}
		logger.V(1).Info(fmt.Sprintf("deleted monitor %s %s/%s", monitor.GroupVersionKind().Kind, monitor.GetNamespace(), monitor.GetName()))
	})

}
