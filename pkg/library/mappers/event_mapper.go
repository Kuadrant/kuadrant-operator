package mappers

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type EventMapper interface {
	MapToPolicy(client.Object, kuadrant.Referrer) []reconcile.Request
}

// options

// TODO(@guicassolato): unit test
func WithLogger(logger logr.Logger) MapperOption {
	return newFuncMapperOption(func(o *MapperOptions) {
		o.Logger = logger
	})
}

type MapperOption interface {
	apply(*MapperOptions)
}

type MapperOptions struct {
	Logger logr.Logger
}

var defaultMapperOptions = MapperOptions{
	Logger: logr.Discard(),
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
