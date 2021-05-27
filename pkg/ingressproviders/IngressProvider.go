package ingressproviders

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/ingressproviders/istioprovider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type IngressProvider interface {
	Create(ctx context.Context, apip v1beta1.APIProduct) error
	Delete(ctx context.Context, apip v1beta1.APIProduct) error
	Update(ctx context.Context, apip v1beta1.APIProduct) error
	Status(ctx context.Context, apip v1beta1.APIProduct) (bool, error)
}

// GetIngressProvider returns the IngressProvider desired
//
//	TODO: Either look for an ENV var or check the cluster capabilities
//
func GetIngressProvider(logr logr.Logger, client client.Client) IngressProvider {
	return istioprovider.New(logr, client)
}
