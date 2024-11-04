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
	"sort"
	"strings"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

const (
	AtomicMergeStrategy     = "atomic"
	PolicyRuleMergeStrategy = "merge"
)

// NewMergeableRule creates a new MergeableRule with a default source if the rule does not have one.
func NewMergeableRule(rule MergeableRule, defaultSource string) MergeableRule {
	if rule.GetSource() == "" {
		return rule.WithSource(defaultSource)
	}
	return rule
}

// MergeableRule is a policy rule that contains a spec which can be traced back to its source,
// i.e. to the policy where the rule spec was defined.
// +kubebuilder:object:generate=false
type MergeableRule interface {
	GetSpec() any
	GetSource() string
	WithSource(string) MergeableRule
}

// +kubebuilder:object:generate=false
type MergeablePolicy interface {
	machinery.Policy

	Rules() map[string]MergeableRule
	SetRules(map[string]MergeableRule)
	Empty() bool

	DeepCopyObject() runtime.Object
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

	if mergeableTarget := target.(MergeablePolicy); !mergeableTarget.Empty() {
		return copyMergeablePolicy(mergeableTarget)
	}

	return copyMergeablePolicy(source.(MergeablePolicy))
}

var _ machinery.MergeStrategy = AtomicDefaultsMergeStrategy

// AtomicOverridesMergeStrategy implements a merge strategy that overrides a target Policy with
// a source one.
func AtomicOverridesMergeStrategy(source, _ machinery.Policy) machinery.Policy {
	if source == nil {
		return nil
	}
	return copyMergeablePolicy(source.(MergeablePolicy))
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
	rules := lo.MapValues(targetMergeablePolicy.Rules(), mapRuleWithSourceFunc(target))

	// add extra rules from the source
	for ruleID, rule := range sourceMergeablePolicy.Rules() {
		if _, ok := targetMergeablePolicy.Rules()[ruleID]; !ok {
			origin := rule.GetSource()
			if origin == "" {
				origin = source.GetLocator()
			}
			rules[ruleID] = rule.WithSource(origin)
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
	rules := lo.MapValues(sourceMergeablePolicy.Rules(), mapRuleWithSourceFunc(source))

	// add extra rules from the target
	for ruleID, rule := range targetMergeablePolicy.Rules() {
		if _, ok := sourceMergeablePolicy.Rules()[ruleID]; !ok {
			origin := rule.GetSource()
			if origin == "" {
				origin = target.GetLocator()
			}
			rules[ruleID] = rule.WithSource(origin)
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
		policies := lo.FilterMap(targetable.Policies(), func(policy machinery.Policy, _ int) (controller.Object, bool) {
			o, object := policy.(controller.Object)
			return o, object && predicate(policy)
		})
		sort.Sort(controller.ObjectsByCreationTimestamp(policies))
		return lo.Map(policies, func(policy controller.Object, _ int) machinery.Policy {
			p, _ := policy.(machinery.Policy)
			return p
		})
	})
}

func PathID(path []machinery.Targetable) string {
	return strings.Join(lo.Map(path, func(t machinery.Targetable, _ int) string {
		return strings.TrimPrefix(k8stypes.NamespacedName{Namespace: t.GetNamespace(), Name: t.GetName()}.String(), string(k8stypes.Separator))
	}), "|")
}

func mapRuleWithSourceFunc(source machinery.Policy) func(MergeableRule, string) MergeableRule {
	return func(rule MergeableRule, _ string) MergeableRule {
		return rule.WithSource(source.GetLocator())
	}
}

func copyMergeablePolicy(policy MergeablePolicy) MergeablePolicy {
	dup := policy.DeepCopyObject().(MergeablePolicy)
	dup.SetRules(lo.MapValues(dup.Rules(), mapRuleWithSourceFunc(policy)))
	return dup
}
