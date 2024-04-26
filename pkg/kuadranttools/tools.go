package kuadranttools

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func KuadrantFromGateway(ctx context.Context, cl client.Client, gw *gatewayapiv1.Gateway) (*kuadrantv1beta1.Kuadrant, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	kNS, isSet := gw.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
	if !isSet {
		logger.Info("gateway not assigned to kuadrant")
		return nil, nil
	}

	// Currently only one kuadrant CR is supported
	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	err = cl.List(ctx, kuadrantList, client.InNamespace(kNS))
	if err != nil {
		return nil, err
	}

	if len(kuadrantList.Items) == 0 {
		logger.V(1).Info("no kuadrant instance found", "namespace", kNS)
		return nil, nil
	}

	if len(kuadrantList.Items) > 1 {
		return nil, fmt.Errorf("multiple kuadrant instances found (%d kuadrant instances)",
			len(kuadrantList.Items))
	}

	return &kuadrantList.Items[0], nil
}
