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
	"sort"

	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"google.golang.org/protobuf/types/descriptorpb"
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
	parsed, err := kuadrantmachinery.ParseTopologyPath(path)
	if err != nil {
		return err
	}

	var routeKind string
	switch parsed.RouteType {
	case kuadrantmachinery.RouteTypeHTTP:
		routeKind = "HTTPRoute"
	case kuadrantmachinery.RouteTypeGRPC:
		routeKind = "GRPCRoute"
	default:
		return fmt.Errorf("unsupported route type: %s", parsed.RouteType)
	}

	targetRefs := []machinery.PolicyTargetReference{
		policyTargetRef("Gateway", parsed.Gateway.GetName(), parsed.Gateway.GetNamespace()),
		policyTargetRef(routeKind, parsed.GetRouteName(), parsed.GetRouteNamespace()),
	}

	return r.applyMutatorsWithTargetRefs(authConfig, targetRefs)
}

func policyTargetRef(kind, name, namespace string) machinery.PolicyTargetReference {
	return machinery.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReferenceWithSectionName: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group("gateway.networking.k8s.io"),
				Kind:  gatewayapiv1alpha2.Kind(kind),
				Name:  gatewayapiv1alpha2.ObjectName(name),
			},
		},
		PolicyNamespace: namespace,
	}
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

