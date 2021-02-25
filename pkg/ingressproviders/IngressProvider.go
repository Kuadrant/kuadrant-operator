package ingressproviders

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/ingressproviders/istioprovider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type IngressProvider interface {
	Create(ctx context.Context, api v1beta1.API) error
	Delete(ctx context.Context, api v1beta1.API) error
	Update(ctx context.Context, api v1beta1.API) error
	Validate(api v1beta1.API) error
	Status(api v1beta1.API) (bool, error)
}

// GetIngressProvider returns the IngressProvider desired
//
//	TODO: Either look for an ENV var or check the cluster capabilities
//
func GetIngressProvider(logr logr.Logger, client client.Client) IngressProvider {
	return istioprovider.New(logr, client)
}
