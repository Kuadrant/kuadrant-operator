package limitador

import (
	"github.com/go-logr/logr"
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
)

func IsLimitadorOperatorInstalled(restMapper meta.RESTMapper, logger logr.Logger) (bool, error) {
	if ok, err := utils.IsCRDInstalled(restMapper, kuadrantv1beta1.LimitadorGroupKind.Group, kuadrantv1beta1.LimitadorGroupKind.Kind, limitadorv1alpha1.GroupVersion.Version); !ok || err != nil {
		logger.V(1).Error(err, "Limitador Operator CRD was not installed", "group", kuadrantv1beta1.AuthorinoGroupKind.Group, "kind", kuadrantv1beta1.AuthorinoGroupKind.Kind, "version", authorinooperatorv1beta1.GroupVersion.Version)
		return false, err
	}

	return true, nil
}
