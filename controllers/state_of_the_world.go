package controllers

import (
	"context"
	"fmt"
	"strings"
	"sync"

	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclientgosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	"github.com/kuadrant/kuadrant-operator/pkg/istio"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

var (
	ConfigMapGroupKind = schema.GroupKind{Group: corev1.GroupName, Kind: "ConfigMap"}
	IstioOperatorKind  = schema.GroupKind{Group: iopv1alpha1.IstioOperatorGVR.Group, Kind: "IstioOperator"}
	operatorNamespace  = env.GetString("OPERATOR_NAMESPACE", "")
)

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=list;watch

func NewPolicyMachineryController(manager ctrlruntime.Manager, client *dynamic.DynamicClient, logger logr.Logger) *controller.Controller {
	controllerOpts := []controller.ControllerOption{
		controller.ManagedBy(manager),
		controller.WithLogger(logger),
		controller.WithClient(client),
		controller.WithRunnable("kuadrant watcher", controller.Watch(&kuadrantv1beta1.Kuadrant{}, kuadrantv1beta1.KuadrantResource, metav1.NamespaceAll)),
		controller.WithRunnable("dnspolicy watcher", controller.Watch(&kuadrantv1alpha1.DNSPolicy{}, kuadrantv1alpha1.DNSPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("tlspolicy watcher", controller.Watch(&kuadrantv1alpha1.TLSPolicy{}, kuadrantv1alpha1.TLSPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("authpolicy watcher", controller.Watch(&kuadrantv1beta2.AuthPolicy{}, kuadrantv1beta2.AuthPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("ratelimitpolicy watcher", controller.Watch(&kuadrantv1beta2.RateLimitPolicy{}, kuadrantv1beta2.RateLimitPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("topology configmap watcher", controller.Watch(&corev1.ConfigMap{}, controller.ConfigMapsResource, operatorNamespace, controller.FilterResourcesByLabel[*corev1.ConfigMap](fmt.Sprintf("%s=true", kuadrant.TopologyLabel)))),
		controller.WithPolicyKinds(
			kuadrantv1alpha1.DNSPolicyKind,
			kuadrantv1alpha1.TLSPolicyKind,
			kuadrantv1beta2.AuthPolicyKind,
			kuadrantv1beta2.RateLimitPolicyKind,
		),
		controller.WithObjectKinds(
			kuadrantv1beta1.KuadrantKind,
			ConfigMapGroupKind),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses,
		),
		controller.WithReconcile(buildReconciler(client)),
	}

	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(manager.GetRESTMapper())
	if err != nil || !ok {
		logger.Info("gateway api is not installed, skipping watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("gatewayclass watcher", controller.Watch(&gwapiv1.GatewayClass{}, controller.GatewayClassesResource, metav1.NamespaceAll)),
			controller.WithRunnable("gateway watcher", controller.Watch(&gwapiv1.Gateway{}, controller.GatewaysResource, metav1.NamespaceAll)),
			controller.WithRunnable("httproute watcher", controller.Watch(&gwapiv1.HTTPRoute{}, controller.HTTPRoutesResource, metav1.NamespaceAll)),
		)
	}

	ok, err = envoygateway.IsEnvoyGatewayInstalled(manager.GetRESTMapper())
	if err != nil || !ok {
		logger.Info("envoygateway is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("envoypatchpolicy watcher", controller.Watch(&egv1alpha1.EnvoyPatchPolicy{}, envoygateway.EnvoyPatchPoliciesResource, metav1.NamespaceAll)),
			controller.WithRunnable("envoyextensionpolicy watcher", controller.Watch(&egv1alpha1.EnvoyExtensionPolicy{}, envoygateway.EnvoyExtensionPoliciesResource, metav1.NamespaceAll)),
			controller.WithRunnable("envoysecuritypolicy watcher", controller.Watch(&egv1alpha1.SecurityPolicy{}, envoygateway.SecurityPoliciesResource, metav1.NamespaceAll)),
			controller.WithObjectKinds(
				envoygateway.EnvoyPatchPolicyGroupKind,
				envoygateway.EnvoyExtensionPolicyGroupKind,
				envoygateway.SecurityPolicyGroupKind,
			),
			// TODO: add object links
		)
		// TODO: add specific tasks to workflow
	}

	ok, err = istio.IsIstioInstalled(manager.GetRESTMapper())
	if err != nil || !ok {
		logger.Info("istio is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("envoyfilter watcher", controller.Watch(&istioclientnetworkingv1alpha3.EnvoyFilter{}, istio.EnvoyFiltersResource, metav1.NamespaceAll)),
			controller.WithRunnable("wasmplugin watcher", controller.Watch(&istioclientgoextensionv1alpha1.WasmPlugin{}, istio.WasmPluginsResource, metav1.NamespaceAll)),
			controller.WithRunnable("authorizationpolicy watcher", controller.Watch(&istioclientgosecurityv1beta1.AuthorizationPolicy{}, istio.AuthorizationPoliciesResource, metav1.NamespaceAll)),
			controller.WithRunnable("istio configmap watcher", controller.Watch(
				&corev1.ConfigMap{},
				controller.ConfigMapsResource,
				controlPlaneProviderNamespace(),
				controller.FilterResourcesByField[*corev1.ConfigMap](fmt.Sprintf("metadata.name=%v", controlPlaneConfigMapName())),
			)),
			controller.WithObjectKinds(
				istio.EnvoyFilterGroupKind,
				istio.WasmPluginGroupKind,
				istio.AuthorizationPolicyGroupKind,
				ConfigMapGroupKind,
			),
			// TODO: add object links
		)
		// TODO: add istio specific tasks to workflow

		ok, err = kuadrantgatewayapi.IsCRDInstalled(manager.GetRESTMapper(), iopv1alpha1.IstioOperatorGVK.Group, iopv1alpha1.IstioOperatorGVK.Kind, iopv1alpha1.IstioOperatorGVK.Version)
		if err == nil && ok {
			controllerOpts = append(controllerOpts,
				controller.WithRunnable("istio operator watcher", controller.Watch(&iopv1alpha1.IstioOperator{}, iopv1alpha1.IstioOperatorGVR, metav1.NamespaceAll)),
				controller.WithObjectKinds(
					schema.GroupKind{Group: iopv1alpha1.IstioOperatorGVR.Group, Kind: "IstioOperator"},
				),
				//controller.WithObjectLinks(LinkIstioOperatorToConfigmap),
			)
		} else {
			logger.Info("istio operator CRD not installed, falling back to watch the istio CR", "err", err)
			controllerOpts = append(controllerOpts,
				controller.WithRunnable("istio CR watcher", controller.Watch(
					&istiov1alpha1.Istio{},
					istiov1alpha1.GroupVersion.WithResource("istios"),
					controlPlaneProviderNamespace(),
					controller.FilterResourcesByField[*istiov1alpha1.Istio](fmt.Sprintf("metadata.name=%v", istioCRName)),
				),
				),
				controller.WithObjectKinds(schema.GroupKind{Group: istiov1alpha1.GroupVersion.Group, Kind: "Istio"}),
			)
		}
	}

	ok, err = kuadrantgatewayapi.IsCertManagerInstalled(manager.GetRESTMapper(), logger)
	if err != nil || !ok {
		logger.Info("cert manager is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("certificate watcher", controller.Watch(&certmanagerv1.Certificate{}, CertManagerCertificatesResource, metav1.NamespaceAll)),
			controller.WithRunnable("issuers watcher", controller.Watch(&certmanagerv1.Issuer{}, CertManagerIssuersResource, metav1.NamespaceAll)),
			controller.WithRunnable("clusterissuers watcher", controller.Watch(&certmanagerv1.Certificate{}, CertMangerClusterIssuersResource, metav1.NamespaceAll)),
			controller.WithObjectKinds(
				CertManagerCertificateKind,
				CertManagerIssuerKind,
				CertManagerClusterIssuerKind,
			),
			// TODO: add object links
		)
		// TODO: add tls policy specific tasks to workflow
	}

	return controller.NewController(controllerOpts...)
}

func buildReconciler(client *dynamic.DynamicClient) controller.ReconcileFunc {
	reconciler := &controller.Workflow{
		Precondition: NewEventLogger().Log,
		Tasks: []controller.ReconcileFunc{
			NewTopologyFileReconciler(client, operatorNamespace).Reconcile,
		},
	}
	return reconciler.Run
}

type TopologyFileReconciler struct {
	Client    *dynamic.DynamicClient
	Namespace string
}

func NewTopologyFileReconciler(client *dynamic.DynamicClient, namespace string) *TopologyFileReconciler {
	if namespace == "" {
		panic("namespace must be specified and can not be a blank string")
	}
	return &TopologyFileReconciler{Client: client, Namespace: namespace}
}

func (r *TopologyFileReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("topology file")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "topology",
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

	existingTopologyConfigMaps := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == cm.GetName() && object.GetNamespace() == cm.GetNamespace() && object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	if len(existingTopologyConfigMaps) == 0 {
		_, err := r.Client.Resource(controller.ConfigMapsResource).Namespace(cm.Namespace).Create(ctx, unstructuredCM, metav1.CreateOptions{})
		if err != nil {
			logger.Error(err, "failed to write topology configmap")
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

type EventLogger struct{}

func NewEventLogger() *EventLogger {
	return &EventLogger{}
}

func (e *EventLogger) Log(ctx context.Context, resourceEvents []controller.ResourceEvent, _ *machinery.Topology, err error, _ *sync.Map) error {
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

	return nil
}
