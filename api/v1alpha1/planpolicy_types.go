/*
Copyright 2021.

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

package v1alpha1

import (
	"fmt"
	"strings"

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

var (
	PlanPolicyGroupKind  = schema.GroupKind{Group: GroupVersion.Group, Kind: "PlanPolicy"}
	PlanPoliciesResource = GroupVersion.WithResource("planpolicies")
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PlanPolicy enables rate limiting through plans of identified requests
type PlanPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlanPolicySpec   `json:"spec,omitempty"`
	Status PlanPolicyStatus `json:"status,omitempty"`
}

func (p *PlanPolicy) GetNamespace() string {
	return p.Namespace
}

func (p *PlanPolicy) GetName() string {
	return p.Name
}

func (p *PlanPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		p.Spec.TargetRef,
	}
}

func (p *PlanPolicy) ToRateLimits() map[string]kuadrantv1.Limit {
	return utils.Associate(p.Spec.Plans, func(plan Plan) (string, kuadrantv1.Limit) {
		return plan.Tier, kuadrantv1.Limit{
			When:  kuadrantv1.NewWhenPredicates(fmt.Sprintf(`auth.kuadrant.plan_tier == "%s"`, plan.Tier)),
			Rates: plan.Limits.ToRates(),
		}
	})
}

func (p *PlanPolicy) ToCelExpression() authorinov1beta3.CelExpression {
	var tierList strings.Builder
	var tierPredicates strings.Builder

	for i, plan := range p.Spec.Plans {
		predicate := strings.ReplaceAll(plan.Predicate, "\n", "")
		tierList.WriteString(fmt.Sprintf(`"%s"`, plan.Tier))
		tierPredicates.WriteString(fmt.Sprintf(`    "%s": %s,`, plan.Tier, predicate))

		if i < len(p.Spec.Plans)-1 {
			tierList.WriteString(", ")
			tierPredicates.WriteString("\n")
		}
	}

	return authorinov1beta3.CelExpression(fmt.Sprintf(
		`[%s]
  .filter(i, i in
    [{
%s
    }].map(m, m.filter(key, m[key]))[0])[0]`, tierList.String(), tierPredicates.String()))
}

// PlanPolicySpec defines the desired state of PlanPolicy
type PlanPolicySpec struct {
	// Reference to the object to which this policy applies.
	// todo(adam-cattermole): This doesn't have to be tied to a particular IdentityPolicy, but could be updated to support other resources
	// +kubebuilder:validation:XValidation:rule="self.group == 'kuadrant.io'",message="Invalid targetRef.group. The only supported value is 'kuadrant.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'AuthPolicy'",message="Invalid targetRef.kind. The only supported value is 'AuthPolicy'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Plans defines the list of plans for the policy. The identity is categorised by the first matching plan in the list.
	Plans []Plan `json:"plans"`
}

type Plan struct {
	// Tier this plan represents.
	Tier string `json:"tier"`

	// Limits contains the list of limits that the plan enforces.
	// +optional
	Limits Limits `json:"limits,omitempty"`

	// Predicate is a CEL expression used to determine if the plan is applied.
	// +kubebuilder:validation:MinLength=1
	Predicate string `json:"predicate"`
}

type Limits struct {
	// Daily limit of requests for this plan.
	// +optional
	Daily *int `json:"daily,omitempty"`

	// Weekly limit of requests for this plan.
	// +optional
	Weekly *int `json:"weekly,omitempty"`

	// Monthly limit of requests for this plan.
	// +optional
	Monthly *int `json:"monthly,omitempty"`

	// Yearly limit of requests for this plan.
	// +optional
	Yearly *int `json:"yearly,omitempty"`

	// Custom defines any additional limits defined in terms of a RateLimitPolicy Rate.
	// +optional
	Custom []kuadrantv1.Rate `json:"custom,omitempty"`
}

func (l *Limits) ToRates() []kuadrantv1.Rate {
	rates := make([]kuadrantv1.Rate, 0)
	addRate := func(limit *int, window kuadrantv1.Duration) {
		if limit != nil {
			rates = append(rates, kuadrantv1.Rate{
				Limit:  *limit,
				Window: window,
			})
		}
	}
	addRate(l.Daily, "24h")
	addRate(l.Weekly, "168h")
	addRate(l.Monthly, "730h")
	addRate(l.Yearly, "8760h")
	rates = append(rates, l.Custom...)
	return rates
}

// PlanPolicyStatus defines the observed state of PlanPolicy
type PlanPolicyStatus struct {
}

//+kubebuilder:object:root=true

// PlanPolicyList contains a list of PlanPolicy
type PlanPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlanPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PlanPolicy{}, &PlanPolicyList{})
}
