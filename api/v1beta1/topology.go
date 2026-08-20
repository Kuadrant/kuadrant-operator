package v1beta1

import (
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	observability "github.com/kuadrant/kuadrant-operator/internal/observability"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

var (
	LimitadorGroupKind = schema.GroupKind{Group: limitadorv1alpha1.GroupVersion.Group, Kind: "Limitador"}
	AuthorinoGroupKind = schema.GroupKind{Group: authorinooperatorv1beta1.GroupVersion.Group, Kind: "Authorino"}

	LimitadorsResource = limitadorv1alpha1.GroupVersion.WithResource("limitadors")
	AuthorinosResource = authorinooperatorv1beta1.GroupVersion.WithResource("authorinos")

	DeploymentGroupKind = appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind()
	DeploymentsResource = appsv1.SchemeGroupVersion.WithResource("deployments")

	NetworkPolicyGroupKind = schema.GroupKind{Group: networkingv1.SchemeGroupVersion.Group, Kind: "NetworkPolicy"}
	NetworkPolicyResource  = networkingv1.SchemeGroupVersion.WithResource("networkpolicies")
)

func LinkKuadrantToGatewayClasses(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantGroupKind), controller.ObjectAs[*Kuadrant])

	return machinery.LinkFunc{
		From: KuadrantGroupKind,
		To:   schema.GroupKind{Group: gatewayapiv1.GroupVersion.Group, Kind: "GatewayClass"},
		Func: func(_ machinery.Object) []machinery.Object {
			parents := make([]machinery.Object, 0, len(kuadrants))
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

func LinkAuthorinoToDeployment(objs controller.Store) machinery.LinkFunc {
	authorinos := utils.Map(objs.FilterByGroupKind(AuthorinoGroupKind), ControllerObjectToMachineryObject)

	return machinery.LinkFunc{
		From: AuthorinoGroupKind,
		To:   DeploymentGroupKind,
		Func: func(deployment machinery.Object) []machinery.Object {
			return lo.Filter(authorinos, func(authorino machinery.Object, _ int) bool {
				// the name of the deployment is hardcoded. This deployment is owned by the authorino operator.
				return authorino.GetNamespace() == deployment.GetNamespace() && deployment.GetName() == "authorino"
			})
		},
	}
}

func LinkLimitadorToDeployment(objs controller.Store) machinery.LinkFunc {
	limitadors := utils.Map(objs.FilterByGroupKind(LimitadorGroupKind), ControllerObjectToMachineryObject)

	return machinery.LinkFunc{
		From: LimitadorGroupKind,
		To:   DeploymentGroupKind,
		Func: func(deployment machinery.Object) []machinery.Object {
			return lo.Filter(limitadors, func(limitador machinery.Object, _ int) bool {
				// the name of the deployment is hardcoded. This deployment is owned by the limitador operator.
				// This Link is used to inject pod template label to the deployment.
				// labels propagation pattern would be more reliable as the kuadrant operator would be owning these labels
				return limitador.GetNamespace() == deployment.GetNamespace() && deployment.GetName() == "limitador-limitador"
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

func LinkAuthorinoToNetworkPolicy(objs controller.Store) machinery.LinkFunc {
	authorinos := utils.Map(objs.FilterByGroupKind(AuthorinoGroupKind), ControllerObjectToMachineryObject)

	return machinery.LinkFunc{
		From: AuthorinoGroupKind,
		To:   NetworkPolicyGroupKind,
		Func: func(networkPolicy machinery.Object) []machinery.Object {
			return lo.Filter(authorinos, func(authorino machinery.Object, _ int) bool {
				return authorino.GetNamespace() == networkPolicy.GetNamespace() && networkPolicy.GetName() == "kuadrant-authorino"
			})
		},
	}
}

func LinkLimitadorToNetworkPolicy(objs controller.Store) machinery.LinkFunc {
	limitadors := utils.Map(objs.FilterByGroupKind(LimitadorGroupKind), ControllerObjectToMachineryObject)

	return machinery.LinkFunc{
		From: LimitadorGroupKind,
		To:   NetworkPolicyGroupKind,
		Func: func(networkPolicy machinery.Object) []machinery.Object {
			return lo.Filter(limitadors, func(limitador machinery.Object, _ int) bool {
				return limitador.GetNamespace() == networkPolicy.GetNamespace() && networkPolicy.GetName() == "kuadrant-limitador"
			})
		},
	}
}

func ControllerObjectToMachineryObject(cObj controller.Object) machinery.Object {
	if mObj, ok := cObj.(machinery.Object); ok {
		return mObj
	}
	return &controller.RuntimeObject{Object: cObj}
}
