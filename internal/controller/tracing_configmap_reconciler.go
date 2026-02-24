/*
Copyright 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/internal/log"
	"github.com/kuadrant/kuadrant-operator/internal/trace"
)

// TracingConfigMapName is the name of the ConfigMap that controls the operator's
// control-plane trace exporter at runtime. Creating or updating this ConfigMap
// reconfigures the trace provider without restarting the operator. Deleting it
// reverts to the env var configuration captured at startup.
const TracingConfigMapName = "kuadrant-tracing"

const (
	tracingEndpointKey = "endpoint"
	tracingInsecureKey = "insecure"
)

// TracingConfigMapReconciler watches the kuadrant-tracing ConfigMap and
// reconfigures the operator's OTEL trace provider whenever it changes.
//
// ConfigMap data keys:
//   - endpoint: OTLP collector URL (e.g. "rpc://otel-collector:4317" or "http://…")
//   - insecure: "true" to skip TLS verification (optional, defaults to false)
//
// This reconciler uses a plain controller-runtime reconcile loop rather than the
// policy machinery subscription pattern, because it configures the operator itself
// rather than reconciling cluster resources.
type TracingConfigMapReconciler struct {
	client    client.Client
	namespace string
	provider  *trace.DynamicProvider
}

func NewTracingConfigMapReconciler(mgr ctrl.Manager, namespace string, provider *trace.DynamicProvider) *TracingConfigMapReconciler {
	return &TracingConfigMapReconciler{
		client:    mgr.GetClient(),
		namespace: namespace,
		provider:  provider,
	}
}

func (r *TracingConfigMapReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.Log.WithName("TracingConfigMapReconciler").WithValues("configmap", req.NamespacedName)

	cm := &corev1.ConfigMap{}
	if err := r.client.Get(ctx, req.NamespacedName, cm); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("tracing configmap deleted, reverting to env var config")
			if revertErr := r.provider.RevertToFallback(ctx); revertErr != nil {
				logger.Error(revertErr, "failed to revert trace provider to fallback")
				return reconcile.Result{}, revertErr
			}
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	endpoint := cm.Data[tracingEndpointKey]
	insecure, _ := strconv.ParseBool(cm.Data[tracingInsecureKey])

	logger.Info("reconfiguring trace provider", "endpoint", endpoint, "insecure", insecure)
	if err := r.provider.Reconfigure(ctx, endpoint, insecure); err != nil {
		logger.Error(err, "failed to reconfigure trace provider")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *TracingConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("tracing-configmap").
		For(&corev1.ConfigMap{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			return obj.GetName() == TracingConfigMapName && obj.GetNamespace() == r.namespace
		}))).
		Complete(r)
}
