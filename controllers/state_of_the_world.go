package controllers

import (
	"context"
	"os"

	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	istiov1 "istio.io/client-go/pkg/apis/security/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

func SetupWithManagerA(manager ctrlruntime.Manager, client *dynamic.DynamicClient) *controller.Controller {
	logger := controller.CreateAndSetLogger()
	controllerOpts := []controller.ControllerOption{
		controller.ManagedBy(manager),
		controller.WithLogger(logger),
		controller.WithClient(client),
		controller.WithRunnable("gateway watcher", buildWatcher(&gwapiv1.Gateway{}, controller.GatewaysResource, metav1.NamespaceAll)),
		controller.WithRunnable("httproute watcher", buildWatcher(&gwapiv1.HTTPRoute{}, controller.HTTPRoutesResource, metav1.NamespaceAll)),
		controller.WithRunnable("dnspolicy watcher", buildWatcher(&kuadrantv1alpha1.DNSPolicy{}, kuadrantv1alpha1.DNSPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("tlspolicy watcher", buildWatcher(&kuadrantv1alpha1.TLSPolicy{}, kuadrantv1alpha1.TLSPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("authpolicy watcher", buildWatcher(&kuadrantv1beta2.AuthPolicy{}, kuadrantv1beta2.AuthPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("ratelimitpolicy watcher", buildWatcher(&kuadrantv1beta2.RateLimitPolicy{}, kuadrantv1beta2.RateLimitPoliciesResource, metav1.NamespaceAll)),
		controller.WithPolicyKinds(
			kuadrantv1alpha1.DNSPolicyKind,
			kuadrantv1alpha1.TLSPolicyKind,
			kuadrantv1beta2.AuthPolicyKind,
			kuadrantv1beta2.RateLimitPolicyKind,
		),
		controller.WithReconcile(buildReconciler(client)),
	}

	return controller.NewController(controllerOpts...)
}

func buildWatcher[T controller.Object](obj T, resource schema.GroupVersionResource, namespace string, options ...controller.RunnableBuilderOption[T]) controller.RunnableBuilder {
	return controller.Watch(obj, resource, namespace, options...)
}

func buildReconciler(client *dynamic.DynamicClient) controller.ReconcileFunc {
	effectivePolicesReconciler := EffectivePolicesReconciler{Client: client}

	commonAuthPolicyResourceEventMatchers := []controller.ResourceEventMatcher{
		{Kind: ptr.To(controller.GatewayClassKind)},
		{Kind: ptr.To(controller.GatewayKind), EventType: ptr.To(controller.CreateEvent)},
		{Kind: ptr.To(controller.GatewayKind), EventType: ptr.To(controller.UpdateEvent)},
		{Kind: ptr.To(controller.HTTPRouteKind)},
		{Kind: ptr.To(kuadrantv1beta2.AuthPolicyKind)},
	}

	istioGatewayProvider := &IstioGatewayProvider{Client: client}
	effectivePolicesReconciler.ReconcileFuncs = append(effectivePolicesReconciler.ReconcileFuncs, (&controller.Subscription{
		ReconcileFunc: istioGatewayProvider.Sample,
		Events:        append(commonAuthPolicyResourceEventMatchers, controller.ResourceEventMatcher{Kind: ptr.To(IstioAuthorizationPolicyKind)}),
	}).Reconcile)
	effectivePolicesReconciler.ReconcileFuncs = append(effectivePolicesReconciler.ReconcileFuncs, (&controller.Subscription{
		ReconcileFunc: istioGatewayProvider.Sample,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(controller.GatewayKind), EventType: ptr.To(controller.DeleteEvent)},
		},
	}).Reconcile)

	reconciler := &controller.Workflow{
		Precondition: func(ctx context.Context, resourceEvents []controller.ResourceEvent, _ *machinery.Topology) {
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
			}
		},
		Tasks: []controller.ReconcileFunc{
			(&TopologyFileReconciler{}).Reconcile,
			effectivePolicesReconciler.Reconcile,
		},
	}

	return reconciler.Run
}

type EffectivePolicesReconciler struct {
	Client         *dynamic.DynamicClient
	ReconcileFuncs []controller.ReconcileFunc
}

func (r *EffectivePolicesReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, _ *machinery.Topology) {
	logger := controller.LoggerFromContext(ctx).WithName("effective polices reconciler")
	logger.Info("reconciling effective polices reconciler")
}

const topologyFile = "topology.dot"

type TopologyFileReconciler struct{}

func (r *TopologyFileReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology) {
	logger := controller.LoggerFromContext(ctx).WithName("topology file")

	file, err := os.Create(topologyFile)
	if err != nil {
		logger.Error(err, "failed to create topology file")
		return
	}
	defer file.Close()
	_, err = file.WriteString(topology.ToDot())
	if err != nil {
		logger.Error(err, "failed to write to topology file")
		return
	}
}

var (
	IstioAuthorizationPolicyKind       = schema.GroupKind{Group: istiov1.GroupName, Kind: "AuthorizationPolicy"}
	IstioAuthorizationPoliciesResource = istiov1.SchemeGroupVersion.WithResource("authorizationpolicies")
)

type IstioGatewayProvider struct {
	Client *dynamic.DynamicClient
}

func (r IstioGatewayProvider) Sample(ctx context.Context, _ []controller.ResourceEvent, _ *machinery.Topology) {
	logger := controller.LoggerFromContext(ctx).WithName("istio gateway provider")
	logger.Info("Sample function ran as expected")
}
