package reconcilers

import (
	"encoding/json"

	"github.com/kuadrant/kuadrant-operator/pkg/library/common"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// GatewayWrapper wraps a Gateway API Gateway adding methods and configs to manage policy references in annotations
type GatewayWrapper struct {
	*gatewayapiv1beta1.Gateway
	common.Referrer
}

func (g GatewayWrapper) Key() client.ObjectKey {
	if g.Gateway == nil {
		return client.ObjectKey{}
	}
	return client.ObjectKeyFromObject(g.Gateway)
}

func (g GatewayWrapper) ContainsPolicy(policyKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}
	refs := common.BackReferencesFromObject(g.Gateway, g.Referrer)
	return slices.Contains(refs, policyKey)
}

// AddPolicy tries to add a policy to the existing ref list.
// Returns true if policy was added, false otherwise
func (g GatewayWrapper) AddPolicy(policyKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	// annotation exists and contains a back reference to the policy → nothing to do
	if g.ContainsPolicy(policyKey) {
		return false
	}

	gwAnnotations := common.ReadAnnotationsFromObject(g)
	_, annotationFound := gwAnnotations[g.BackReferenceAnnotationName()]

	// annotation does not exist → create it
	if !annotationFound {
		refs := []client.ObjectKey{policyKey}
		serialized, err := json.Marshal(refs)
		if err != nil {
			return false
		}
		gwAnnotations[g.BackReferenceAnnotationName()] = string(serialized)
		g.SetAnnotations(gwAnnotations)
		return true
	}

	// annotation exists and does not contain a back reference to the policy → add the policy to it
	refs := append(common.BackReferencesFromObject(g.Gateway, g.Referrer), policyKey)
	serialized, err := json.Marshal(refs)
	if err != nil {
		return false
	}
	gwAnnotations[g.BackReferenceAnnotationName()] = string(serialized)
	g.SetAnnotations(gwAnnotations)
	return true
}

// DeletePolicy tries to delete a policy from the existing ref list.
// Returns true if the policy was deleted, false otherwise
func (g GatewayWrapper) DeletePolicy(policyKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := common.ReadAnnotationsFromObject(g)

	// annotation does not exist → nothing to do
	refsAsStr, annotationFound := gwAnnotations[g.BackReferenceAnnotationName()]
	if !annotationFound {
		return false
	}

	var refs []client.ObjectKey
	err := json.Unmarshal([]byte(refsAsStr), &refs)
	if err != nil {
		return false
	}

	// annotation exists and contains a back reference to the policy → remove the policy from it
	if idx := slices.Index(refs, policyKey); idx >= 0 {
		refs = append(refs[:idx], refs[idx+1:]...)
		serialized, err := json.Marshal(refs)
		if err != nil {
			return false
		}
		gwAnnotations[g.BackReferenceAnnotationName()] = string(serialized)
		g.SetAnnotations(gwAnnotations)
		return true
	}

	// annotation exists and does not contain a back reference the policy → nothing to do
	return false
}

// Hostnames builds a list of hostnames from the listeners.
func (g GatewayWrapper) Hostnames() []gatewayapiv1beta1.Hostname {
	hostnames := make([]gatewayapiv1beta1.Hostname, 0)
	if g.Gateway == nil {
		return hostnames
	}

	for idx := range g.Spec.Listeners {
		if g.Spec.Listeners[idx].Hostname != nil {
			hostnames = append(hostnames, *g.Spec.Listeners[idx].Hostname)
		}
	}

	return hostnames
}

// GatewayWrapperList is a list of GatewayWrappers that implements sort.Interface
type GatewayWrapperList []GatewayWrapper

func (g GatewayWrapperList) Len() int {
	return len(g)
}

func (g GatewayWrapperList) Less(i, j int) bool {
	return g[i].CreationTimestamp.Before(&g[j].CreationTimestamp)
}

func (g GatewayWrapperList) Swap(i, j int) {
	g[i], g[j] = g[j], g[i]
}
