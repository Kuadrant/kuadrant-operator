package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

const (
	TopologyConfigMapName = "topology"
)

type TopologyReconciler struct {
	Client    *dynamic.DynamicClient
	Namespace string
}

func NewTopologyReconciler(client *dynamic.DynamicClient, namespace string) *TopologyReconciler {
	if namespace == "" {
		panic("namespace must be specified and can not be a blank string")
	}
	return &TopologyReconciler{Client: client, Namespace: namespace}
}

func (r *TopologyReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("topology file").WithValues("context", ctx)

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      TopologyConfigMapName,
			Namespace: r.Namespace,
			Labels:    map[string]string{kuadrant.TopologyLabel: "true"},
		},
		Data: map[string]string{
			"topology": topology.ToDot(),
		},
	}
	unstructuredCM, err := controller.Destruct(cm)
	if err != nil {
		logger.Error(err, "failed to destruct topology configmap")
		return err
	}

	resource := r.Client.Resource(controller.ConfigMapsResource)
	_, err = resource.Namespace(cm.Namespace).Apply(
		ctx, unstructuredCM.GetName(), unstructuredCM, metav1.ApplyOptions{FieldManager: FieldManager, Force: true},
	)
	if err != nil {
		logger.Error(err, "failed to apply topology configmap")
	}

	return nil
}
