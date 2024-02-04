package conditions

import (
	"errors"
	"fmt"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type ConditionType string
type ConditionReason string

const (
	ConditionTypeReady ConditionType = "Ready"

	//common policy reasons for policy affected conditions

	PolicyReasonAccepted   ConditionReason = "Accepted"
	PolicyReasonInvalid    ConditionReason = "Invalid"
	PolicyReasonUnknown    ConditionReason = "Unknown"
	PolicyReasonConflicted ConditionReason = "Conflicted"

	PolicyReasonTargetNotFound ConditionReason = "TargetNotFound"
)

var ErrTargetNotFound = errors.New("target not found")

func BuildPolicyAffectedCondition(conditionType ConditionType, policyObject runtime.Object, targetRef metav1.Object, reason ConditionReason, err error) metav1.Condition {

	condition := metav1.Condition{
		Type:               string(conditionType),
		Status:             metav1.ConditionTrue,
		Reason:             string(reason),
		ObservedGeneration: targetRef.GetGeneration(),
	}

	objectMeta, metaErr := k8smeta.Accessor(policyObject)
	if metaErr != nil {
		condition.Status = metav1.ConditionFalse
		condition.Message = fmt.Sprintf("failed to get metadata about policy object %s", policyObject.GetObjectKind().GroupVersionKind().String())
		condition.Reason = string(PolicyReasonUnknown)
		return condition
	}
	if err != nil {
		condition.Status = metav1.ConditionFalse
		condition.Message = fmt.Sprintf("policy failed. Object unaffected by policy %s in namespace %s with name %s with error %s", policyObject.GetObjectKind().GroupVersionKind().String(), objectMeta.GetNamespace(), objectMeta.GetName(), err)
		return condition
	}

	condition.Message = fmt.Sprintf("policy success. Object affected by policy %s in namespace %s with name %s ", policyObject.GetObjectKind().GroupVersionKind().String(), objectMeta.GetNamespace(), objectMeta.GetName())

	return condition
}
