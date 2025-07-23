package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

const (
	TopologyConfigMapName = "topology"
)

type TopologyReconciler struct {
	Client    client.Client
	Scheme    *runtime.Scheme
	Namespace string
}

func NewTopologyReconciler(client client.Client, scheme *runtime.Scheme, namespace string) *TopologyReconciler {
	if namespace == "" {
		panic("namespace must be specified and can not be a blank string")
	}
	return &TopologyReconciler{Client: client, Scheme: scheme, Namespace: namespace}
}

func (r *TopologyReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("topology file")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TopologyConfigMapName,
			Namespace: r.Namespace,
			Labels: map[string]string{
				kuadrant.TopologyLabel: "true",
			},
		},
		Data: map[string]string{
			"topology": topology.ToDot(),
		},
	}

	// Fetch kuadrant-operator deployment for ownership
	var operator appsv1.Deployment
	if err := r.Client.Get(ctx, client.ObjectKey{
		Name:      kuadrant.OperatorDeploymentName,
		Namespace: r.Namespace,
	}, &operator); err != nil {
		logger.Error(err, "could not fetch kuadrant-operator deployment for owner reference")
		return err
	}

	if err := controllerutil.SetControllerReference(&operator, cm, r.Scheme); err != nil {
		logger.Error(err, "failed to set controller reference on topology configmap")
		return err
	}

	existingTopologyConfigMaps := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == cm.GetName() && object.GetNamespace() == cm.GetNamespace() && object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	if len(existingTopologyConfigMaps) == 0 {
		if err := r.Client.Create(ctx, cm); err != nil {
			if errors.IsAlreadyExists(err) {
				logger.Info("topology configmap already exists but not in topology index")
				return err
			}
			return err
		}
	} else {
		if len(existingTopologyConfigMaps) > 1 {
			logger.Info("multiple topology configmap found; continuing but unexpected behavior may occur")
		}

		existingCM := existingTopologyConfigMaps[0].(controller.Object).(*controller.RuntimeObject).Object.(*corev1.ConfigMap)

		if d, found := existingCM.Data["topology"]; !found || d != cm.Data["topology"] {
			if err := r.Client.Update(ctx, cm); err != nil {
				logger.Error(err, "failed to update topology configmap")
				return err
			}
		}
	}

	return nil
}
