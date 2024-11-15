package authorino

import (
	"github.com/go-logr/logr"
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"k8s.io/apimachinery/pkg/api/meta"

	"github.com/kuadrant/kuadrant-operator/pkg/utils"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func FindAuthorinoStatusCondition(conditions []authorinooperatorv1beta1.Condition, conditionType string) *authorinooperatorv1beta1.Condition {
	for i := range conditions {
		if conditions[i].Type == authorinooperatorv1beta1.ConditionType(conditionType) {
			return &conditions[i]
		}
	}

	return nil
}

func IsAuthorinoOperatorInstalled(restMapper meta.RESTMapper, logger logr.Logger) (bool, error) {
	if ok, err := utils.IsCRDInstalled(restMapper, kuadrantv1beta1.AuthorinoGroupKind.Group, kuadrantv1beta1.AuthorinoGroupKind.Kind, authorinooperatorv1beta1.GroupVersion.Version); !ok || err != nil {
		logger.V(1).Error(err, "Authorino Operator CRD was not installed", "group", kuadrantv1beta1.AuthorinoGroupKind.Group, "kind", kuadrantv1beta1.AuthorinoGroupKind.Kind, "version", authorinooperatorv1beta1.GroupVersion.Version)
		return false, err
	}

	if ok, err := utils.IsCRDInstalled(restMapper, AuthConfigGroupKind.Group, AuthConfigGroupKind.Kind, authorinov1beta3.GroupVersion.Version); !ok || err != nil {
		logger.V(1).Error(err, "Authorino Operator CRD was not installed", "group", AuthConfigGroupKind.Group, "kind", AuthConfigGroupKind.Kind, "version", authorinov1beta3.GroupVersion.Version)
		return false, err
	}

	return true, nil
}
