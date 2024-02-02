package controllers

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func NewDNSHealthCheckProbeEventMapper(o ...mappers.MapperOption) mappers.EventMapper {
	return &DNSHealthCheckProbeEventMapper{opts: mappers.Apply(o...)}
}

// DNSHealthCheckProbeEventMapper is an EventHandler that maps DNSHealthCheckProbe object events to policy events.
type DNSHealthCheckProbeEventMapper struct {
	opts mappers.MapperOptions
}

func (m *DNSHealthCheckProbeEventMapper) MapToPolicy(obj client.Object, policyKind utils.Referrer) []reconcile.Request {
	logger := m.opts.Logger.V(1).WithValues("object", client.ObjectKeyFromObject(obj))
	probe, ok := obj.(*kuadrantdnsv1alpha1.DNSHealthCheckProbe)
	if !ok {
		logger.Info("mapToPolicyRequest:", "error", fmt.Sprintf("%T is not a *v1alpha1.DNSHealthCheckProbe", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	policyName := common.GetLabel(probe, policyKind.DirectReferenceAnnotationName())
	if policyName == "" {
		return requests
	}
	policyNamespace := common.GetLabel(probe, fmt.Sprintf("%s-namespace", policyKind.DirectReferenceAnnotationName()))
	if policyNamespace == "" {
		return requests
	}
	logger.Info("mapToPolicyRequest", policyKind.Kind(), policyName)
	requests = append(requests, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      policyName,
			Namespace: policyNamespace,
		}})

	return requests
}
