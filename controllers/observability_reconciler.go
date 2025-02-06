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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kuadrantObservabilityLabel = "kuadrant-observability"
	kOpMonitorName             = "kuadrant-operator-monitor"
	dnsOpMonitorName           = "dns-operator-monitor"
	authOpMonitorName          = "authorino-operator-monitor"
	limitOpMonitorName         = "limitador-operator-monitor"
	istiodMonitorName          = "istiod-monitor"
	istiodMonitorNS            = "istio-system"
	istioPodMonitorName        = "istio-pod-monitor"
	envoyGatewayMonitorName    = "envoy-gateway-monitor"
	envoyGatewayMonitorNS      = "envoy-gateway-system"
	envoyStatsMonitorName      = "envoy-stats-monitor"
)

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
var istiodMonitorSpec = &monitoringv1.ServiceMonitor{
	TypeMeta: metav1.TypeMeta{
		Kind:       monitoringv1.ServiceMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: istiodMonitorName,
		Labels: map[string]string{
			kuadrantObservabilityLabel: "true",
		},
	},
	Spec: monitoringv1.ServiceMonitorSpec{
		Endpoints: []monitoringv1.Endpoint{{
			Port: "http-monitoring",
		}},
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "istiod",
			},
		},
	},
}

var istioPodMonitorPortReplacement1 = `[$2]:$1`
var istioPodMonitorPortReplacement2 = `$2:$1`
var istioPodMonitorSpec = &monitoringv1.PodMonitor{
	TypeMeta: metav1.TypeMeta{
		Kind:       monitoringv1.PodMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: istioPodMonitorName,
		Labels: map[string]string{
			kuadrantObservabilityLabel: "true",
		},
	},
	Spec: monitoringv1.PodMonitorSpec{
		Selector: metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "istio-prometheus-ignore",
					Operator: metav1.LabelSelectorOpDoesNotExist,
				},
			},
		},
		PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
			{
				Path:     "/stats/prometheus",
				Interval: "30s",
				RelabelConfigs: []monitoringv1.RelabelConfig{
					{
						Action:       "keep",
						SourceLabels: []monitoringv1.LabelName{"__meta_kubernetes_pod_container_name"},
						Regex:        "istio-proxy",
					},
					{
						Action:       "keep",
						SourceLabels: []monitoringv1.LabelName{"__meta_kubernetes_pod_annotationpresent_prometheus_io_scrape"},
					},
					{
						Action: "replace",
						SourceLabels: []monitoringv1.LabelName{
							"__meta_kubernetes_pod_annotation_prometheus_io_port",
							"__meta_kubernetes_pod_ip",
						},
						Regex:       `(\\d+);(([A-Fa-f0-9]{1,4}::?){1,7}[A-Fa-f0-9]{1,4})`,
						Replacement: &istioPodMonitorPortReplacement1,
						TargetLabel: "__address__",
					},
					{
						Action: "replace",
						SourceLabels: []monitoringv1.LabelName{
							"__meta_kubernetes_pod_annotation_prometheus_io_port",
							"__meta_kubernetes_pod_ip",
						},
						Regex:       `(\\d+);((([0-9]+?)(\\.|$)){4})`,
						Replacement: &istioPodMonitorPortReplacement2,
						TargetLabel: "__address__",
					},
					{
						Action: "labeldrop",
						Regex:  "__meta_kubernetes_pod_label_(.+)",
					},
					{
						Action:       "replace",
						SourceLabels: []monitoringv1.LabelName{"__meta_kubernetes_namespace"},
						TargetLabel:  "namespace",
					},
					{
						Action:       "replace",
						SourceLabels: []monitoringv1.LabelName{"__meta_kubernetes_pod_name"},
						TargetLabel:  "pod_name",
					},
				},
			},
		},
	},
}

var envoyGatewayMonitorSpec = &monitoringv1.ServiceMonitor{
	TypeMeta: metav1.TypeMeta{
		Kind:       monitoringv1.ServiceMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: envoyGatewayMonitorName,
		Labels: map[string]string{
			kuadrantObservabilityLabel: "true",
		},
	},
	Spec: monitoringv1.ServiceMonitorSpec{
		Endpoints: []monitoringv1.Endpoint{{
			Port: "metrics",
		}},
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"control-plane": "envoy-gateway",
			},
		},
	},
}

