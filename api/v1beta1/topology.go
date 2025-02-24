package v1beta1

import (
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
	observability "github.com/kuadrant/kuadrant-operator/pkg/observability"
)

var (
	LimitadorGroupKind = schema.GroupKind{Group: limitadorv1alpha1.GroupVersion.Group, Kind: "Limitador"}
	AuthorinoGroupKind = schema.GroupKind{Group: authorinooperatorv1beta1.GroupVersion.Group, Kind: "Authorino"}

	LimitadorsResource = limitadorv1alpha1.GroupVersion.WithResource("limitadors")
	AuthorinosResource = authorinooperatorv1beta1.GroupVersion.WithResource("authorinos")
)

func LinkKuadrantToGatewayClasses(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[*Kuadrant])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   schema.GroupKind{Group: gatewayapiv1.GroupVersion.Group, Kind: "GatewayClass"},
		Func: func(_ machinery.Object) []machinery.Object {
			parents := make([]machinery.Object, len(kuadrants))
			for _, parent := range kuadrants {
				parents = append(parents, parent)
			}
			return parents
		},
	}
}

func LinkKuadrantToLimitador(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   LimitadorGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.Filter(kuadrants, func(k machinery.Object, _ int) bool {
				return k.GetNamespace() == child.GetNamespace() && child.GetName() == "limitador"
			})
		},
	}
}

func LinkKuadrantToAuthorino(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   AuthorinoGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.Filter(kuadrants, func(k machinery.Object, _ int) bool {
				return k.GetNamespace() == child.GetNamespace() && child.GetName() == "authorino"
			})
		},
	}
}

func LinkKuadrantToServiceMonitor(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   observability.ServiceMonitorGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.Filter(kuadrants, func(_ machinery.Object, _ int) bool {
				if metaObj, ok := child.(metav1.Object); ok {
					if val, exists := metaObj.GetLabels()[kuadrant.ObservabilityLabel]; exists {
						return val == "true"
					}
				}
				return false
			})
		},
	}
}

func LinkKuadrantToPodMonitor(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   observability.PodMonitorGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.Filter(kuadrants, func(_ machinery.Object, _ int) bool {
				if metaObj, ok := child.(metav1.Object); ok {
					if val, exists := metaObj.GetLabels()[kuadrant.ObservabilityLabel]; exists {
						return val == "true"
					}
				}
				return false
			})
		},
	}
}
