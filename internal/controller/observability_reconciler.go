package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/observability"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
)

//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;podmonitors,verbs=get;list;watch;create;update;patch;delete

const (
	authOpMonitorName     = "authorino-operator-monitor"
	dnsOpMonitorName      = "dns-operator-monitor"
	envoyStatsMonitorName = "envoy-stats-monitor"
	istioPodMonitorName   = "istio-pod-monitor"
	kOpMonitorName        = "kuadrant-operator-monitor"
	limitOpMonitorName    = "limitador-operator-monitor"
	limitPodMonitorName   = "kuadrant-limitador-monitor"
)

func kOpMonitorBuild(ns string) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.ServiceMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kOpMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				"control-plane":             "controller-manager",
				kuadrant.ObservabilityLabel: "true",
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
}

func dnsOpMonitorBuild(ns string) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.ServiceMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsOpMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				kuadrant.ObservabilityLabel:    "true",
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
}

func authOpMonitorBuild(ns string) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.ServiceMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      authOpMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				"control-plane":             "controller-manager",
				kuadrant.ObservabilityLabel: "true",
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
}

func limitOpMonitorBuild(ns string) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.ServiceMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      limitOpMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				"control-plane":             "controller-manager",
				kuadrant.ObservabilityLabel: "true",
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
}

var istioPodMonitorPortReplacement1 = `[$2]:$1`
var istioPodMonitorPortReplacement2 = `$2:$1`

func istioPodMonitorBuild(ns string) *monitoringv1.PodMonitor {
	return &monitoringv1.PodMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PodMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      istioPodMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				kuadrant.ObservabilityLabel: "true",
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
}

func limitMonitorBuild(ns string) *monitoringv1.PodMonitor {
	return &monitoringv1.PodMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PodMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      limitPodMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				kuadrant.ObservabilityLabel: "true",
				"app":                       "limitador",
			},
		},
		Spec: monitoringv1.PodMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                "limitador",
					"limitador-resource": kuadrant.LimitadorName,
				},
			},
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
				{
					Path:   "/metrics",
					Port:   "http",
					Scheme: "http",
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{ns},
			},
		},
	}
}

func envoyStatsMonitorBuild(ns string) *monitoringv1.PodMonitor {
	return &monitoringv1.PodMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PodMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      envoyStatsMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				kuadrant.ObservabilityLabel: "true",
			},
		},
		Spec: monitoringv1.PodMonitorSpec{
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{{
				Port: "http-envoy-prom",
				Path: "/stats/prometheus",
			}},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "kuadrant-ingressgateway",
				},
			},
		},
	}
}

type ObservabilityReconciler struct {
	*reconcilers.BaseReconciler

	Client     *dynamic.DynamicClient
	restMapper meta.RESTMapper
	namespace  string
}

func NewObservabilityReconciler(client *dynamic.DynamicClient, mgr ctrlruntime.Manager, namespace string) *ObservabilityReconciler {
	return &ObservabilityReconciler{
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
		),
		Client:     client,
		restMapper: mgr.GetRESTMapper(),
		namespace:  namespace,
	}
}

func (r *ObservabilityReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{ReconcileFunc: r.Reconcile, Events: []controller.ResourceEventMatcher{
		{Kind: &kuadrantv1beta1.KuadrantGroupKind},
		{Kind: ptr.To(observability.ServiceMonitorGroupKind), EventType: ptr.To(controller.DeleteEvent)},
		{Kind: ptr.To(observability.PodMonitorGroupKind), EventType: ptr.To(controller.DeleteEvent)},
		{Kind: &machinery.GatewayGroupKind}, // all events
		{Kind: &machinery.GatewayClassGroupKind, EventType: ptr.To(controller.CreateEvent)},
		{Kind: &machinery.GatewayClassGroupKind, EventType: ptr.To(controller.DeleteEvent)},
	},
	}
}

