/*
Copyright 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package extension

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantmachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

const KuadrantDataNamespace string = "kuadrant"

type ResourceMutator[TResource any, TTargetRefs []machinery.PolicyTargetReference] interface {
	Mutate(resource TResource, targetRefs TTargetRefs) error
}

type AuthConfigMutator = ResourceMutator[*authorinov1beta3.AuthConfig, []machinery.PolicyTargetReference]
type WasmConfigMutator = ResourceMutator[*wasm.Config, []machinery.PolicyTargetReference]

type MutatorRegistry struct {
	authConfigMutators []AuthConfigMutator
	wasmConfigMutators []WasmConfigMutator
	mutex              sync.RWMutex
}

var GlobalMutatorRegistry = &MutatorRegistry{}

func (r *MutatorRegistry) RegisterAuthConfigMutator(mutator AuthConfigMutator) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.authConfigMutators = append(r.authConfigMutators, mutator)
}

func (r *MutatorRegistry) RegisterWasmConfigMutator(mutator WasmConfigMutator) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.wasmConfigMutators = append(r.wasmConfigMutators, mutator)
}

func (r *MutatorRegistry) ApplyAuthConfigMutators(authConfig *authorinov1beta3.AuthConfig, path []machinery.Targetable) error {
	_, gateway, _, httpRoute, _, err := kuadrantmachinery.ObjectsInRequestPath(path)
	if err != nil {
		return err
	}

	targetRefs := []machinery.PolicyTargetReference{
		// HTTPRoute - for extension policies targeting this specific route
		machinery.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReferenceWithSectionName: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: gatewayapiv1alpha2.Group("gateway.networking.k8s.io"),
					Kind:  gatewayapiv1alpha2.Kind("HTTPRoute"),
					Name:  gatewayapiv1alpha2.ObjectName(httpRoute.GetName()),
				},
			},
			PolicyNamespace: httpRoute.GetNamespace(),
		},
		// Gateway - for extension policies targeting the parent gateway
		machinery.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReferenceWithSectionName: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: gatewayapiv1alpha2.Group("gateway.networking.k8s.io"),
					Kind:  gatewayapiv1alpha2.Kind("Gateway"),
					Name:  gatewayapiv1alpha2.ObjectName(gateway.GetName()),
				},
			},
			PolicyNamespace: gateway.GetNamespace(),
		},
	}

	return r.applyMutatorsWithTargetRefs(authConfig, targetRefs)
}

func (r *MutatorRegistry) applyMutatorsWithTargetRefs(authConfig *authorinov1beta3.AuthConfig, targetRefs []machinery.PolicyTargetReference) error {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, mutator := range r.authConfigMutators {
		if err := mutator.Mutate(authConfig, targetRefs); err != nil {
			return err
		}
	}
	return nil
}

func (r *MutatorRegistry) ApplyWasmConfigMutators(wasmConfig *wasm.Config, gateway *machinery.Gateway) error {
	// Create target ref for the gateway
	targetRefs := []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReferenceWithSectionName: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: gatewayapiv1alpha2.Group("gateway.networking.k8s.io"),
					Kind:  gatewayapiv1alpha2.Kind("Gateway"),
					Name:  gatewayapiv1alpha2.ObjectName(gateway.GetName()),
				},
			},
			PolicyNamespace: gateway.GetNamespace(),
		},
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, mutator := range r.wasmConfigMutators {
		if err := mutator.Mutate(wasmConfig, targetRefs); err != nil {
			return err
		}
	}
	return nil
}

// ApplyWasmConfigMutators applies all registered wasm config mutators for a specific gateway
func ApplyWasmConfigMutators(wasmConfig *wasm.Config, gateway *machinery.Gateway) error {
	return GlobalMutatorRegistry.ApplyWasmConfigMutators(wasmConfig, gateway)
}

// ApplyAuthConfigMutators applies all registered auth config mutators to an auth config
func ApplyAuthConfigMutators(authConfig *authorinov1beta3.AuthConfig, path []machinery.Targetable) error {
	return GlobalMutatorRegistry.ApplyAuthConfigMutators(authConfig, path)
}

type ResourceID struct {
	Kind      string
	Namespace string
	Name      string
}

type DataProviderEntry struct {
	Policy     ResourceID
	Binding    string
	Expression string
	CAst       *cel.Ast
}

type Subscription struct {
	CAst       *cel.Ast
	Input      map[string]any
	Val        ref.Val
	PolicyKind string
}

type DataProviderKey struct {
	Policy           ResourceID
	TargetRefLocator string
	Domain           extpb.Domain
	Binding          string
}

type SubscriptionKey struct {
	Policy     ResourceID
	Expression string
}

type RegisteredUpstreamKey struct {
	Policy ResourceID
	URL    string
}

type RegisteredUpstreamEntry struct {
	ClusterName string
	Host        string
	Port        int
	TargetRef   TargetRef
	FailureMode string
	Timeout     string
}

type TargetRef struct {
	Group     string
	Kind      string
	Name      string
	Namespace string
}

type RegisteredDataStore struct {
	dataProviders map[DataProviderKey]DataProviderEntry
	dataMutex     sync.RWMutex

	subscriptions map[SubscriptionKey]Subscription
	subsMutex     sync.RWMutex

	registeredUpstreams map[RegisteredUpstreamKey]RegisteredUpstreamEntry
	upstreamsMutex      sync.RWMutex
}

func NewRegisteredDataStore() *RegisteredDataStore {
	return &RegisteredDataStore{
		dataProviders:       make(map[DataProviderKey]DataProviderEntry),
		subscriptions:       make(map[SubscriptionKey]Subscription),
		registeredUpstreams: make(map[RegisteredUpstreamKey]RegisteredUpstreamEntry),
	}
}

func (r *RegisteredDataStore) Set(policy ResourceID, targetRefLocator string, domain extpb.Domain, binding string, entry DataProviderEntry) {
	key := DataProviderKey{
		Policy:           policy,
		TargetRefLocator: targetRefLocator,
		Domain:           domain,
		Binding:          binding,
	}

	r.dataMutex.Lock()
	defer r.dataMutex.Unlock()
	r.dataProviders[key] = entry
}

func (r *RegisteredDataStore) GetAllForTargetRef(targetRefLocator string, domain extpb.Domain) []DataProviderEntry {
	r.dataMutex.RLock()
	defer r.dataMutex.RUnlock()

	var result []DataProviderEntry
	for key, entry := range r.dataProviders {
		if key.TargetRefLocator == targetRefLocator && key.Domain == domain {
			result = append(result, entry)
		}
	}
	return result
}

func (r *RegisteredDataStore) Get(policy ResourceID, targetRefLocator string, domain extpb.Domain, binding string) (DataProviderEntry, bool) {
	key := DataProviderKey{
		Policy:           policy,
		TargetRefLocator: targetRefLocator,
		Domain:           domain,
		Binding:          binding,
	}

	r.dataMutex.RLock()
	defer r.dataMutex.RUnlock()

	entry, exists := r.dataProviders[key]
	return entry, exists
}

func (r *RegisteredDataStore) Exists(policy ResourceID, targetRefLocator string, domain extpb.Domain, binding string) bool {
	key := DataProviderKey{
		Policy:           policy,
		TargetRefLocator: targetRefLocator,
		Domain:           domain,
		Binding:          binding,
	}

	r.dataMutex.RLock()
	defer r.dataMutex.RUnlock()

	_, exists := r.dataProviders[key]
	return exists
}

func (r *RegisteredDataStore) Delete(policy ResourceID, targetRefLocator string, domain extpb.Domain, binding string) bool {
	key := DataProviderKey{
		Policy:           policy,
		TargetRefLocator: targetRefLocator,
		Domain:           domain,
		Binding:          binding,
	}

	r.dataMutex.Lock()
	defer r.dataMutex.Unlock()

	_, existed := r.dataProviders[key]
	if existed {
		delete(r.dataProviders, key)
	}
	return existed
}

func (r *RegisteredDataStore) SetSubscription(policy ResourceID, expression string, subscription Subscription) {
	key := SubscriptionKey{
		Policy:     policy,
		Expression: expression,
	}

	r.subsMutex.Lock()
	defer r.subsMutex.Unlock()
	r.subscriptions[key] = subscription
}

func (r *RegisteredDataStore) GetSubscriptionsForPolicyKind(policyKind string) map[SubscriptionKey]Subscription {
	r.subsMutex.RLock()
	defer r.subsMutex.RUnlock()

	result := make(map[SubscriptionKey]Subscription)
	for key, sub := range r.subscriptions {
		if sub.PolicyKind == policyKind {
			result[key] = sub
		}
	}
	return result
}

func (r *RegisteredDataStore) GetSubscription(policy ResourceID, expression string) (Subscription, bool) {
	key := SubscriptionKey{
		Policy:     policy,
		Expression: expression,
	}

	r.subsMutex.RLock()
	defer r.subsMutex.RUnlock()

	sub, exists := r.subscriptions[key]
	return sub, exists
}

func (r *RegisteredDataStore) GetAllSubscriptions() map[SubscriptionKey]Subscription {
	r.subsMutex.RLock()
	defer r.subsMutex.RUnlock()

	result := make(map[SubscriptionKey]Subscription, len(r.subscriptions))
	maps.Copy(result, r.subscriptions)

	return result
}

func (r *RegisteredDataStore) UpdateSubscriptionValue(policy ResourceID, expression string, newVal ref.Val) bool {
	key := SubscriptionKey{
		Policy:     policy,
		Expression: expression,
	}

	r.subsMutex.Lock()
	defer r.subsMutex.Unlock()

	if sub, exists := r.subscriptions[key]; exists {
		sub.Val = newVal
		r.subscriptions[key] = sub
		return true
	}
	return false
}

func (r *RegisteredDataStore) DeleteSubscription(policy ResourceID, expression string) bool {
	key := SubscriptionKey{
		Policy:     policy,
		Expression: expression,
	}

	r.subsMutex.Lock()
	defer r.subsMutex.Unlock()

	_, existed := r.subscriptions[key]
	if existed {
		delete(r.subscriptions, key)
	}
	return existed
}

func (r *RegisteredDataStore) SetUpstream(key RegisteredUpstreamKey, entry RegisteredUpstreamEntry) {
	r.upstreamsMutex.Lock()
	defer r.upstreamsMutex.Unlock()
	r.registeredUpstreams[key] = entry
}

func (r *RegisteredDataStore) GetUpstream(key RegisteredUpstreamKey) (RegisteredUpstreamEntry, bool) {
	r.upstreamsMutex.RLock()
	defer r.upstreamsMutex.RUnlock()
	entry, exists := r.registeredUpstreams[key]
	return entry, exists
}

func (r *RegisteredDataStore) GetAllUpstreams() map[RegisteredUpstreamKey]RegisteredUpstreamEntry {
	r.upstreamsMutex.RLock()
	defer r.upstreamsMutex.RUnlock()
	result := make(map[RegisteredUpstreamKey]RegisteredUpstreamEntry, len(r.registeredUpstreams))
	maps.Copy(result, r.registeredUpstreams)
	return result
}

func (r *RegisteredDataStore) DeleteUpstream(key RegisteredUpstreamKey) bool {
	r.upstreamsMutex.Lock()
	defer r.upstreamsMutex.Unlock()
	_, existed := r.registeredUpstreams[key]
	if existed {
		delete(r.registeredUpstreams, key)
	}
	return existed
}

func (r *RegisteredDataStore) ClearPolicyData(policy ResourceID) (clearedMutators int, clearedSubscriptions int, clearedUpstreams int) {
	r.dataMutex.Lock()
	r.subsMutex.Lock()
	r.upstreamsMutex.Lock()
	defer r.dataMutex.Unlock()
	defer r.subsMutex.Unlock()
	defer r.upstreamsMutex.Unlock()

	// clear data providers
	for key := range r.dataProviders {
		if key.Policy == policy {
			delete(r.dataProviders, key)
			clearedMutators++
		}
	}

	// clear subscriptions
	for key := range r.subscriptions {
		if key.Policy == policy {
			delete(r.subscriptions, key)
			clearedSubscriptions++
		}
	}

	// clear registered upstreams
	for key := range r.registeredUpstreams {
		if key.Policy == policy {
			delete(r.registeredUpstreams, key)
			clearedUpstreams++
		}
	}

	return clearedMutators, clearedSubscriptions, clearedUpstreams
}

func (r *RegisteredDataStore) GetPolicySubscriptions(policy ResourceID) []SubscriptionKey {
	r.subsMutex.RLock()
	defer r.subsMutex.RUnlock()

	var subscriptionKeys []SubscriptionKey
	for key := range r.subscriptions {
		if key.Policy == policy {
			subscriptionKeys = append(subscriptionKeys, key)
		}
	}
	return subscriptionKeys
}

type RegisteredDataMutator[TResource any] struct {
	store *RegisteredDataStore
}

func NewRegisteredDataMutator[TResource any](store *RegisteredDataStore) *RegisteredDataMutator[TResource] {
	return &RegisteredDataMutator[TResource]{
		store: store,
	}
}

// Mutate handles registered data mutations for different resource types
func (m *RegisteredDataMutator[TResource]) Mutate(resource TResource, targetRefs []machinery.PolicyTargetReference) error {
	switch r := any(resource).(type) {
	case *authorinov1beta3.AuthConfig:
		return m.mutateAuthConfig(r, targetRefs)
	case *wasm.Config:
		return m.mutateWasmConfig(r, targetRefs)
	default:
		return fmt.Errorf("unsupported resource type: %T", resource)
	}
}

// mutateAuthConfig handles AuthConfig-specific mutations
func (m *RegisteredDataMutator[TResource]) mutateAuthConfig(authConfig *authorinov1beta3.AuthConfig, targetRefs []machinery.PolicyTargetReference) error {
	var allProviderEntries []DataProviderEntry

	// Find mutations for each target reference
	for _, targetRef := range targetRefs {
		providerEntries := m.store.GetAllForTargetRef(targetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH)
		allProviderEntries = append(allProviderEntries, providerEntries...)
	}

	if len(allProviderEntries) == 0 {
		return nil
	}

	if authConfig.Spec.Response == nil {
		authConfig.Spec.Response = &authorinov1beta3.ResponseSpec{
			Success: authorinov1beta3.WrappedSuccessResponseSpec{
				DynamicMetadata: make(map[string]authorinov1beta3.SuccessResponseSpec),
			},
		}
	} else if authConfig.Spec.Response.Success.DynamicMetadata == nil {
		authConfig.Spec.Response.Success.DynamicMetadata = make(map[string]authorinov1beta3.SuccessResponseSpec)
	}

	properties := make(map[string]authorinov1beta3.ValueOrSelector)
	for _, entry := range allProviderEntries {
		properties[entry.Binding] = authorinov1beta3.ValueOrSelector{
			Expression: authorinov1beta3.CelExpression(entry.Expression),
		}
	}

	authConfig.Spec.Response.Success.DynamicMetadata[KuadrantDataNamespace] = authorinov1beta3.SuccessResponseSpec{
		AuthResponseMethodSpec: authorinov1beta3.AuthResponseMethodSpec{
			Json: &authorinov1beta3.JsonAuthResponseSpec{
				Properties: properties,
			},
		},
	}
	return nil
}

// mutateWasmConfig handles WasmConfig-specific mutations
func (m *RegisteredDataMutator[TResource]) mutateWasmConfig(wasmConfig *wasm.Config, targetRefs []machinery.PolicyTargetReference) error {
	requestData := make(map[string]string)

	for _, targetRef := range targetRefs {
		providerEntries := m.store.GetAllForTargetRef(targetRef.GetLocator(), extpb.Domain_DOMAIN_REQUEST)

		for _, entry := range providerEntries {
			// Add if it doesn't exist
			if _, exists := requestData[entry.Binding]; !exists {
				requestData[entry.Binding] = entry.Expression
			}
		}
	}

	wasmConfig.RequestData = requestData

	// Inject registered upstream services
	allUpstreams := m.store.GetAllUpstreams()
	for _, entry := range allUpstreams {
		timeout := entry.Timeout
		svc := wasm.Service{
			Endpoint:    entry.ClusterName,
			Type:        wasm.AuthServiceType,
			FailureMode: wasm.FailureModeType(entry.FailureMode),
			Timeout:     &timeout,
		}
		wasmServiceKey := "ext-" + HashUpstreamServiceConfig(svc)
		if wasmConfig.Services == nil {
			wasmConfig.Services = make(map[string]wasm.Service)
		}
		wasmConfig.Services[wasmServiceKey] = svc
	}

	return nil
}

// HashUpstreamServiceConfig produces a deterministic short hash from a wasm.Service
// config. Identical configurations produce the same hash, providing natural deduplication.
func HashUpstreamServiceConfig(svc wasm.Service) string {
	timeout := ""
	if svc.Timeout != nil {
		timeout = *svc.Timeout
	}
	data := fmt.Sprintf("%s|%s|%s|%s", svc.Type, svc.Endpoint, svc.FailureMode, timeout)
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h[:8])
}

// GetRegisteredUpstreamsByTargetRef returns registered upstreams matching the given targetRef,
// aggregated across all extension data stores in the GlobalMutatorRegistry.
func GetRegisteredUpstreamsByTargetRef(targetRef TargetRef) []RegisteredUpstreamEntry {
	GlobalMutatorRegistry.mutex.RLock()
	defer GlobalMutatorRegistry.mutex.RUnlock()

	var result []RegisteredUpstreamEntry
	for _, mutator := range GlobalMutatorRegistry.wasmConfigMutators {
		if m, ok := mutator.(*RegisteredDataMutator[*wasm.Config]); ok {
			m.store.upstreamsMutex.RLock()
			for _, entry := range m.store.registeredUpstreams {
				if entry.TargetRef == targetRef {
					result = append(result, entry)
				}
			}
			m.store.upstreamsMutex.RUnlock()
		}
	}
	return result
}
