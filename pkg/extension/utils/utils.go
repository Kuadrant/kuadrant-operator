// Package utils provides context helper functions and keys used by extension
// reconcile functions to access controller-runtime primitives (logger, client
// and scheme). Values are injected into the context by the extension
// controller before invoking user Reconcile logic. These helpers intentionally
// avoid introducing additional dependencies for extension authors.
package utils

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/policy-machinery/controller"
)

// clientKeyType and schemeKeyType are unexported marker types used as context
// keys to avoid collisions.
type clientKeyType struct{}
type schemeKeyType struct{}

// ClientKey is the context key under which the controller-runtime client is
// stored.
var ClientKey = clientKeyType{}

// SchemeKey is the context key under which the runtime.Scheme is stored.
var SchemeKey = schemeKeyType{}

// LoggerFromContext returns a logger from the context.
func LoggerFromContext(ctx context.Context) logr.Logger {
	return controller.LoggerFromContext(ctx)
}

// ClientFromContext returns a client from the context.
func ClientFromContext(ctx context.Context) (client.Client, error) {
	client, ok := ctx.Value(ClientKey).(client.Client)
	if !ok {
		return nil, errors.New("failed to retrieve the client from context")
	}
	return client, nil
}

// SchemeFromContext returns a scheme from the context.
func SchemeFromContext(ctx context.Context) (*runtime.Scheme, error) {
	scheme, ok := ctx.Value(SchemeKey).(*runtime.Scheme)
	if !ok {
		return nil, errors.New("failed to retrieve scheme from context")
	}
	return scheme, nil
}