func (r *ObservabilityReconciler) Reconcile(baseCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(baseCtx).WithName("ObservabilityReconciler")
	ctx := logr.NewContext(baseCtx, logger)
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
	kOpMonitor := kOpMonitorBuild(r.namespace)
	err := r.createServiceMonitor(ctx, kOpMonitor, logger)
	if err != nil {
		return err
	}

	// DNS Operator monitor
	dnsOpMonitor := dnsOpMonitorBuild(r.namespace)
	err = r.createServiceMonitor(ctx, dnsOpMonitor, logger)
	if err != nil {
		return err
	}

	// Authorino operator monitor
	authOpMonitor := authOpMonitorBuild(r.namespace)
	err = r.createServiceMonitor(ctx, authOpMonitor, logger)
	if err != nil {
		return err
	}

	// Limitador operator monitor
	limitOpMonitor := limitOpMonitorBuild(r.namespace)
	err = r.createServiceMonitor(ctx, limitOpMonitor, logger)
	if err != nil {
		return err
	}

	// Limitador monitor
	limitMonitor := limitMonitorBuild(kObj.Namespace)
	if err := r.createPodMonitor(ctx, limitMonitor, logger); err != nil {
		return err
	}

	// Create monitors for each gateway instance of each gateway class
	gatewayClasses := topology.Targetables().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == machinery.GatewayClassGroupKind
	})

	var wantedPMs []monitoringv1.PodMonitor
	var monitorsToDelete []machinery.Object

	for _, gatewayClass := range gatewayClasses {
		gateways := topology.All().Children(gatewayClass)
		gwClass := gatewayClass.(*machinery.GatewayClass)
		if lo.Contains(istioGatewayControllerNames, gwClass.Spec.ControllerName) {
			for _, gateway := range gateways {
				istioPodMonitor := istioPodMonitorBuild(gateway.GetNamespace())
				r.createPodMonitor(ctx, istioPodMonitor, logger)
				wantedPMs = append(wantedPMs, *istioPodMonitor)
			}
		} else if lo.Contains(envoyGatewayGatewayControllerNames, gwClass.Spec.ControllerName) {
			for _, gateway := range gateways {
				envoyStatsMonitor := envoyStatsMonitorBuild(gateway.GetNamespace())
				r.createPodMonitor(ctx, envoyStatsMonitor, logger)
				wantedPMs = append(wantedPMs, *envoyStatsMonitor)
			}
		}
	}

	// Check for unwanted monitors
	for _, monitor := range monitorObjs {
		monitorName := monitor.GetName()
		monitorNamespace := monitor.GetNamespace()

		if monitor.GroupVersionKind().Kind == monitoringv1.PodMonitorsKind {
			if !lo.ContainsBy(wantedPMs, func(wanted monitoringv1.PodMonitor) bool {
				return wanted.Name == monitorName && wanted.Namespace == monitorNamespace && wanted.Labels[kuadrant.ObservabilityLabel] == "true"
			}) {
				monitorsToDelete = append(monitorsToDelete, monitor)
			}
		}
	}

	// Delete unwanted monitors
	if len(monitorsToDelete) > 0 {
		for _, monitor := range monitorsToDelete {
			logger.Info("deleting unwanted monitor", "monitors", monitor.GetName(), "namespace", monitor.GetNamespace())
		}
		r.deleteAllMonitors(ctx, monitorsToDelete, logger)
	}

	return nil
}

func (r *ObservabilityReconciler) createServiceMonitor(ctx context.Context, monitor *monitoringv1.ServiceMonitor, logger logr.Logger) error {
	ns := monitor.GetNamespace()
	if ns == "" {
		logger.V(1).Info(fmt.Sprintf("cannot create monitor '%s' as namespace is not set, skipping create", monitor.GetName()))
		return nil
	}

	// Only create, do not mutate if exists.
	// Effectively the controlller does not revert back external changes
	_, err := r.ReconcileResource(ctx, &monitoringv1.ServiceMonitor{}, monitor, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "reconciling service monitor", "key", client.ObjectKeyFromObject(monitor))
		return err
	}

	return nil
}

func (r *ObservabilityReconciler) createPodMonitor(ctx context.Context, monitor *monitoringv1.PodMonitor, logger logr.Logger) error {
	ns := monitor.GetNamespace()
	if ns == "" {
		logger.V(1).Info(fmt.Sprintf("cannot create monitor '%s' as namespace is not set, skipping create", monitor.GetName()))
		return nil
	}

	// Only create, do not mutate if exists.
	// Effectively the controlller does not revert back external changes
	_, err := r.ReconcileResource(ctx, &monitoringv1.PodMonitor{}, monitor, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "reconciling pod monitor", "key", client.ObjectKeyFromObject(monitor))
		return err
	}

	return nil
}

func (r *ObservabilityReconciler) deleteAllMonitors(ctx context.Context, monitorObjs []machinery.Object, logger logr.Logger) {
	for _, monitor := range monitorObjs {
		logger.V(1).Info(fmt.Sprintf("deleting monitor %s %s/%s", monitor.GroupVersionKind().Kind, monitor.GetNamespace(), monitor.GetName()))
		mapping, err := r.restMapper.RESTMapping(monitor.GroupVersionKind().GroupKind())
		if err != nil {
			logger.Error(err, "failed to get monitor restmapping")
			return
		}
		if err = r.Client.Resource(mapping.Resource).Namespace(monitor.GetNamespace()).Delete(ctx, monitor.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, fmt.Sprintf("failed to delete monitor %s %s/%s", monitor.GroupVersionKind().Kind, monitor.GetNamespace(), monitor.GetName()))
			return
		}
		logger.V(1).Info(fmt.Sprintf("deleted monitor %s %s/%s", monitor.GroupVersionKind().Kind, monitor.GetNamespace(), monitor.GetName()))
	}
}
