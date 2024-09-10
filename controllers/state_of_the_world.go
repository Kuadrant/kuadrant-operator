package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=list;watch

func NewPolicyMachineryController(manager ctrlruntime.Manager, client *dynamic.DynamicClient, logger logr.Logger) *controller.Controller {
	controllerOpts := []controller.ControllerOption{
		controller.ManagedBy(manager),
		controller.WithLogger(logger),
		controller.WithClient(client),
		controller.WithRunnable("kuadrant watcher", controller.Watch(&kuadrantv1beta1.Kuadrant{}, kuadrantv1beta1.KuadrantResource, metav1.NamespaceAll)),
		controller.WithRunnable("gatewayclass watcher", controller.Watch(&gwapiv1.GatewayClass{}, controller.GatewayClassesResource, metav1.NamespaceAll)),
		controller.WithRunnable("gateway watcher", controller.Watch(&gwapiv1.Gateway{}, controller.GatewaysResource, metav1.NamespaceAll)),
		controller.WithRunnable("httproute watcher", controller.Watch(&gwapiv1.HTTPRoute{}, controller.HTTPRoutesResource, metav1.NamespaceAll)),
		controller.WithRunnable("dnspolicy watcher", controller.Watch(&kuadrantv1alpha1.DNSPolicy{}, kuadrantv1alpha1.DNSPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("tlspolicy watcher", controller.Watch(&kuadrantv1alpha1.TLSPolicy{}, kuadrantv1alpha1.TLSPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("authpolicy watcher", controller.Watch(&kuadrantv1beta2.AuthPolicy{}, kuadrantv1beta2.AuthPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("ratelimitpolicy watcher", controller.Watch(&kuadrantv1beta2.RateLimitPolicy{}, kuadrantv1beta2.RateLimitPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("configmap watcher", controller.Watch(&corev1.ConfigMap{}, controller.ConfigMapsResource, metav1.NamespaceAll, controller.FilterResourcesByLabel[*corev1.ConfigMap](fmt.Sprintf("%s=true", kuadrant.TopologyAnnotation)))),
		controller.WithPolicyKinds(
			kuadrantv1alpha1.DNSPolicyKind,
			kuadrantv1alpha1.TLSPolicyKind,
			kuadrantv1beta2.AuthPolicyKind,
			kuadrantv1beta2.RateLimitPolicyKind,
		),
		controller.WithObjectKinds(
			kuadrantv1beta1.KuadrantKind,
			schema.GroupKind{Group: corev1.GroupName, Kind: "ConfigMap"}),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses,
			kuadrantv1beta1.LinkKuadrantToTopologyConfigMap,
		),
		controller.WithReconcile(buildReconciler(client)),
	}

	return controller.NewController(controllerOpts...)
}

func buildReconciler(client *dynamic.DynamicClient) controller.ReconcileFunc {
	reconciler := &controller.Workflow{
		Precondition: NewEventLogger().Log,
		Tasks: []controller.ReconcileFunc{
			NewTopologyFileReconciler(client).Reconcile,
		},
	}

	return reconciler.Run
}

type TopologyFileReconciler struct {
	Client *dynamic.DynamicClient
}

func NewTopologyFileReconciler(client *dynamic.DynamicClient) *TopologyFileReconciler {
	return &TopologyFileReconciler{Client: client}
}

func (r *TopologyFileReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error) {
	logger := controller.LoggerFromContext(ctx).WithName("topology file")

	if len(topology.Objects().Roots()) == 0 {
		logger.Info("no Kuadrant CR found, can create topology configmap")
		return
	}

	kuadrantCR := topology.Objects().Roots()[0]
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "topology",
			Namespace: kuadrantCR.GetNamespace(),
			Labels:    map[string]string{kuadrant.TopologyAnnotation: "true"},
		},
		Data: map[string]string{
			"topology": topology.ToDot(),
		},
	}
	configMapRes := controller.ConfigMapsResource
	unstructuredCM, _ := controller.Destruct(cm)

	targets := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == cm.GetName() && object.GetNamespace() == cm.GetNamespace() && object.GroupVersionKind().Kind == cm.GroupVersionKind().Kind
	})

	if len(targets) == 0 {
		_, err := r.Client.Resource(configMapRes).Namespace(cm.Namespace).Create(ctx, unstructuredCM, metav1.CreateOptions{})
		if err != nil {
			logger.Error(err, "failed to write topology configmap")
		}
		return
	}

	if len(targets) > 1 {
		logger.Info("multiple topology configmaps found, continuing but unexpected behaviour may occur")
	}
	target := targets[0].(controller.Object).(*controller.RuntimeObject)
	cmTarget := target.Object.(*corev1.ConfigMap)

	if d, found := cmTarget.Data["topology"]; !found || strings.Compare(d, cm.Data["topology"]) != 0 {
		_, err := r.Client.Resource(configMapRes).Namespace(cm.Namespace).Update(ctx, unstructuredCM, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "failed to update topology configmap")
		}
	}
}

type EventLogger struct{}

func NewEventLogger() *EventLogger {
	return &EventLogger{}
}

func (e *EventLogger) Log(ctx context.Context, resourceEvents []controller.ResourceEvent, _ *machinery.Topology, err error) {
	logger := controller.LoggerFromContext(ctx).WithName("event logger")
	for _, event := range resourceEvents {
		// log the event
		obj := event.OldObject
		if obj == nil {
			obj = event.NewObject
		}
		values := []any{
			"type", event.EventType.String(),
			"kind", obj.GetObjectKind().GroupVersionKind().Kind,
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
		}
		if event.EventType == controller.UpdateEvent && logger.V(1).Enabled() {
			values = append(values, "diff", cmp.Diff(event.OldObject, event.NewObject))
		}
		logger.Info("new event", values...)
		if err != nil {
			logger.Error(err, "error passed to reconcile")
		}
	}
}
