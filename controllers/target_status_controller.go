package controllers

/*
Copyright 2021 Red Hat, Inc.

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

import (
	"fmt"

	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

const PolicyAffectedConditionPattern = "kuadrant.io/%sAffected" // Policy kinds are expected to be named XPolicy

func FindRouteParentStatusFunc(route *gatewayapiv1.HTTPRoute, gatewayKey client.ObjectKey, controllerName gatewayapiv1.GatewayController) func(gatewayapiv1.RouteParentStatus) bool {
	return func(p gatewayapiv1.RouteParentStatus) bool {
		return *p.ParentRef.Kind == ("Gateway") &&
			p.ControllerName == controllerName &&
			((p.ParentRef.Namespace == nil && route.GetNamespace() == gatewayKey.Namespace) || string(*p.ParentRef.Namespace) == gatewayKey.Namespace) &&
			string(p.ParentRef.Name) == gatewayKey.Name
	}
}

func buildPolicyAffectedCondition(policy kuadrantgatewayapi.Policy) metav1.Condition {
	policyKind := policy.GetObjectKind().GroupVersionKind().Kind

	condition := metav1.Condition{
		Type:    PolicyAffectedConditionType(policyKind),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("Object affected by %s %s", policyKind, client.ObjectKeyFromObject(policy)),
	}

	if c := meta.FindStatusCondition(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted)); c == nil || c.Status != metav1.ConditionTrue { // should we aim for 'Enforced' instead?
		condition.Status = metav1.ConditionFalse
		condition.Message = fmt.Sprintf("Object unaffected by %s %s, policy is not accepted", policyKind, client.ObjectKeyFromObject(policy))
		condition.Reason = PolicyReasonUnknown
		if c != nil {
			condition.Reason = c.Reason
		}
	}

	return condition
}

func PolicyAffectedCondition(policyKind string, policies []machinery.Policy) metav1.Condition {
	condition := metav1.Condition{
		Type:   PolicyAffectedConditionType(policyKind),
		Status: metav1.ConditionTrue,
		Reason: string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("Object affected by %s %s", policyKind, lo.Map(policies, func(item machinery.Policy, index int) client.ObjectKey {
			return client.ObjectKey{Name: item.GetName(), Namespace: item.GetNamespace()}
		})),
	}

	//if c := meta.FindStatusCondition(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted)); c == nil || c.Status != metav1.ConditionTrue { // should we aim for 'Enforced' instead?
	//	condition.Status = metav1.ConditionFalse
	//	condition.Message = fmt.Sprintf("Object unaffected by %s %s, policy is not accepted", policyKind, client.ObjectKeyFromObject(policy))
	//	condition.Reason = PolicyReasonUnknown
	//	if c != nil {
	//		condition.Reason = c.Reason
	//	}
	//}

	return condition
}

func PolicyAffectedConditionType(policyKind string) string {
	return fmt.Sprintf(PolicyAffectedConditionPattern, policyKind)
}