func (r *MutatorRegistry) ApplyWasmConfigMutators(wasmConfig *wasm.Config, gateway *machinery.Gateway, topology *machinery.Topology) error {
	targetRefs := []machinery.PolicyTargetReference{
		policyTargetRef("Gateway", gateway.GetName(), gateway.GetNamespace()),
	}

	// Include route-level targetRefs so mutators can find upstreams and pipeline
	// actions registered by policies that target HTTPRoutes/GRPCRoutes.
	if topology != nil {
		targetRefs = append(targetRefs, collectRouteTargetRefs(gateway, topology)...)
	}

	// When no actionsets exist but extension policies have pipeline actions,
	// create skeleton actionsets from the topology so the mutator's append logic works.
	if len(wasmConfig.ActionSets) == 0 && topology != nil {
		if r.hasRelevantPipelineActions(targetRefs) {
			wasmConfig.ActionSets = buildActionSetsFromTopology(gateway, topology)
		}
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, mutator := range r.wasmConfigMutators {
		if err := mutator.Mutate(wasmConfig, targetRefs); err != nil {
			return err
		}
	}

	// Remove skeleton action sets that received no actions after mutation.
	// The wasm-shim requires a non-empty actions field in every action set.
	filtered := wasmConfig.ActionSets[:0]
	for _, as := range wasmConfig.ActionSets {
		if len(as.Actions) > 0 || len(as.TypedActions) > 0 {
			filtered = append(filtered, as)
		}
	}
	wasmConfig.ActionSets = filtered

	return nil
}

// ApplyWasmConfigMutators applies all registered wasm config mutators for a specific gateway
func ApplyWasmConfigMutators(wasmConfig *wasm.Config, gateway *machinery.Gateway, topology *machinery.Topology) error {
	return GlobalMutatorRegistry.ApplyWasmConfigMutators(wasmConfig, gateway, topology)
}

// hasRelevantPipelineActions checks whether any wasm config mutator has pipeline actions
// for policies targeting the given gateway. This checks both policies with registered
// upstream methods and policies that only have pipeline actions (e.g., pure allow or
// response-only extensions).
func (r *MutatorRegistry) hasRelevantPipelineActions(targetRefs []machinery.PolicyTargetReference) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, mutator := range r.wasmConfigMutators {
		m, ok := mutator.(*RegisteredDataMutator[*wasm.Config])
		if !ok {
			continue
		}
		// Check policies with registered upstreams
		relevantUpstreamKeys := m.store.GetRelevantUpstreamKeys(targetRefs)
		policyIDs := lo.Uniq(lo.Map(lo.Keys(relevantUpstreamKeys), func(k RegisteredUpstreamKey, _ int) ResourceID { return k.Policy }))
		// Also include policies that have pipeline actions but no upstreams
		policyIDs = append(policyIDs, m.store.GetPoliciesWithPipelineActions()...)
		policyIDs = lo.Uniq(policyIDs)
		for _, policyID := range policyIDs {
			for _, phase := range []PipelinePhase{PipelinePhaseRequest, PipelinePhaseResponse} {
				if actions := m.store.GetPipelineActions(policyID, phase); len(actions) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// collectRouteTargetRefs traverses the topology from a gateway through listeners to
// routes and returns a PolicyTargetReference for each unique HTTPRoute/GRPCRoute.
func collectRouteTargetRefs(gateway *machinery.Gateway, topology *machinery.Topology) []machinery.PolicyTargetReference {
	var refs []machinery.PolicyTargetReference
	seen := make(map[string]bool)

	for _, child := range topology.Targetables().Children(gateway) {
		for _, grandchild := range topology.Targetables().Children(child) {
			var kind, name, namespace string
			switch route := grandchild.(type) {
			case *machinery.HTTPRoute:
				kind, name, namespace = "HTTPRoute", route.GetName(), route.GetNamespace()
			case *machinery.GRPCRoute:
				kind, name, namespace = "GRPCRoute", route.GetName(), route.GetNamespace()
			default:
				continue
			}
			routeKey := kind + "/" + namespace + "/" + name
			if seen[routeKey] {
				continue
			}
			seen[routeKey] = true
			refs = append(refs, policyTargetRef(kind, name, namespace))
		}
	}
	return refs
}

// buildActionSetsFromTopology creates skeleton ActionSets by traversing the topology
// from a gateway through its listeners to routes. Each route/hostname/match combination
// produces one ActionSet with proper RouteRuleConditions but no actions.
func buildActionSetsFromTopology(gateway *machinery.Gateway, topology *machinery.Topology) []wasm.ActionSet {
	var actionSets []wasm.ActionSet
	seen := make(map[string]bool)

	for _, child := range topology.Targetables().Children(gateway) {
		listener, ok := child.(*machinery.Listener)
		if !ok {
			continue
		}
		for _, grandchild := range topology.Targetables().Children(listener) {
			var routeLocator string
			switch route := grandchild.(type) {
			case *machinery.HTTPRoute:
				routeLocator = fmt.Sprintf("HTTPRoute/%s/%s", route.GetNamespace(), route.GetName())
			case *machinery.GRPCRoute:
				routeLocator = fmt.Sprintf("GRPCRoute/%s/%s", route.GetNamespace(), route.GetName())
			}
			for _, as := range wasm.BuildSkeletonActionSetsForRoute(listener, grandchild) {
				if !seen[as.Name] {
					seen[as.Name] = true
					as.SourceRoute = routeLocator
					actionSets = append(actionSets, as)
				}
			}
		}
	}
	return actionSets
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
	Policy  ResourceID
	Name    string
	URL     string
	Service string
	Method  string
}

type RegisteredUpstreamEntry struct {
	ClusterName     string
	Host            string
	Port            int
	TargetRef       TargetRef
	Service         string
	Method          string
	FailureMode     string
	Timeout         string
	MessageTemplate string
}

type TargetRef struct {
	Group     string
	Kind      string
	Name      string
	Namespace string
}

// PipelinePhase identifies whether actions run in the request or response phase.
type PipelinePhase string

const (
	PipelinePhaseRequest  PipelinePhase = "request"
	PipelinePhaseResponse PipelinePhase = "response"
)

// PipelineActionEntry represents a single stored pipeline action.
type PipelineActionEntry struct {
	Index           int
	ActionType      extpb.ActionType
	Predicate       string
	Intention       string // CEL expression (grpc_method, allow)
	Method          string // registered action method name (grpc_method)
	Var             string // variable name for gRPC response (grpc_method)
	HeadersToAdd    string // CEL expression for headers (add_headers)
	NewResponseCode int32  // HTTP status code (with_response_code)
}

// pipelineKey identifies a set of actions for a specific policy and phase.
type pipelineKey struct {
	Policy ResourceID
	Phase  PipelinePhase
}

type RegisteredDataStore struct {
	dataProviders map[DataProviderKey]DataProviderEntry
	dataMutex     sync.RWMutex

	subscriptions map[SubscriptionKey]Subscription
	subsMutex     sync.RWMutex

	registeredUpstreams map[RegisteredUpstreamKey]RegisteredUpstreamEntry
	protoCache          *ProtoCache
	upstreamsMutex      sync.RWMutex

	pipelineActions  map[pipelineKey][]PipelineActionEntry
	pipelineCounters map[pipelineKey]int
	pipelineMutex    sync.RWMutex
}

func NewRegisteredDataStore() *RegisteredDataStore {
	return &RegisteredDataStore{
		dataProviders:       make(map[DataProviderKey]DataProviderEntry),
		subscriptions:       make(map[SubscriptionKey]Subscription),
		registeredUpstreams: make(map[RegisteredUpstreamKey]RegisteredUpstreamEntry),
		protoCache:          NewProtoCache(),
		pipelineActions:     make(map[pipelineKey][]PipelineActionEntry),
		pipelineCounters:    make(map[pipelineKey]int),
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

func (r *RegisteredDataStore) SetUpstream(key RegisteredUpstreamKey, entry RegisteredUpstreamEntry, fds *descriptorpb.FileDescriptorSet) {
	r.upstreamsMutex.Lock()
	defer r.upstreamsMutex.Unlock()
	r.registeredUpstreams[key] = entry
	cacheKey := ProtoCacheKey{
		ClusterName: entry.ClusterName,
		Service:     entry.Service,
	}
	r.protoCache.Set(cacheKey, fds)
}

// IsUpstreamNameTaken returns true when another key for the same policy already
// uses the given name. This is a cheap read-lock check intended as a fast-path
// rejection before expensive operations; the authoritative check remains in
// SetUpstreamIfNameAvailable.
func (r *RegisteredDataStore) IsUpstreamNameTaken(key RegisteredUpstreamKey) bool {
	r.upstreamsMutex.RLock()
	defer r.upstreamsMutex.RUnlock()
	for k := range r.registeredUpstreams {
		if k.Policy == key.Policy && k.Name == key.Name && k != key {
			return true
		}
	}
	return false
}

// SetUpstreamIfNameAvailable atomically checks that no other key for the same
// policy already uses the given name, then writes the upstream and proto cache
// entries. Returns true on success; false when a name conflict was detected.
func (r *RegisteredDataStore) SetUpstreamIfNameAvailable(key RegisteredUpstreamKey, entry RegisteredUpstreamEntry, fds *descriptorpb.FileDescriptorSet) bool {
	r.upstreamsMutex.Lock()
	defer r.upstreamsMutex.Unlock()

	for k := range r.registeredUpstreams {
		if k.Policy == key.Policy && k.Name == key.Name && k != key {
			return false
		}
	}

	r.registeredUpstreams[key] = entry
	cacheKey := ProtoCacheKey{
		ClusterName: entry.ClusterName,
		Service:     entry.Service,
	}
	r.protoCache.Set(cacheKey, fds)
	return true
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

func (r *RegisteredDataStore) GetUpstreamsByTargetRef(targetRef TargetRef) []RegisteredUpstreamEntry {
	r.upstreamsMutex.RLock()
	defer r.upstreamsMutex.RUnlock()
	var result []RegisteredUpstreamEntry
	for _, entry := range r.registeredUpstreams {
		if entry.TargetRef == targetRef {
			result = append(result, entry)
		}
	}
	return result
}

// GetRelevantUpstreamKeys returns upstream key/entry pairs whose TargetRef
// matches any of the given target references.
func (r *RegisteredDataStore) GetRelevantUpstreamKeys(targetRefs []machinery.PolicyTargetReference) map[RegisteredUpstreamKey]RegisteredUpstreamEntry {
	r.upstreamsMutex.RLock()
	defer r.upstreamsMutex.RUnlock()

	result := make(map[RegisteredUpstreamKey]RegisteredUpstreamEntry)
	for _, targetRef := range targetRefs {
		tr := TargetRef{
			Group:     targetRef.GroupVersionKind().Group,
			Kind:      targetRef.GroupVersionKind().Kind,
			Name:      targetRef.GetName(),
			Namespace: targetRef.GetNamespace(),
		}
		for key, entry := range r.registeredUpstreams {
			if entry.TargetRef == tr {
				result[key] = entry
			}
		}
	}
	return result
}

func (r *RegisteredDataStore) GetRelevantUpstreams(targetRefs []machinery.PolicyTargetReference) []RegisteredUpstreamEntry {
	return lo.FlatMap(targetRefs, func(targetRef machinery.PolicyTargetReference, _ int) []RegisteredUpstreamEntry {
		return r.GetUpstreamsByTargetRef(TargetRef{
			Group:     targetRef.GroupVersionKind().Group,
			Kind:      targetRef.GroupVersionKind().Kind,
			Name:      targetRef.GetName(),
			Namespace: targetRef.GetNamespace(),
		})
	})
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

// HasUpstreamName returns true when a registered upstream with the given name
// exists for the specified policy.
func (r *RegisteredDataStore) HasUpstreamName(policy ResourceID, name string) bool {
	r.upstreamsMutex.RLock()
	defer r.upstreamsMutex.RUnlock()
	for k := range r.registeredUpstreams {
		if k.Policy == policy && k.Name == name {
			return true
		}
	}
	return false
}

func (r *RegisteredDataStore) GetUpstreamsForPolicy(policy ResourceID) []RegisteredUpstreamEntry {
	r.upstreamsMutex.RLock()
	defer r.upstreamsMutex.RUnlock()

	filtered := lo.PickBy(r.registeredUpstreams, func(key RegisteredUpstreamKey, _ RegisteredUpstreamEntry) bool {
		return key.Policy == policy
	})
	return lo.Values(filtered)
}

func (r *RegisteredDataStore) GetProtoDescriptor(cacheKey ProtoCacheKey) (*descriptorpb.FileDescriptorSet, bool) {
	r.upstreamsMutex.RLock()
	defer r.upstreamsMutex.RUnlock()
	return r.protoCache.Get(cacheKey)
}

// AppendPipelineActions atomically appends actions to the given policy and phase,
// assigning sequential indices starting from the current counter value.
// Returns the index of the first appended action.
func (r *RegisteredDataStore) AppendPipelineActions(policy ResourceID, phase PipelinePhase, actions []PipelineActionEntry) int {
	r.pipelineMutex.Lock()
	defer r.pipelineMutex.Unlock()

	key := pipelineKey{Policy: policy, Phase: phase}
	startIndex := r.pipelineCounters[key]

	for i := range actions {
		actions[i].Index = startIndex + i
		r.pipelineActions[key] = append(r.pipelineActions[key], actions[i])
	}
	r.pipelineCounters[key] = startIndex + len(actions)

	return startIndex
}

// GetPipelineActions returns the ordered pipeline actions for a policy and phase.
func (r *RegisteredDataStore) GetPipelineActions(policy ResourceID, phase PipelinePhase) []PipelineActionEntry {
	r.pipelineMutex.RLock()
	defer r.pipelineMutex.RUnlock()

	key := pipelineKey{Policy: policy, Phase: phase}
	actions := r.pipelineActions[key]
	if actions == nil {
		return nil
	}

	result := make([]PipelineActionEntry, len(actions))
	copy(result, actions)
	return result
}

// ClearPipelinePhase removes all pipeline actions for a policy in a single phase
// and resets the index counter.
func (r *RegisteredDataStore) ClearPipelinePhase(policy ResourceID, phase PipelinePhase) {
	r.pipelineMutex.Lock()
	defer r.pipelineMutex.Unlock()

	key := pipelineKey{Policy: policy, Phase: phase}
	delete(r.pipelineActions, key)
	delete(r.pipelineCounters, key)
}

// ReplacePipelineActions atomically clears all pipeline actions for a policy
// and replaces them with the provided request and response entries.
func (r *RegisteredDataStore) ReplacePipelineActions(policy ResourceID, requestEntries, responseEntries []PipelineActionEntry) {
	r.pipelineMutex.Lock()
	defer r.pipelineMutex.Unlock()

	for _, phase := range []PipelinePhase{PipelinePhaseRequest, PipelinePhaseResponse} {
		key := pipelineKey{Policy: policy, Phase: phase}
		delete(r.pipelineActions, key)
		delete(r.pipelineCounters, key)
	}

	if len(requestEntries) > 0 {
		key := pipelineKey{Policy: policy, Phase: PipelinePhaseRequest}
		for i := range requestEntries {
			requestEntries[i].Index = i
		}
		r.pipelineActions[key] = requestEntries
		r.pipelineCounters[key] = len(requestEntries)
	}

	if len(responseEntries) > 0 {
		key := pipelineKey{Policy: policy, Phase: PipelinePhaseResponse}
		for i := range responseEntries {
			responseEntries[i].Index = i
		}
		r.pipelineActions[key] = responseEntries
		r.pipelineCounters[key] = len(responseEntries)
	}
}

// ClearPipelineActions removes all pipeline actions for a policy across both phases
// and resets the index counters. Returns the number of actions cleared.
func (r *RegisteredDataStore) ClearPipelineActions(policy ResourceID) int {
	r.pipelineMutex.Lock()
	defer r.pipelineMutex.Unlock()

	cleared := 0
	for _, phase := range []PipelinePhase{PipelinePhaseRequest, PipelinePhaseResponse} {
		key := pipelineKey{Policy: policy, Phase: phase}
		cleared += len(r.pipelineActions[key])
		delete(r.pipelineActions, key)
		delete(r.pipelineCounters, key)
	}
	return cleared
}

// GetPoliciesWithPipelineActions returns the set of policy IDs that have any
// pipeline actions (request or response phase).
func (r *RegisteredDataStore) GetPoliciesWithPipelineActions() []ResourceID {
	r.pipelineMutex.RLock()
	defer r.pipelineMutex.RUnlock()

	seen := make(map[ResourceID]bool)
	for key, actions := range r.pipelineActions {
		if len(actions) > 0 {
			seen[key.Policy] = true
		}
	}
	result := make([]ResourceID, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	return result
}

func (r *RegisteredDataStore) ClearPolicyData(policy ResourceID) (clearedMutators int, clearedSubscriptions int, clearedUpstreams int) {
	r.dataMutex.Lock()
	r.subsMutex.Lock()
	r.upstreamsMutex.Lock()
	r.pipelineMutex.Lock()
	defer r.dataMutex.Unlock()
	defer r.subsMutex.Unlock()
	defer r.upstreamsMutex.Unlock()
	defer r.pipelineMutex.Unlock()

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

	// Collect upstreams to check for cache cleanup
	var upstreamsToCheck []RegisteredUpstreamEntry
	for key, entry := range r.registeredUpstreams {
		if key.Policy == policy {
			upstreamsToCheck = append(upstreamsToCheck, entry)
		}
	}

	// clear registered upstreams
	for key := range r.registeredUpstreams {
		if key.Policy == policy {
			delete(r.registeredUpstreams, key)
			clearedUpstreams++
		}
	}

	// Clean up proto cache for upstreams that are no longer referenced
	for _, upstream := range upstreamsToCheck {
		cacheKey := ProtoCacheKey{
			ClusterName: upstream.ClusterName,
			Service:     upstream.Service,
		}
		// Check if any other upstream still references this cache entry
		stillReferenced := false
		for _, entry := range r.registeredUpstreams {
			if entry.ClusterName == cacheKey.ClusterName && entry.Service == cacheKey.Service {
				stillReferenced = true
				break
			}
		}
		if !stillReferenced {
			r.protoCache.Delete(cacheKey)
		}
	}

	// clear pipeline actions (lock already held)
	for _, phase := range []PipelinePhase{PipelinePhaseRequest, PipelinePhaseResponse} {
		key := pipelineKey{Policy: policy, Phase: phase}
		delete(r.pipelineActions, key)
		delete(r.pipelineCounters, key)
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

	// preserve existing request data
	for k, v := range wasmConfig.RequestData {
		requestData[k] = v
	}

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

	// Inject registered upstream services matching the current target refs
	relevantUpstreamKeys := m.store.GetRelevantUpstreamKeys(targetRefs)

	// Build method name → wasm service key lookup and inject services
	methodServiceKeys := make(map[ResourceID]map[string]string) // policy → method name → service key
	for key, entry := range relevantUpstreamKeys {
		timeout := entry.Timeout
		svc := wasm.Service{
			Endpoint:    entry.ClusterName,
			Type:        wasm.DynamicServiceType,
			FailureMode: wasm.FailureModeType(entry.FailureMode),
			Timeout:     &timeout,
			GrpcService: &entry.Service,
			GrpcMethod:  &entry.Method,
		}
		wasmServiceKey := "ext-" + HashUpstreamServiceConfig(svc)
		if wasmConfig.Services == nil {
			wasmConfig.Services = make(map[string]wasm.Service)
		}
		wasmConfig.Services[wasmServiceKey] = svc

		if methodServiceKeys[key.Policy] == nil {
			methodServiceKeys[key.Policy] = make(map[string]string)
		}
		methodServiceKeys[key.Policy][key.Name] = wasmServiceKey
	}

	// Set descriptor service cluster name if there are any dynamic services
	if len(relevantUpstreamKeys) > 0 {
		wasmConfig.DescriptorService = "kuadrant-operator-grpc"
	}

	// Translate pipeline actions into TypedAction entries (deterministic order across policies)
	policyIDs := lo.Keys(methodServiceKeys)
	sort.Slice(policyIDs, func(i, j int) bool {
		if policyIDs[i].Kind != policyIDs[j].Kind {
			return policyIDs[i].Kind < policyIDs[j].Kind
		}
		if policyIDs[i].Namespace != policyIDs[j].Namespace {
			return policyIDs[i].Namespace < policyIDs[j].Namespace
		}
		return policyIDs[i].Name < policyIDs[j].Name
	})

	// Build upstream lookup: policy+method → RegisteredUpstreamEntry (for MessageTemplate)
	// Also build policy → route locator mapping for action set filtering.
	upstreamByMethod := make(map[ResourceID]map[string]RegisteredUpstreamEntry)
	policyRouteLocator := make(map[ResourceID]string)
	for key, entry := range relevantUpstreamKeys {
		if upstreamByMethod[key.Policy] == nil {
			upstreamByMethod[key.Policy] = make(map[string]RegisteredUpstreamEntry)
		}
		upstreamByMethod[key.Policy][key.Name] = entry
		if entry.TargetRef.Kind == "HTTPRoute" || entry.TargetRef.Kind == "GRPCRoute" {
			policyRouteLocator[key.Policy] = fmt.Sprintf("%s/%s/%s", entry.TargetRef.Kind, entry.TargetRef.Namespace, entry.TargetRef.Name)
		}
	}

	for _, policyID := range policyIDs {
		methods := methodServiceKeys[policyID]
		policyLocator := fmt.Sprintf("%s/%s/%s", policyID.Kind, policyID.Namespace, policyID.Name)
		sources := []string{policyLocator}

		requestEntries := m.store.GetPipelineActions(policyID, PipelinePhaseRequest)
		responseEntries := m.store.GetPipelineActions(policyID, PipelinePhaseResponse)

		// Build response-phase TypedActions (these nest inside gRPC action's onReply)
		var onReplyActions []wasm.TypedAction
		for _, entry := range responseEntries {
			switch entry.ActionType {
			case extpb.ActionType_ACTION_TYPE_ADD_HEADERS:
				onReplyActions = append(onReplyActions, wasm.TypedAction{
					Type:                 "headers",
					Predicate:            predicateOrTrue(entry.Predicate),
					Terminal:             false,
					Target:               "response",
					Headers:              entry.HeadersToAdd,
					SourcePolicyLocators: sources,
				})
			case extpb.ActionType_ACTION_TYPE_WITH_RESPONSE_CODE:
				onReplyActions = append(onReplyActions, wasm.TypedAction{
					Type:                 "deny",
					Predicate:            predicateOrTrue(entry.Predicate),
					Terminal:             true,
					DenyWith:             fmt.Sprintf("DenyResponse{status: %du}", entry.NewResponseCode),
					SourcePolicyLocators: sources,
				})
			}
		}

		// Collect AllowAction deny entries first — the wasm-shim only supports
		// gRPC typed actions at the top level, so AllowAction denies must be
		// injected into a gRPC action's onReply.
		var allowDenyActions []wasm.TypedAction
		for _, entry := range requestEntries {
			if entry.ActionType == extpb.ActionType_ACTION_TYPE_ALLOW && entry.Intention != "" {
				denyPredicate := fmt.Sprintf("!(%s)", entry.Intention)
				if entry.Predicate != "" {
					denyPredicate = fmt.Sprintf("%s && !(%s)", entry.Predicate, entry.Intention)
				}
				allowDenyActions = append(allowDenyActions, wasm.TypedAction{
					Type:                 "deny",
					Predicate:            denyPredicate,
					Terminal:             true,
					DenyWith:             "DenyResponse{status: 403u}",
					SourcePolicyLocators: sources,
				})
			}
		}

		// Build request-phase TypedActions
		var typedActions []wasm.TypedAction
		allowsPrepended := false
		for _, entry := range requestEntries {
			if entry.ActionType != extpb.ActionType_ACTION_TYPE_GRPC_METHOD {
				continue
			}
			grpcPredicate := predicateOrTrue(entry.Predicate)

			ta := wasm.TypedAction{
				Type:                 "grpc",
				Terminal:             false,
				Var:                  entry.Var,
				Service:              methods[entry.Method],
				SourcePolicyLocators: sources,
			}
			if upstream, ok := upstreamByMethod[policyID][entry.Method]; ok && upstream.MessageTemplate != "" {
				ta.MessageBuilder = upstream.MessageTemplate
			}

			if len(allowDenyActions) > 0 && !allowsPrepended {
				// AllowActions must fire regardless of the gRPC predicate, so
				// we remove the predicate from this gRPC action (it fires
				// unconditionally) and move the original predicate into the
				// intention deny predicate.
				ta.Predicate = "true"
				ta.OnReply = append(ta.OnReply, allowDenyActions...)
				allowsPrepended = true

				if entry.Intention != "" {
					intentionPredicate := fmt.Sprintf("!(%s)", entry.Intention)
					if grpcPredicate != "true" {
						intentionPredicate = fmt.Sprintf("%s && !(%s)", grpcPredicate, entry.Intention)
					}
					ta.OnReply = append(ta.OnReply, wasm.TypedAction{
						Type:                 "deny",
						Predicate:            intentionPredicate,
						Terminal:             true,
						DenyWith:             "DenyResponse{status: 403u}",
						SourcePolicyLocators: sources,
					})
				}
			} else {
				ta.Predicate = grpcPredicate
				if entry.Intention != "" {
					ta.OnReply = append(ta.OnReply, wasm.TypedAction{
						Type:                 "deny",
						Predicate:            fmt.Sprintf("!(%s)", entry.Intention),
						Terminal:             true,
						DenyWith:             "DenyResponse{status: 403u}",
						SourcePolicyLocators: sources,
					})
				}
			}

			ta.OnReply = append(ta.OnReply, onReplyActions...)
			typedActions = append(typedActions, ta)
		}

		// If there are only response actions and no request actions, attach them directly
		if len(requestEntries) == 0 && len(onReplyActions) > 0 {
			typedActions = append(typedActions, onReplyActions...)
		}

		// Append typed actions to matching action sets. If the policy targets a
		// specific route, only action sets created from that route receive the
		// actions. If the policy targets a gateway (or no route locator is
		// known), all action sets receive the actions.
		routeLocator := policyRouteLocator[policyID]
		for i := range wasmConfig.ActionSets {
			if routeLocator != "" && wasmConfig.ActionSets[i].SourceRoute != "" && wasmConfig.ActionSets[i].SourceRoute != routeLocator {
				continue
			}
			wasmConfig.ActionSets[i].TypedActions = append(wasmConfig.ActionSets[i].TypedActions, typedActions...)
		}
	}

	return nil
}

func predicateOrTrue(predicate string) string {
	if predicate == "" {
		return "true"
	}
	return predicate
}

// HashUpstreamServiceConfig produces a deterministic short hash from a wasm.Service
// config. Identical configurations produce the same hash, providing natural deduplication.
func HashUpstreamServiceConfig(svc wasm.Service) string {
	timeout := ""
	if svc.Timeout != nil {
		timeout = *svc.Timeout
	}
	grpcService := ""
	if svc.GrpcService != nil {
		grpcService = *svc.GrpcService
	}
	grpcMethod := ""
	if svc.GrpcMethod != nil {
		grpcMethod = *svc.GrpcMethod
	}
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s", svc.Type, svc.Endpoint, svc.FailureMode, timeout, grpcService, grpcMethod)
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h[:8])
}

// CollectRouteUpstreams traverses the topology from a gateway through its listeners
// to find all attached routes (HTTPRoute and GRPCRoute), then returns registered
// upstreams for those routes. Routes are deduplicated by kind/namespace/name to avoid
// duplicate entries when a route appears under multiple listeners.
func CollectRouteUpstreams(topology *machinery.Topology, gateway *machinery.Gateway) []RegisteredUpstreamEntry {
	var result []RegisteredUpstreamEntry
	seen := make(map[string]bool)

	for _, child := range topology.Targetables().Children(gateway) {
		for _, grandchild := range topology.Targetables().Children(child) {
			var kind, name, namespace string
			switch route := grandchild.(type) {
			case *machinery.HTTPRoute:
				kind, name, namespace = "HTTPRoute", route.GetName(), route.GetNamespace()
			case *machinery.GRPCRoute:
				kind, name, namespace = "GRPCRoute", route.GetName(), route.GetNamespace()
			default:
				continue
			}

			routeKey := kind + "/" + namespace + "/" + name
			if seen[routeKey] {
				continue
			}
			seen[routeKey] = true

			result = append(result, GetRegisteredUpstreamsByTargetRef(TargetRef{
				Group:     "gateway.networking.k8s.io",
				Kind:      kind,
				Name:      name,
				Namespace: namespace,
			})...)
		}
	}
	return result
}

// GetRegisteredUpstreamsByTargetRef returns registered upstreams matching the given targetRef,
// aggregated across all extension data stores in the GlobalMutatorRegistry.
func GetRegisteredUpstreamsByTargetRef(targetRef TargetRef) []RegisteredUpstreamEntry {
	GlobalMutatorRegistry.mutex.RLock()
	defer GlobalMutatorRegistry.mutex.RUnlock()

	var result []RegisteredUpstreamEntry
	for _, mutator := range GlobalMutatorRegistry.wasmConfigMutators {
		if m, ok := mutator.(*RegisteredDataMutator[*wasm.Config]); ok {
			result = append(result, m.store.GetUpstreamsByTargetRef(targetRef)...)
		}
	}
	return result
}
