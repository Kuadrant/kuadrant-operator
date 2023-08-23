package controllers

import (
	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AuthConfigEventMapper is an EventHandler that maps AuthConfig objects events to Policy events.
type AuthConfigEventMapper struct {
	Logger logr.Logger
}

func (m *AuthConfigEventMapper) MapToAuthPolicy(obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(obj, "authpolicy", common.AuthPoliciesBackRefAnnotation)
}

func (m *AuthConfigEventMapper) mapToPolicyRequest(obj client.Object, policyKind string, policyBackRefAnnotationName string) []reconcile.Request {
	policyRef, found := common.ReadAnnotationsFromObject(obj)[policyBackRefAnnotationName]
	if !found {
		return []reconcile.Request{}
	}

	policyKey := common.NamespacedNameToObjectKey(policyRef, obj.GetNamespace())

	m.Logger.V(1).Info("Processing object", "object", client.ObjectKeyFromObject(obj), policyKind, policyKey)
	return []reconcile.Request{{NamespacedName: policyKey}}
}
