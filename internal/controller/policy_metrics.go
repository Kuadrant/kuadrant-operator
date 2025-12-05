/*
Copyright 2025.

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

package controllers

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

const (
	policyKindLabel   = "kind"
	policyStatusLabel = "status"
)

var (
	// policiesTotal tracks the total number of policies by kind
	policiesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuadrant_policies_total",
			Help: "Total number of Kuadrant policies by kind",
		},
		[]string{policyKindLabel})

	// policiesEnforced tracks the enforcement status of policies
	policiesEnforced = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuadrant_policies_enforced",
			Help: "Number of Kuadrant policies by kind and enforcement status",
		},
		[]string{policyKindLabel, policyStatusLabel})
)

// PolicyStatus represents the enforcement status of a policy
type PolicyStatus string

const (
	PolicyStatusTrue  PolicyStatus = "true"
	PolicyStatusFalse PolicyStatus = "false"
)

// PolicyMetricsReconciler emits Prometheus metrics for all Kuadrant policies
type PolicyMetricsReconciler struct{}

// NewPolicyMetricsReconciler creates a new PolicyMetricsReconciler
func NewPolicyMetricsReconciler() *PolicyMetricsReconciler {
	return &PolicyMetricsReconciler{}
}

// Reconcile collects and emits metrics for all policies in the topology.
// This reconciler automatically discovers and tracks all policy types by grouping policies by their Kind.
// Currently includes core policies: AuthPolicy, RateLimitPolicy, DNSPolicy, TLSPolicy, and TokenRateLimitPolicy.
// Note: Extension policies (OIDCPolicy, PlanPolicy, TelemetryPolicy) are not part of the topology and are not tracked.
func (r *PolicyMetricsReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("policy_metrics").WithValues("context", ctx)

	// Reset all metrics to zero before recalculating
	policiesTotal.Reset()
	policiesEnforced.Reset()

	// Group all policies by kind for automatic discovery
	policiesByKind := make(map[string][]machinery.Policy)

	for _, policy := range topology.Policies().Items() {
		kind := policy.GroupVersionKind().Kind
		policiesByKind[kind] = append(policiesByKind[kind], policy)
	}

	// Emit metrics for each discovered policy kind
	for kind, policies := range policiesByKind {
		r.emitMetricsForPolicies(kind, policies)
	}

	logger.V(1).Info("policy metrics updated", "policyKinds", len(policiesByKind))
	return nil
}

// emitMetricsForPolicies emits metrics for a list of policies of a given kind
func (r *PolicyMetricsReconciler) emitMetricsForPolicies(kind string, policies []machinery.Policy) {
	total := len(policies)
	policiesTotal.WithLabelValues(kind).Set(float64(total))

	// Track enforcement status counts
	enforcedCounts := map[PolicyStatus]int{
		PolicyStatusTrue:  0,
		PolicyStatusFalse: 0,
	}

	for _, policy := range policies {
		status := r.getEnforcementStatus(policy)
		enforcedCounts[status]++
	}

	// Emit enforcement metrics
	for status, count := range enforcedCounts {
		policiesEnforced.WithLabelValues(kind, string(status)).Set(float64(count))
	}
}

// getEnforcementStatus returns the enforcement status of a policy based on its Enforced condition.
// A policy is considered enforced (true) only when it has an Enforced condition with status True.
// All other cases (no condition, condition False, condition Unknown, or unable to read status) are
// treated as not enforced (false).
func (r *PolicyMetricsReconciler) getEnforcementStatus(policy machinery.Policy) PolicyStatus {
	policyWithStatusObj, ok := policy.(kuadrantgatewayapi.Policy)
	if !ok {
		return PolicyStatusFalse
	}

	conditions := policyWithStatusObj.GetStatus().GetConditions()
	enforcedCondition := meta.FindStatusCondition(conditions, string(kuadrant.PolicyConditionEnforced))

	if enforcedCondition == nil || enforcedCondition.Status != metav1.ConditionTrue {
		return PolicyStatusFalse
	}

	return PolicyStatusTrue
}

func init() {
	// Register metrics with controller-runtime's Prometheus registry
	metrics.Registry.MustRegister(policiesTotal, policiesEnforced)
}
