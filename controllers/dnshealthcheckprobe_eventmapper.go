package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantdnsv1alpha1 "github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

// DNSHealthCheckProbeEventMapper is an EventHandler that maps DNSHealthCheckProbe object events to policy events.
type DNSHealthCheckProbeEventMapper struct {
	Logger logr.Logger
}

func (m *DNSHealthCheckProbeEventMapper) MapToDNSPolicy(_ context.Context, obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(obj, "dnspolicy", common.DNSPolicyBackRefAnnotation)
}

func (m *DNSHealthCheckProbeEventMapper) mapToPolicyRequest(obj client.Object, policyKind string, policyBackRefAnnotationName string) []reconcile.Request {
	logger := m.Logger.V(1).WithValues("object", client.ObjectKeyFromObject(obj))
	probe, ok := obj.(*kuadrantdnsv1alpha1.DNSHealthCheckProbe)
	if !ok {
		logger.Info("mapToPolicyRequest:", "error", fmt.Sprintf("%T is not a *v1alpha1.DNSHealthCheckProbe", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	policyName := common.GetLabel(probe, policyBackRefAnnotationName)
	if policyName == "" {
		return requests
	}
	policyNamespace := common.GetLabel(probe, fmt.Sprintf("%s-namespace", policyBackRefAnnotationName))
	if policyNamespace == "" {
		return requests
	}
	logger.Info("mapToPolicyRequest", policyKind, policyName)
	requests = append(requests, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      policyName,
			Namespace: policyNamespace,
		}})

	return requests
}
