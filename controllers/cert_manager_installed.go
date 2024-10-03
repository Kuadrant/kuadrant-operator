package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/api/meta"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

const IsCertManagerInstalledKey = "IsCertManagerInstalled"

func NewIsCertManagerInstalledReconciler(restMapper meta.RESTMapper) IsCertManagerInstalledReconciler {
	return IsCertManagerInstalledReconciler{
		restMapper: restMapper,
	}
}

type IsCertManagerInstalledReconciler struct {
	restMapper meta.RESTMapper
}

func (t IsCertManagerInstalledReconciler) Check(ctx context.Context, _ []controller.ResourceEvent, _ *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("IsCertManagerInstalledReconciler").WithName("Reconcile")
	isCertManagerInstalled, err := kuadrantgatewayapi.IsCertManagerInstalled(t.restMapper, logger)

	if err != nil {
		logger.Error(err, "error checking IsCertManagerInstalled")
	}

	s.Store(IsCertManagerInstalledKey, isCertManagerInstalled)

	return nil
}
