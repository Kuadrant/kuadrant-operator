package mappers

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

type EventMapper interface {
	MapToPolicy(client.Object, common.Referrer) []reconcile.Request
}

// options

// TODO(@guicassolato): unit test
func WithLogger(logger logr.Logger) mapperOption {
	return newFuncMapperOption(func(o *mapperOptions) {
		o.logger = logger
	})
}

type mapperOption interface {
	apply(*mapperOptions)
}

type mapperOptions struct {
	logger logr.Logger
}

var defaultMapperOptions = mapperOptions{
	logger: logr.Discard(),
}

func newFuncMapperOption(f func(*mapperOptions)) *funcMapperOption {
	return &funcMapperOption{
		f: f,
	}
}

type funcMapperOption struct {
	f func(*mapperOptions)
}

func (fmo *funcMapperOption) apply(opts *mapperOptions) {
	fmo.f(opts)
}

func apply(opt ...mapperOption) mapperOptions {
	opts := defaultMapperOptions
	for _, o := range opt {
		o.apply(&opts)
	}
	return opts
}
