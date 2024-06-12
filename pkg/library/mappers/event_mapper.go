package mappers

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

type EventMapper interface {
	MapToPolicy(context.Context, client.Object, kuadrantgatewayapi.Policy) []reconcile.Request
}

// options

// TODO(@guicassolato): unit test
func WithLogger(logger logr.Logger) MapperOption {
	return newFuncMapperOption(func(o *MapperOptions) {
		o.Logger = logger
	})
}

func WithClient(cl client.Client) MapperOption {
	return newFuncMapperOption(func(o *MapperOptions) {
		o.Client = cl
	})
}

type MapperOption interface {
	apply(*MapperOptions)
}

type MapperOptions struct {
	Logger logr.Logger
	Client client.Client
}

var defaultMapperOptions = MapperOptions{
	Logger: logr.Discard(),
	Client: fake.NewClientBuilder().Build(),
}

func newFuncMapperOption(f func(*MapperOptions)) *funcMapperOption {
	return &funcMapperOption{
		f: f,
	}
}

type funcMapperOption struct {
	f func(*MapperOptions)
}

func (fmo *funcMapperOption) apply(opts *MapperOptions) {
	fmo.f(opts)
}

func Apply(opt ...MapperOption) MapperOptions {
	opts := defaultMapperOptions
	for _, o := range opt {
		o.apply(&opts)
	}
	return opts
}
