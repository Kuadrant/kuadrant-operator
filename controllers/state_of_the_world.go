package controllers

import (
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
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
	operatorNamespace  = env.GetString("OPERATOR_NAMESPACE", "kuadrant-system")
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
		controller.WithRunnable("limitador watcher", controller.Watch(&limitadorv1alpha1.Limitador{}, kuadrantv1beta1.LimitadorResource, metav1.NamespaceAll)),
		controller.WithRunnable("authorino watcher", controller.Watch(&authorinov1beta1.Authorino{}, kuadrantv1beta1.AuthorinoResource, metav1.NamespaceAll)),
		controller.WithPolicyKinds(
			kuadrantv1alpha1.DNSPolicyKind,
			kuadrantv1alpha1.TLSPolicyKind,
			kuadrantv1beta2.AuthPolicyKind,
			kuadrantv1beta2.RateLimitPolicyKind,
		),
		controller.WithObjectKinds(
			kuadrantv1beta1.KuadrantKind,
			ConfigMapGroupKind,
			kuadrantv1beta1.LimitadorKind,
			kuadrantv1beta1.AuthorinoKind,
		),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses,
			kuadrantv1beta1.LinkKuadrantToLimitador,
			kuadrantv1beta1.LinkKuadrantToAuthorino,
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
			controller.WithObjectKinds(
				istio.EnvoyFilterGroupKind,
				istio.WasmPluginGroupKind,
				istio.AuthorizationPolicyGroupKind,
			),
			// TODO: add object links
		)
		// TODO: add istio specific tasks to workflow
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
	mainWorkflow := &controller.Workflow{
		Precondition: preConditionWorkflow(client).Run,
		Tasks: []controller.ReconcileFunc{
			NewAuthorinoCrReconciler(client).Subscription().Reconcile,
			NewDNSWorkflow().Run,
			NewTLSWorkflow().Run,
			NewAuthWorkflow().Run,
			NewRateLimitWorkflow().Run,
		},
		Postcondition: postConditionWorkflow().Run,
	}

	return mainWorkflow.Run
}

func preConditionWorkflow(client *dynamic.DynamicClient) controller.Workflow {
	return controller.Workflow{
		Precondition: NewEventLogger().Log,
		Tasks: []controller.ReconcileFunc{
			NewTopologyFileReconciler(client, operatorNamespace).Reconcile,
		},
	}
}

func postConditionWorkflow() controller.Workflow {
	return controller.Workflow{}
}

// GetOldestKuadrant returns the oldest kuadrant resource from a list of kuadrant resources that is not marked for deletion.
func GetOldestKuadrant(kuadrants []*kuadrantv1beta1.Kuadrant) (*kuadrantv1beta1.Kuadrant, error) {
	if len(kuadrants) == 1 {
		return kuadrants[0], nil
	}
	if len(kuadrants) == 0 {
		return nil, fmt.Errorf("empty list passed")
	}
	oldest := kuadrants[0]
	for _, k := range kuadrants[1:] {
		if k == nil || k.DeletionTimestamp != nil {
			continue
		}
		if oldest == nil {
			oldest = k
			continue
		}
		if k.CreationTimestamp.Before(&oldest.CreationTimestamp) {
			oldest = k
		}
	}
	if oldest == nil {
		return nil, fmt.Errorf("only nil pointers in list")
	}
	return oldest, nil
}
