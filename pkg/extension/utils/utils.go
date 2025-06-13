package utils

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/policy-machinery/controller"
)

type clientKeyType struct{}
type schemeKeyType struct{}

var ClientKey = clientKeyType{}
var SchemeKey = schemeKeyType{}

func LoggerFromContext(ctx context.Context) logr.Logger {
	return controller.LoggerFromContext(ctx)
}

func ClientFromContext(ctx context.Context) (client.Client, error) {
	client, ok := ctx.Value(ClientKey).(client.Client)
	if !ok {
		return nil, errors.New("failed to retrieve the client from context")
	}
	return client, nil
}

func SchemeFromContext(ctx context.Context) (*runtime.Scheme, error) {
	scheme, ok := ctx.Value(SchemeKey).(*runtime.Scheme)
	if !ok {
		return nil, errors.New("failed to retrieve scheme from context")
	}
	return scheme, nil
}
