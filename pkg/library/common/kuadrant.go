package common

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// TODO: Does this belong here ? TargetRefReconciler GetAllGatewayPolicyRefs func depends on this
const (
	KuadrantNamespaceLabel = "kuadrant.io/namespace"
)

type KuadrantPolicy interface {
	client.Object
	GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference
	GetWrappedNamespace() gatewayapiv1beta1.Namespace
	GetRulesHostnames() []string
}

func IsKuadrantManaged(obj client.Object) bool {
	_, isSet := obj.GetAnnotations()[KuadrantNamespaceLabel]
	return isSet
}
