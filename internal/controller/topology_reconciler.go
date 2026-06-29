package controllers

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

const (
	TopologyConfigMapName = "topology"
	// TODO: This size cap is a temporary workaround. The topology can outgrow a single ConfigMap,
	// and other resources that consume it (e.g. the console-plugin) will need coordinated changes
	// to support a different storage or serialization strategy.
	maxTopologyBytes     = 900 * 1024 // ~900KB, safely under the 1MB ConfigMap limit
	oversizedPlaceholder = `digraph { "error" [label="Topology exceeds ConfigMap 1MB limit"] }`
)

type TopologyReconciler struct {
	Client    dynamic.Interface
	Namespace string
}

func NewTopologyReconciler(client dynamic.Interface, namespace string) *TopologyReconciler {
	if namespace == "" {
		panic("namespace must be specified and can not be a blank string")
	}
	return &TopologyReconciler{Client: client, Namespace: namespace}
}

func (r *TopologyReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("topology file").WithValues("context", ctx)
	tracer := controller.TracerFromContext(ctx)
	ctx, span := tracer.Start(ctx, "TopologyReconciler.Reconcile")
	defer span.End()

	span.SetAttributes(
		attribute.String("configmap.name", TopologyConfigMapName),
		attribute.String("configmap.namespace", r.Namespace),
	)

	topologyData := topology.ToDot()
	topologySize := len(topologyData)
	span.SetAttributes(attribute.Int("topology.size_bytes", topologySize))

	if topologySize > maxTopologyBytes {
		logger.Info("topology data exceeds ConfigMap size limit, using placeholder",
			"size_bytes", topologySize, "max_bytes", maxTopologyBytes)
		span.RecordError(fmt.Errorf("topology data is %d bytes, exceeds %d byte limit", topologySize, maxTopologyBytes))
		span.SetStatus(codes.Error, "topology exceeds ConfigMap size limit")
		span.AddEvent("topology data replaced with placeholder")
		topologyData = oversizedPlaceholder
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TopologyConfigMapName,
			Namespace: r.Namespace,
			Labels:    map[string]string{kuadrant.TopologyLabel: "true"},
		},
		Data: map[string]string{
			"topology": topologyData,
		},
	}
	unstructuredCM, err := controller.Destruct(cm)
	if err != nil {
		logger.Error(err, "failed to destruct topology configmap")
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to destruct topology configmap")
		return err
	}

	existingTopologyConfigMaps := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == cm.GetName() && object.GetNamespace() == cm.GetNamespace() && object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	if len(existingTopologyConfigMaps) == 0 {
		_, err := r.Client.Resource(controller.ConfigMapsResource).Namespace(cm.Namespace).Create(ctx, unstructuredCM, metav1.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			logger.Info("already created topology configmap, must not be in topology yet")
			span.AddEvent("configmap already exists but not in topology yet")
			return err
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to create topology configmap")
		} else {
			span.AddEvent("topology configmap created")
			span.SetStatus(codes.Ok, "")
		}
		return err
	}

	if len(existingTopologyConfigMaps) > 1 {
		logger.Info("multiple topology configmaps found, continuing but unexpected behaviour may occur")
	}
	existingTopologyConfigMap := existingTopologyConfigMaps[0].(controller.Object).(*controller.RuntimeObject)
	cmTopology := existingTopologyConfigMap.Object.(*corev1.ConfigMap)

	if d, found := cmTopology.Data["topology"]; !found || strings.Compare(d, cm.Data["topology"]) != 0 {
		_, err := r.Client.Resource(controller.ConfigMapsResource).Namespace(cm.Namespace).Update(ctx, unstructuredCM, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "failed to update topology configmap")
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to update topology configmap")
		} else {
			span.AddEvent("topology configmap updated")
			span.SetStatus(codes.Ok, "")
		}
		return err
	}

	span.AddEvent("topology configmap unchanged")
	span.SetStatus(codes.Ok, "")
	return nil
}
