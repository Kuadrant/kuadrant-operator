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
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

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

type RegisteredDataEntry struct {
	Requester  string
	Binding    string
	Expression string
	CAst       *cel.Ast
}

type Subscription struct {
	CAst  *cel.Ast
	Input map[string]any
	Val   ref.Val
}

type RegisteredDataStore struct {
	data          map[string]map[string]RegisteredDataEntry
	subscriptions map[string]Subscription
	mutex         sync.RWMutex
}

func NewRegisteredDataStore() *RegisteredDataStore {
	return &RegisteredDataStore{
		data:          make(map[string]map[string]RegisteredDataEntry),
		subscriptions: make(map[string]Subscription),
	}
}

func (r *RegisteredDataStore) Set(target, requester, binding string, entry RegisteredDataEntry) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	entryKey := fmt.Sprintf("%s#%s", requester, binding)

	if _, exists := r.data[target]; !exists {
		r.data[target] = make(map[string]RegisteredDataEntry)
	}

	r.data[target][entryKey] = entry
}

func (r *RegisteredDataStore) GetAllForTarget(target string) []RegisteredDataEntry {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	entries, exists := r.data[target]
	if !exists || len(entries) == 0 {
		return nil
	}

	result := make([]RegisteredDataEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	return result
}

func (r *RegisteredDataStore) Get(target, requester, binding string) (RegisteredDataEntry, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	entryKey := fmt.Sprintf("%s#%s", requester, binding)

	if targetMap, exists := r.data[target]; exists {
		if entry, entryExists := targetMap[entryKey]; entryExists {
			return entry, true
		}
	}
	return RegisteredDataEntry{}, false
}

func (r *RegisteredDataStore) Exists(target, requester, binding string) bool {
	_, exists := r.Get(target, requester, binding)
	return exists
}

func (r *RegisteredDataStore) Delete(target, requester, binding string) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	entryKey := fmt.Sprintf("%s#%s", requester, binding)

	if targetMap, exists := r.data[target]; exists {
		if _, entryExists := targetMap[entryKey]; entryExists {
			delete(targetMap, entryKey)
			if len(targetMap) == 0 {
				delete(r.data, target)
			}
			return true
		}
	}
	return false
}

func (r *RegisteredDataStore) ClearTarget(target string) int {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if targetMap, exists := r.data[target]; exists {
		count := len(targetMap)
		delete(r.data, target)
		return count
	}
	return 0
}

func (r *RegisteredDataStore) SetSubscription(key string, subscription Subscription) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.subscriptions[key] = subscription
}

func (r *RegisteredDataStore) GetSubscription(key string) (Subscription, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	sub, exists := r.subscriptions[key]
	return sub, exists
}

func (r *RegisteredDataStore) GetAllSubscriptions() map[string]Subscription {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	result := make(map[string]Subscription, len(r.subscriptions))
	maps.Copy(result, r.subscriptions)

	return result
}

func (r *RegisteredDataStore) UpdateSubscriptionValue(key string, newVal ref.Val) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if sub, exists := r.subscriptions[key]; exists {
		sub.Val = newVal
		r.subscriptions[key] = sub
		return true
	}
	return false
}

func (r *RegisteredDataStore) DeleteSubscription(key string) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.subscriptions[key]; exists {
		delete(r.subscriptions, key)
		return true
	}
	return false
}

func (r *RegisteredDataStore) ClearPolicyData(policyKey string) (clearedMutators int, clearedSubscriptions int) {
	clearedMutators = r.ClearTarget(policyKey)
	clearedSubscriptions = r.ClearPolicySubscriptions(policyKey)
	return clearedMutators, clearedSubscriptions
}

func (r *RegisteredDataStore) ClearPolicySubscriptions(policyKey string) int {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	keysToDelete := make([]string, 0)
	keyPrefix := policyKey + "#"
	for key := range r.subscriptions {
		if strings.HasPrefix(key, keyPrefix) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		delete(r.subscriptions, key)
	}

	return len(keysToDelete)
}

func (r *RegisteredDataStore) GetPolicySubscriptions(policyKey string) []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var subscriptionKeys []string
	keyPrefix := policyKey + "#"
	for key := range r.subscriptions {
		if strings.HasPrefix(key, keyPrefix) {
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
	policyKey := fmt.Sprintf("%s/%s/%s", policy.GetObjectKind().GroupVersionKind().Kind, policy.GetNamespace(), policy.GetName())

	registeredEntries := m.store.GetAllForTarget(policyKey)
	if len(registeredEntries) == 0 {
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
	for _, entry := range registeredEntries {
		properties[entry.Binding] = authorinov1beta3.ValueOrSelector{
			Expression: authorinov1beta3.CelExpression(entry.Expression),
		}
	}

	authConfig.Spec.Response.Success.DynamicMetadata["kuadrant"] = authorinov1beta3.SuccessResponseSpec{
		AuthResponseMethodSpec: authorinov1beta3.AuthResponseMethodSpec{
			Json: &authorinov1beta3.JsonAuthResponseSpec{
				Properties: properties,
			},
		},
	}

	return nil
}
