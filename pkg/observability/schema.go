package observability

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	ServiceMonitorsResource = monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")
	PodMonitorsResource     = monitoringv1.SchemeGroupVersion.WithResource("podmonitors")

	ServiceMonitorGroupKind = schema.GroupKind{Group: monitoringv1.SchemeGroupVersion.Group, Kind: monitoringv1.ServiceMonitorsKind}
	PodMonitorGroupKind     = schema.GroupKind{Group: monitoringv1.SchemeGroupVersion.Group, Kind: monitoringv1.PodMonitorsKind}
)
