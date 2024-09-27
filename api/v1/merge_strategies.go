/*
Copyright 2024.

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

package v1

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/policy-machinery/machinery"
)

const (
	AtomicMergeStrategy     = "atomic"
	PolicyRuleMergeStrategy = "merge"
)

type MergeableRule struct {
	Spec   any
	Source string
}

// +kubebuilder:object:generate=false
type MergeablePolicy interface {
	machinery.Policy

	Rules() map[string]MergeableRule
	SetRules(map[string]MergeableRule)
	Empty() bool

	DeepCopyObject() runtime.Object
}

type SortablePolicy interface {
	machinery.Policy
	GetCreationTimestamp() metav1.Time
}

type PolicyByCreationTimestamp []SortablePolicy

func (a PolicyByCreationTimestamp) Len() int      { return len(a) }
func (a PolicyByCreationTimestamp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a PolicyByCreationTimestamp) Less(i, j int) bool {
	p1Time := ptr.To(a[i].GetCreationTimestamp())
	p2Time := ptr.To(a[j].GetCreationTimestamp())
	if !p1Time.Equal(p2Time) {
		return p1Time.Before(p2Time)
	}
	//  The object appearing first in alphabetical order by "{namespace}/{name}".
	return fmt.Sprintf("%s/%s", a[i].GetNamespace(), a[i].GetName()) < fmt.Sprintf("%s/%s", a[j].GetNamespace(), a[j].GetName())
}

// AtomicDefaultsMergeStrategy implements a merge strategy that returns the target Policy if it exists,
// otherwise it returns the source Policy.
func AtomicDefaultsMergeStrategy(source, target machinery.Policy) machinery.Policy {
	if source == nil {
		return target
	}
	if target == nil {
		return source
	}

	mergeableTargetPolicy := target.(MergeablePolicy)

	if !mergeableTargetPolicy.Empty() {
		return mergeableTargetPolicy.DeepCopyObject().(machinery.Policy)
	}

	return source.(MergeablePolicy).DeepCopyObject().(machinery.Policy)
}

var _ machinery.MergeStrategy = AtomicDefaultsMergeStrategy

// AtomicOverridesMergeStrategy implements a merge strategy that overrides a target Policy with
// a source one.
func AtomicOverridesMergeStrategy(source, _ machinery.Policy) machinery.Policy {
	if source == nil {
		return nil
	}
	return source.(MergeablePolicy).DeepCopyObject().(machinery.Policy)
}

var _ machinery.MergeStrategy = AtomicOverridesMergeStrategy

// PolicyRuleDefaultsMergeStrategy implements a merge strategy that merges a source Policy into a target one
// by keeping the policy rules from the target and adding the ones from the source that do not exist in the target.
func PolicyRuleDefaultsMergeStrategy(source, target machinery.Policy) machinery.Policy {
	if source == nil {
		return target
	}
	if target == nil {
		return source
	}

	sourceMergeablePolicy := source.(MergeablePolicy)
	targetMergeablePolicy := target.(MergeablePolicy)

	// copy rules from the target
	rules := targetMergeablePolicy.Rules()

	// add extra rules from the source
	for ruleID, rule := range sourceMergeablePolicy.Rules() {
		if _, ok := targetMergeablePolicy.Rules()[ruleID]; !ok {
			rules[ruleID] = MergeableRule{
				Spec:   rule.Spec,
				Source: source.GetLocator(),
			}
		}
	}

	mergedPolicy := targetMergeablePolicy.DeepCopyObject().(MergeablePolicy)
	mergedPolicy.SetRules(rules)
	return mergedPolicy
}

var _ machinery.MergeStrategy = PolicyRuleDefaultsMergeStrategy

// PolicyRuleOverridesMergeStrategy implements a merge strategy that merges a source Policy into a target one
// by using the policy rules from the source and keeping from the target only the policy rules that do not exist in
// the source.
func PolicyRuleOverridesMergeStrategy(source, target machinery.Policy) machinery.Policy {
	sourceMergeablePolicy := source.(MergeablePolicy)
	targetMergeablePolicy := target.(MergeablePolicy)

	// copy rules from the source
	rules := sourceMergeablePolicy.Rules()

	// add extra rules from the target
	for ruleID, rule := range targetMergeablePolicy.Rules() {
		if _, ok := sourceMergeablePolicy.Rules()[ruleID]; !ok {
			rules[ruleID] = rule
		}
	}

	mergedPolicy := targetMergeablePolicy.DeepCopyObject().(MergeablePolicy)
	mergedPolicy.SetRules(rules)
	return mergedPolicy
}

var _ machinery.MergeStrategy = PolicyRuleOverridesMergeStrategy

func DefaultsMergeStrategy(strategy string) machinery.MergeStrategy {
	switch strategy {
	case AtomicMergeStrategy:
		return AtomicDefaultsMergeStrategy
	case PolicyRuleMergeStrategy:
		return PolicyRuleDefaultsMergeStrategy
	default:
		return AtomicDefaultsMergeStrategy
	}
}

func OverridesMergeStrategy(strategy string) machinery.MergeStrategy {
	switch strategy {
	case AtomicMergeStrategy:
		return AtomicOverridesMergeStrategy
	case PolicyRuleMergeStrategy:
		return PolicyRuleOverridesMergeStrategy
	default:
		return AtomicOverridesMergeStrategy
	}
}

// EffectivePolicyForPath returns the effective policy for a given path, merging all policies in the path.
// The policies in the path are sorted from the least specific to the most specific.
// Only policies whose predicate returns true are considered.
func EffectivePolicyForPath[T machinery.Policy](path []machinery.Targetable, predicate func(machinery.Policy) bool) *T {
	policies := PoliciesInPath(path, predicate)
	if len(policies) == 0 {
		return nil
	}

	// map reduces the policies from most specific to least specific, merging them into one effective policy
	effectivePolicy := lo.ReduceRight(policies, func(effectivePolicy machinery.Policy, policy machinery.Policy, _ int) machinery.Policy {
		return effectivePolicy.Merge(policy)
	}, policies[len(policies)-1])

	concreteEffectivePolicy, _ := effectivePolicy.(T)
	return &concreteEffectivePolicy
}

// OrderedPoliciesForPath gathers all policies in a path sorted from the least specific to the most specific.
// Only policies whose predicate returns true are considered.
func PoliciesInPath(path []machinery.Targetable, predicate func(machinery.Policy) bool) []machinery.Policy {
	return lo.FlatMap(path, func(targetable machinery.Targetable, _ int) []machinery.Policy {
		policies := lo.FilterMap(targetable.Policies(), func(policy machinery.Policy, _ int) (SortablePolicy, bool) {
			p, sortable := policy.(SortablePolicy)
			return p, sortable && predicate(p)
		})
		sort.Sort(PolicyByCreationTimestamp(policies))
		return lo.Map(policies, func(policy SortablePolicy, _ int) machinery.Policy { return policy })
	})
}

func PathID(path []machinery.Targetable) string {
	s := strings.Join(lo.Map(path, machinery.MapTargetableToLocatorFunc), ">")
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:8])
}
