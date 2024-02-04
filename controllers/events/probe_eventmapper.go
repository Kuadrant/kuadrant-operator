package events

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantdnsv1alpha1 "github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/controllers/metadata"
)

// ProbeEventMapper is an EventHandler that maps DNSHealthCheckProbe object events to policy events.
type ProbeEventMapper struct {
	Logger     logr.Logger
	PolicyKind string
	PolicyRef  string
}

func (p *ProbeEventMapper) MapToPolicy(_ context.Context, obj client.Object) []reconcile.Request {
	return p.mapToPolicyRequest(obj, p.PolicyRef, p.PolicyKind)
}

func NewProbeEventMapper(logger logr.Logger, policyRef, policyKind string) *ProbeEventMapper {
	return &ProbeEventMapper{
		Logger:     logger.WithName("GatewayEventMapper"),
		PolicyKind: policyKind,
		PolicyRef:  policyRef,
	}
}

func (p *ProbeEventMapper) mapToPolicyRequest(obj client.Object, policyRef, policyKind string) []reconcile.Request {
	logger := p.Logger.V(3).WithValues("object", client.ObjectKeyFromObject(obj))
	probe, ok := obj.(*kuadrantdnsv1alpha1.DNSHealthCheckProbe)
	if !ok {
		logger.Info("mapToPolicyRequest:", "error", fmt.Sprintf("%T is not a *v1alpha1.DNSHealthCheckProbe", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	policyName := metadata.GetLabel(probe, policyRef)
	if policyName == "" {
		return requests
	}
	policyNamespace := metadata.GetLabel(probe, fmt.Sprintf("%s-namespace", policyRef))
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