var envoyStatsPort = "http-envoy-prom"
var envoyStatsMonitorSpec = &monitoringv1.PodMonitor{
	TypeMeta: metav1.TypeMeta{
		Kind:       monitoringv1.PodMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: envoyStatsMonitorName,
		Labels: map[string]string{
			kuadrantObservabilityLabel: "true",
		},
	},
	Spec: monitoringv1.PodMonitorSpec{
		PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{{
			Port: &envoyStatsPort,
			Path: "/stats/prometheus",
		}},
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "kuadrant-ingressgateway",
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
	if err := r.createMonitor(ctx, monitorObjs, kOpMonitorSpec, kObj.Namespace, logger); err != nil {
		logger.Error(err, "failed to create kuadrant operator monitor", "status", "error")
		return err
	}

	// DNS Operator monitor
	if err := r.createMonitor(ctx, monitorObjs, dnsOpMonitorSpec, kObj.Namespace, logger); err != nil {
		logger.Error(err, "failed to create dns operator monitor", "status", "error")
		return err
	}

	// Authorino operator monitor
	if err := r.createMonitor(ctx, monitorObjs, authOpMonitorSpec, kObj.Namespace, logger); err != nil {
		logger.Error(err, "failed to create authorino operator monitor", "status", "error")
		return err
	}

	// Limitador operator monitor
	if err := r.createMonitor(ctx, monitorObjs, limitOpMonitorSpec, kObj.Namespace, logger); err != nil {
		logger.Error(err, "failed to create limitador operator monitor", "status", "error")
		return err
	}

	// Create monitors for each gateway instance
	gateways := lo.Map(topology.Targetables().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == machinery.GatewayGroupKind
	}), func(g machinery.Targetable, _ int) *machinery.Gateway {
		return g.(*machinery.Gateway)
	})

	for _, gateway := range gateways {
		gatewayClasses := topology.All().Parents(gateway)
		logger.V(1).Info(fmt.Sprintf("found %d gateway classes for gateway %s/%s", len(gatewayClasses), gateway.Namespace, gateway.Name))
		if len(gatewayClasses) != 1 {
			// must be 1 parent only
			continue
		}
		gatewayClass := gatewayClasses[0].(*machinery.GatewayClass)
		if gatewayClass.GatewayClass.Spec.ControllerName == istioGatewayControllerName {
			if err := r.createMonitor(ctx, monitorObjs, istiodMonitorSpec, istiodMonitorNS, logger); err != nil {
				logger.Error(err, "failed to create istiod monitor", "status", "error")
				return err
			}
			if err := r.createMonitor(ctx, monitorObjs, istioPodMonitorSpec, gateway.Namespace, logger); err != nil {
				logger.Error(err, "failed to create istio pod monitor", "status", "error")
				return err
			}
		} else if gatewayClass.GatewayClass.Spec.ControllerName == envoyGatewayGatewayControllerName {
			if err := r.createMonitor(ctx, monitorObjs, envoyGatewayMonitorSpec, envoyGatewayMonitorNS, logger); err != nil {
				logger.Error(err, "failed to create envoy gateway monitor", "status", "error")
				return err
			}
			if err := r.createMonitor(ctx, monitorObjs, envoyStatsMonitorSpec, gateway.Namespace, logger); err != nil {
				logger.Error(err, "failed to create envoy stats monitor", "status", "error")
				return err
			}
		}
	}

	return nil
}

func (r *ObservabilityReconciler) createMonitor(ctx context.Context, monitorObjs []machinery.Object, monitor client.Object, ns string, logger logr.Logger) error {
	_, monitorExists := lo.Find(monitorObjs, func(item machinery.Object) bool {
		return item.GroupVersionKind().Kind == monitor.GetObjectKind().GroupVersionKind().Kind && item.GetName() == monitor.GetName() && item.GetNamespace() == ns
	})
	if monitorExists {
		logger.V(1).Info(fmt.Sprintf("monitor already exists %s/%s, skipping create", ns, monitor.GetName()))
		return nil
	} else {
		logger.V(1).Info(fmt.Sprintf("creating monitor %s", monitor.GetName()))
		obj, err := controller.Destruct(monitor)
		if err != nil {
			return err
		}

		mapping, err := r.restMapper.RESTMapping(monitor.GetObjectKind().GroupVersionKind().GroupKind())
		if err != nil {
			logger.Error(err, "failed to get monitor restmapping", "status", "error")
			return err
		}
		if _, err = r.Client.Resource(mapping.Resource).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{}); err == nil {
			logger.V(1).Info(fmt.Sprintf("created monitor %s", monitor.GetName()))
		}
		return err
	}
}

func (r *ObservabilityReconciler) deleteAllMonitors(ctx context.Context, monitorObjs []machinery.Object, logger logr.Logger) {
	for _, monitor := range monitorObjs {
		logger.V(1).Info(fmt.Sprintf("deleting monitor %s %s/%s", monitor.GroupVersionKind().Kind, monitor.GetNamespace(), monitor.GetName()))
		mapping, err := r.restMapper.RESTMapping(monitor.GroupVersionKind().GroupKind())
		if err != nil {
			logger.Error(err, "failed to get monitor restmapping", "status", "error")
			return
		}
		if err = r.Client.Resource(mapping.Resource).Namespace(monitor.GetNamespace()).Delete(ctx, monitor.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete monitor", "status", "error")
			return
		}
		logger.V(1).Info(fmt.Sprintf("deleted monitor %s %s/%s", monitor.GroupVersionKind().Kind, monitor.GetNamespace(), monitor.GetName()))
	}

}
