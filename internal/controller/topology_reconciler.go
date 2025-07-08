package controllers

import (
	"context"
	"strings"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

const (
	TopologyConfigMapName  = "topology"
	OperatorDeploymentName = "kuadrant-operator"
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
	logger := controller.LoggerFromContext(ctx).WithName("topology file")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TopologyConfigMapName,
			Namespace: r.Namespace,
			Labels:    map[string]string{kuadrant.TopologyLabel: "true"},
		},
		Data: map[string]string{
			"topology": topology.ToDot(),
		},
	}

	// Attach owner ref to configmap from kuadrant-operator to link their lifecycles
	if ownerRef, err := getOwnerRef(ctx, r.Namespace, OperatorDeploymentName); err == nil {
		cm.OwnerReferences = []metav1.OwnerReference{*ownerRef}
	} else {
		logger.Error(err, "failed to set owner reference on topology configmap")
	}

	unstructuredCM, err := controller.Destruct(cm)
	if err != nil {
		logger.Error(err, "failed to destruct topology configmap")
		return err
	}

	existingTopologyConfigMaps := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == cm.GetName() && object.GetNamespace() == cm.GetNamespace() && object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	if len(existingTopologyConfigMaps) == 0 {
		_, err := r.Client.Resource(controller.ConfigMapsResource).Namespace(cm.Namespace).Create(ctx, unstructuredCM, metav1.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			// This error can happen when the operator is starting, and the create event for the topology has not being processed.
			logger.Info("already created topology configmap, must not be in topology yet")
			return err
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
		}
		return err
	}

	return nil
}

// Helper function for controller of owner ref
func pointerBool(b bool) *bool {
	return &b
}

// Returns an owner reference based on kuadrant-operator deployment
func getOwnerRef(ctx context.Context, namespace, name string) (*metav1.OwnerReference, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	deploy, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &metav1.OwnerReference{
		APIVersion: appsv1.SchemeGroupVersion.String(),
		Kind:       "Deployment",
		Name:       deploy.Name,
		UID:        deploy.UID,
		Controller: pointerBool(true),
	}, nil
}
