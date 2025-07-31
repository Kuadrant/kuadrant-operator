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
	"maps"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

const KuadrantDataNamespace string = "kuadrant"

type ResourceMutator[TResource any, TPolicy machinery.Policy] interface {
	Mutate(resource TResource, policy TPolicy) error
}

type AuthConfigMutator = ResourceMutator[*authorinov1beta3.AuthConfig, *kuadrantv1.AuthPolicy]

type MutatorRegistry struct {
	authConfigMutators []AuthConfigMutator
	mutex              sync.RWMutex
}

var GlobalMutatorRegistry = &MutatorRegistry{}

func (r *MutatorRegistry) RegisterAuthConfigMutator(mutator AuthConfigMutator) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.authConfigMutators = append(r.authConfigMutators, mutator)
}

func (r *MutatorRegistry) ApplyAuthConfigMutators(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, mutator := range r.authConfigMutators {
		if err := mutator.Mutate(authConfig, policy); err != nil {
			return err
		}
	}
	return nil
}

func ApplyAuthConfigMutators(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
	return GlobalMutatorRegistry.ApplyAuthConfigMutators(authConfig, policy)
}

type PolicyID struct {
	Kind      string
	Namespace string
	Name      string
}

type DataProviderEntry struct {
	Requester  PolicyID
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
	Target    PolicyID
	Requester PolicyID
	Binding   string
}

type SubscriptionKey struct {
	Policy     PolicyID
	Expression string
}

type RegisteredDataStore struct {
	dataProviders map[DataProviderKey]DataProviderEntry
	dataMutex     sync.RWMutex

	subscriptions map[SubscriptionKey]Subscription
	subsMutex     sync.RWMutex
}

func NewRegisteredDataStore() *RegisteredDataStore {
	return &RegisteredDataStore{
		dataProviders: make(map[DataProviderKey]DataProviderEntry),
		subscriptions: make(map[SubscriptionKey]Subscription),
	}
}

func (r *RegisteredDataStore) Set(target, requester PolicyID, binding string, entry DataProviderEntry) {
	key := DataProviderKey{
		Target:    target,
		Requester: requester,
		Binding:   binding,
	}

	r.dataMutex.Lock()
	defer r.dataMutex.Unlock()
	r.dataProviders[key] = entry
}

func (r *RegisteredDataStore) GetAllForTarget(target PolicyID) []DataProviderEntry {
	r.dataMutex.RLock()
	defer r.dataMutex.RUnlock()

	var result []DataProviderEntry
	for key, entry := range r.dataProviders {
		if key.Target == target {
			result = append(result, entry)
		}
	}
	return result
}

func (r *RegisteredDataStore) Get(target, requester PolicyID, binding string) (DataProviderEntry, bool) {
	key := DataProviderKey{
		Target:    target,
		Requester: requester,
		Binding:   binding,
	}

	r.dataMutex.RLock()
	defer r.dataMutex.RUnlock()

	entry, exists := r.dataProviders[key]
	return entry, exists
}

func (r *RegisteredDataStore) Exists(target, requester PolicyID, binding string) bool {
	key := DataProviderKey{
		Target:    target,
		Requester: requester,
		Binding:   binding,
	}

	r.dataMutex.RLock()
	defer r.dataMutex.RUnlock()

	_, exists := r.dataProviders[key]
	return exists
}

func (r *RegisteredDataStore) Delete(target, requester PolicyID, binding string) bool {
	key := DataProviderKey{
		Target:    target,
		Requester: requester,
		Binding:   binding,
	}

	r.dataMutex.Lock()
	defer r.dataMutex.Unlock()

	_, existed := r.dataProviders[key]
	if existed {
		delete(r.dataProviders, key)
	}
	return existed
}

func (r *RegisteredDataStore) SetSubscription(policy PolicyID, expression string, subscription Subscription) {
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

func (r *RegisteredDataStore) GetSubscription(policy PolicyID, expression string) (Subscription, bool) {
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

func (r *RegisteredDataStore) UpdateSubscriptionValue(policy PolicyID, expression string, newVal ref.Val) bool {
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

func (r *RegisteredDataStore) DeleteSubscription(policy PolicyID, expression string) bool {
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

func (r *RegisteredDataStore) ClearPolicyData(policy PolicyID) (clearedMutators int, clearedSubscriptions int) {
	r.dataMutex.Lock()
	r.subsMutex.Lock()
	defer r.dataMutex.Unlock()
	defer r.subsMutex.Unlock()

	// clear data providers
	for key := range r.dataProviders {
		if key.Target == policy {
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
	return clearedMutators, clearedSubscriptions
}

func (r *RegisteredDataStore) GetPolicySubscriptions(policy PolicyID) []SubscriptionKey {
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

type RegisteredDataMutator struct {
	store *RegisteredDataStore
}

func NewRegisteredDataMutator(store *RegisteredDataStore) *RegisteredDataMutator {
	return &RegisteredDataMutator{
		store: store,
	}
}

// Currently this is bespoke, adding data items to the success metadata
func (m *RegisteredDataMutator) Mutate(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
	policyID := PolicyID{
		Kind:      policy.GetObjectKind().GroupVersionKind().Kind,
		Namespace: policy.GetNamespace(),
		Name:      policy.GetName(),
	}

	providerEntries := m.store.GetAllForTarget(policyID)
	if len(providerEntries) == 0 {
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
	for _, entry := range providerEntries {
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
