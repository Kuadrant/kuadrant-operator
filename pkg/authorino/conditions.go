package authorino

import (
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
)

func FindAuthorinoStatusCondition(conditions []authorinov1beta1.Condition, conditionType string) *authorinov1beta1.Condition {
	for i := range conditions {
		if conditions[i].Type == authorinov1beta1.ConditionType(conditionType) {
			return &conditions[i]
		}
	}

	return nil
}
