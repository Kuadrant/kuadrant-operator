package utils

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"

	"github.com/kuadrant/policy-machinery/controller"
)

func LoggerFromContext(ctx context.Context) logr.Logger {
	return controller.LoggerFromContext(ctx)
}

func DynamicClientFromContext(ctx context.Context) (*dynamic.DynamicClient, error) {
	dynamicClient, ok := ctx.Value((*dynamic.DynamicClient)(nil)).(*dynamic.DynamicClient)
	if !ok {
		return nil, errors.New("failed to retrieve dynamic client from context")
	}
	return dynamicClient, nil
}
