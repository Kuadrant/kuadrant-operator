package rlptools

import (
	"encoding/json"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

func NewGateways(gwList *gatewayapiv1alpha2.GatewayList,
	rlpKey client.ObjectKey,
	rlpGwKeys []client.ObjectKey) []GatewayWrapper {
	// gateways referenced by the rlp but do not have reference to it in the annotations
	newGateways := make([]GatewayWrapper, 0)
	for idx := range gwList.Items {
		if common.ContainsObjectKey(rlpGwKeys, client.ObjectKeyFromObject(&gwList.Items[idx])) &&
			!(GatewayWrapper{&gwList.Items[idx]}).ContainsRLP(rlpKey) {
			newGateways = append(newGateways, GatewayWrapper{&gwList.Items[idx]})
		}
	}
	return newGateways
}

func SameGateways(gwList *gatewayapiv1alpha2.GatewayList,
	rlpKey client.ObjectKey,
	rlpGwKeys []client.ObjectKey) []GatewayWrapper {
	// gateways referenced by the rlp but also have reference to it in the annotations
	sameGateways := make([]GatewayWrapper, 0)
	for idx := range gwList.Items {
		if common.ContainsObjectKey(rlpGwKeys, client.ObjectKeyFromObject(&gwList.Items[idx])) &&
			(GatewayWrapper{&gwList.Items[idx]}).ContainsRLP(rlpKey) {
			sameGateways = append(sameGateways, GatewayWrapper{&gwList.Items[idx]})
		}
	}

	return sameGateways
}

func LeftGateways(gwList *gatewayapiv1alpha2.GatewayList,
	rlpKey client.ObjectKey,
	rlpGwKeys []client.ObjectKey) []GatewayWrapper {
	// gateways not referenced by the rlp but still have reference in the annotations
	leftGateways := make([]GatewayWrapper, 0)
	for idx := range gwList.Items {
		if !common.ContainsObjectKey(rlpGwKeys, client.ObjectKeyFromObject(&gwList.Items[idx])) &&
			(GatewayWrapper{&gwList.Items[idx]}).ContainsRLP(rlpKey) {
			leftGateways = append(leftGateways, GatewayWrapper{&gwList.Items[idx]})
		}
	}
	return leftGateways
}

// GatewayWrapper add methods to manage RLP references in annotations
type GatewayWrapper struct {
	*gatewayapiv1alpha2.Gateway
}

func (g GatewayWrapper) Key() client.ObjectKey {
	if g.Gateway == nil {
		return client.ObjectKey{}
	}
	return client.ObjectKeyFromObject(g.Gateway)
}

func (g GatewayWrapper) RLPRefs() []client.ObjectKey {
	if g.Gateway == nil {
		return make([]client.ObjectKey, 0)
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation]
	if !ok {
		return make([]client.ObjectKey, 0)
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return make([]client.ObjectKey, 0)
	}

	return refs
}

func (g GatewayWrapper) ContainsRLP(rlpKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation]
	if !ok {
		return false
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	return common.ContainsObjectKey(refs, rlpKey)
}

// AddRLP tries to add RLP to the existing ref list.
// Returns true if RLP was added, false otherwise
func (g GatewayWrapper) AddRLP(rlpKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation]
	if !ok {
		refs := []client.ObjectKey{rlpKey}
		serialized, err := json.Marshal(refs)
		if err != nil {
			return false
		}
		gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation] = string(serialized)
		g.SetAnnotations(gwAnnotations)
		return true
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	if common.ContainsObjectKey(refs, rlpKey) {
		return false
	}

	refs = append(refs, rlpKey)
	serialized, err := json.Marshal(refs)
	if err != nil {
		return false
	}
	gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation] = string(serialized)
	g.SetAnnotations(gwAnnotations)
	return true
}

// DeleteRLP tries to delete RLP from the existing ref list.
// Returns true if RLP was deleted, false otherwise
func (g GatewayWrapper) DeleteRLP(rlpKey client.ObjectKey) bool {
	if g.Gateway == nil {
		return false
	}

	gwAnnotations := g.GetAnnotations()
	if gwAnnotations == nil {
		gwAnnotations = map[string]string{}
	}

	val, ok := gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation]
	if !ok {
		return false
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(val), &refs)
	if err != nil {
		return false
	}

	if refID := common.FindObjectKey(refs, rlpKey); refID != len(refs) {
		// remove index
		refs = append(refs[:refID], refs[refID+1:]...)
		serialized, err := json.Marshal(refs)
		if err != nil {
			return false
		}
		gwAnnotations[common.KuadrantRateLimitPolicyRefAnnotation] = string(serialized)
		g.SetAnnotations(gwAnnotations)
		return true
	}

	return false
}

// Hostnames builds a list of hostnames from the listeners.
func (g GatewayWrapper) Hostnames() []string {
	hostnames := make([]string, 0)
	if g.Gateway == nil {
		return hostnames
	}

	for idx := range g.Spec.Listeners {
		if g.Spec.Listeners[idx].Hostname != nil {
			hostnames = append(hostnames, string(*g.Spec.Listeners[idx].Hostname))
		}
	}

	return hostnames
}
