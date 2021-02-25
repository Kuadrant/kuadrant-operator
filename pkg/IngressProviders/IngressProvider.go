package IngressProviders

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/IngressProviders/IstioProvider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type IngressProvider interface {
	Create(ctx context.Context, api v1beta1.Api) error
	Delete(ctx context.Context, api v1beta1.Api) error
	Update(ctx context.Context, api v1beta1.Api) error
	Validate(api v1beta1.Api) error
	Status(api v1beta1.Api) (bool, error)
}

// GetIngressProvider returns the IngressProvider desired
//
//	TODO: Either look for an ENV var or check the cluster capabilities
//
func GetIngressProvider(logr logr.Logger, client client.Client) IngressProvider {
	return IstioProvider.New(logr, client)
}
