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
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"

	"github.com/kuadrant/kuadrant-operator/internal/trace"
)

// TracingConfigMapName is the name of the ConfigMap that controls the operator's
// control-plane trace exporter at runtime. Creating or updating this ConfigMap
// reconfigures the trace provider without restarting the operator. Deleting it
// reverts to the env var configuration captured at startup.
//
// This is a shared convention: other Kuadrant operators (limitador-operator,
// authorino-operator, dns-operator) watch the same ConfigMap name in their
// namespace, allowing a single ConfigMap to configure tracing across all
// control-plane operators without introducing cross-operator CRD dependencies.
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
type TracingConfigMapReconciler struct {
	namespace string
	provider  *trace.DynamicProvider
}

func NewTracingConfigMapReconciler(provider *trace.DynamicProvider, namespace string) *TracingConfigMapReconciler {
	return &TracingConfigMapReconciler{
		namespace: namespace,
		provider:  provider,
	}
}

func (r *TracingConfigMapReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Run,
		Events: []controller.ResourceEventMatcher{
			{
				Kind:            ptr.To(ConfigMapGroupKind),
				ObjectNamespace: r.namespace,
				ObjectName:      TracingConfigMapName,
			},
		},
	}
}

func (r *TracingConfigMapReconciler) Run(eventCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(eventCtx).WithName("TracingConfigMapReconciler")
	ctx := logr.NewContext(eventCtx, logger)

	cmObjs := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == TracingConfigMapName &&
			object.GetNamespace() == r.namespace &&
			object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	if len(cmObjs) == 0 {
		logger.V(1).Info("tracing configmap not found, reverting to env var config")
		if err := r.provider.RevertToFallback(ctx); err != nil {
			logger.Error(err, "failed to revert trace provider to fallback")
			return err
		}
		return nil
	}

	cm := cmObjs[0].(*controller.RuntimeObject).Object.(*corev1.ConfigMap)
	endpoint, ok := cm.Data[tracingEndpointKey]
	if !ok || endpoint == "" {
		logger.Info("tracing configmap found but missing endpoint key, reverting to env var config")
		if err := r.provider.RevertToFallback(ctx); err != nil {
			logger.Error(err, "failed to revert trace provider to fallback")
			return err
		}
		return nil
	}
	insecure, _ := strconv.ParseBool(cm.Data[tracingInsecureKey])

	logger.V(1).Info("reconfiguring trace provider", "endpoint", endpoint, "insecure", insecure)
	if err := r.provider.Reconfigure(ctx, endpoint, insecure); err != nil {
		logger.Error(err, "failed to reconfigure trace provider")
		return err
	}

	return nil
}
