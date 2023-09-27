package common

import "sigs.k8s.io/controller-runtime/pkg/client"

// TODO: Does this belong here ? TargetRefReconciler GetAllGatewayPolicyRefs func depends on this
const (
	KuadrantNamespaceLabel = "kuadrant.io/namespace"
)

func IsKuadrantManaged(obj client.Object) bool {
	_, isSet := obj.GetAnnotations()[KuadrantNamespaceLabel]
	return isSet
}
