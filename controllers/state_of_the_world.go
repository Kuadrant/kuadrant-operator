package controllers

import (
	"fmt"
	"reflect"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	consolev1 "github.com/openshift/api/console/v1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclientgosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrlruntimepredicate "sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	"github.com/kuadrant/kuadrant-operator/pkg/istio"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/openshift"
	"github.com/kuadrant/kuadrant-operator/pkg/openshift/consoleplugin"
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
		controller.WithRunnable("kuadrant watcher", controller.Watch(
			&kuadrantv1beta1.Kuadrant{},
			kuadrantv1beta1.KuadrantsResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1beta1.Kuadrant]{}),
		)),
		controller.WithRunnable("dnspolicy watcher", controller.Watch(
			&kuadrantv1alpha1.DNSPolicy{},
			kuadrantv1alpha1.DNSPoliciesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1alpha1.DNSPolicy]{}),
		)),
		controller.WithRunnable("tlspolicy watcher", controller.Watch(
			&kuadrantv1alpha1.TLSPolicy{},
			kuadrantv1alpha1.TLSPoliciesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1alpha1.TLSPolicy]{}),
		)),
		controller.WithRunnable("authpolicy watcher", controller.Watch(
			&kuadrantv1beta2.AuthPolicy{},
			kuadrantv1beta2.AuthPoliciesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1beta2.AuthPolicy]{}),
		)),
		controller.WithRunnable("ratelimitpolicy watcher", controller.Watch(
			&kuadrantv1beta3.RateLimitPolicy{},
			kuadrantv1beta3.RateLimitPoliciesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1beta3.RateLimitPolicy]{}),
		)),
		controller.WithRunnable("topology configmap watcher", controller.Watch(
			&corev1.ConfigMap{},
			controller.ConfigMapsResource,
			operatorNamespace,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*corev1.ConfigMap]{}),
			controller.FilterResourcesByLabel[*corev1.ConfigMap](fmt.Sprintf("%s=true", kuadrant.TopologyLabel)),
		)),
		controller.WithRunnable("limitador watcher", controller.Watch(
			&limitadorv1alpha1.Limitador{},
			kuadrantv1beta1.LimitadorsResource,
			metav1.NamespaceAll,
		)),
		controller.WithRunnable("authorino watcher", controller.Watch(
			&authorinov1beta1.Authorino{},
			kuadrantv1beta1.AuthorinosResource,
			metav1.NamespaceAll,
		)),
		controller.WithPolicyKinds(
			kuadrantv1alpha1.DNSPolicyGroupKind,
			kuadrantv1alpha1.TLSPolicyGroupKind,
			kuadrantv1beta2.AuthPolicyGroupKind,
			kuadrantv1beta3.RateLimitPolicyGroupKind,
		),
		controller.WithObjectKinds(
			kuadrantv1beta1.KuadrantGroupKind,
			ConfigMapGroupKind,
			kuadrantv1beta1.LimitadorGroupKind,
			kuadrantv1beta1.AuthorinoGroupKind,
		),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses,
			kuadrantv1beta1.LinkKuadrantToLimitador,
			kuadrantv1beta1.LinkKuadrantToAuthorino,
		),
	}

	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(manager.GetRESTMapper())
	if err != nil || !ok {
		logger.Info("gateway api is not installed, skipping watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("gatewayclass watcher", controller.Watch(
				&gwapiv1.GatewayClass{},
				controller.GatewayClassesResource,
				metav1.NamespaceAll,
			)),
			controller.WithRunnable("gateway watcher", controller.Watch(
				&gwapiv1.Gateway{},
				controller.GatewaysResource,
				metav1.NamespaceAll,
			)),
			controller.WithRunnable("httproute watcher", controller.Watch(
				&gwapiv1.HTTPRoute{},
				controller.HTTPRoutesResource,
				metav1.NamespaceAll,
			)),
		)
	}

	isEnvoyGatewayInstalled, err := envoygateway.IsEnvoyGatewayInstalled(manager.GetRESTMapper())
	if err != nil || !isEnvoyGatewayInstalled {
		logger.Info("envoygateway is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("envoypatchpolicy watcher", controller.Watch(
				&egv1alpha1.EnvoyPatchPolicy{},
				envoygateway.EnvoyPatchPoliciesResource,
				metav1.NamespaceAll,
			)),
			controller.WithRunnable("envoyextensionpolicy watcher", controller.Watch(
				&egv1alpha1.EnvoyExtensionPolicy{},
				envoygateway.EnvoyExtensionPoliciesResource,
				metav1.NamespaceAll,
			)),
			controller.WithRunnable("envoysecuritypolicy watcher", controller.Watch(
				&egv1alpha1.SecurityPolicy{},
				envoygateway.SecurityPoliciesResource,
				metav1.NamespaceAll,
			)),
			controller.WithObjectKinds(
				envoygateway.EnvoyPatchPolicyGroupKind,
				envoygateway.EnvoyExtensionPolicyGroupKind,
				envoygateway.SecurityPolicyGroupKind,
			),
			// TODO: add object links
		)
		// TODO: add specific tasks to workflow
	}

	isIstioInstalled, err := istio.IsIstioInstalled(manager.GetRESTMapper())
	if err != nil || !isIstioInstalled {
		logger.Info("istio is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("envoyfilter watcher", controller.Watch(
				&istioclientnetworkingv1alpha3.EnvoyFilter{},
				istio.EnvoyFiltersResource,
				metav1.NamespaceAll,
			)),
			controller.WithRunnable("wasmplugin watcher", controller.Watch(
				&istioclientgoextensionv1alpha1.WasmPlugin{},
				istio.WasmPluginsResource,
				metav1.NamespaceAll,
			)),
			controller.WithRunnable("authorizationpolicy watcher", controller.Watch(
				&istioclientgosecurityv1beta1.AuthorizationPolicy{},
				istio.AuthorizationPoliciesResource,
				metav1.NamespaceAll,
			)),
			controller.WithObjectKinds(
				istio.EnvoyFilterGroupKind,
				istio.WasmPluginGroupKind,
				istio.AuthorizationPolicyGroupKind,
			),
			// TODO: add object links
		)
		// TODO: add istio specific tasks to workflow
	}

	isCertManagerInstalled, err := kuadrantgatewayapi.IsCertManagerInstalled(manager.GetRESTMapper(), logger)
	if err != nil || !isCertManagerInstalled {
		logger.Info("cert manager is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts, certManagerControllerOpts()...)
	}

	isConsolePluginInstalled, err := openshift.IsConsolePluginInstalled(manager.GetRESTMapper())
	if err != nil || !isConsolePluginInstalled {
		logger.Info("console plugin is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("consoleplugin watcher", controller.Watch(
				&consolev1.ConsolePlugin{}, openshift.ConsolePluginsResource, metav1.NamespaceAll,
				controller.FilterResourcesByLabel[*consolev1.ConsolePlugin](fmt.Sprintf("%s=%s", consoleplugin.AppLabelKey, consoleplugin.AppLabelValue)))),
			controller.WithObjectKinds(openshift.ConsolePluginGVK.GroupKind()),
		)
	}

	controllerOpts = append(controllerOpts, controller.WithReconcile(buildReconciler(manager, client, isIstioInstalled, isEnvoyGatewayInstalled, isConsolePluginInstalled)))

	return controller.NewController(controllerOpts...)
}

func certManagerControllerOpts() []controller.ControllerOption {
	isCertificateOwnedByTLSPolicy := func(c *certmanagerv1.Certificate) bool {
		return isObjectOwnedByKind(c, kuadrantv1alpha1.TLSPolicyGroupKind.Group, kuadrantv1alpha1.TLSPolicyGroupKind.Kind)
	}

	return []controller.ControllerOption{
		controller.WithRunnable("certificate watcher", controller.Watch(
			&certmanagerv1.Certificate{},
			CertManagerCertificatesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(ctrlruntimepredicate.TypedFuncs[*certmanagerv1.Certificate]{
				CreateFunc: func(e event.TypedCreateEvent[*certmanagerv1.Certificate]) bool {
					return isCertificateOwnedByTLSPolicy(e.Object)
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*certmanagerv1.Certificate]) bool {
					return isCertificateOwnedByTLSPolicy(e.ObjectNew)
				},
				DeleteFunc: func(e event.TypedDeleteEvent[*certmanagerv1.Certificate]) bool {
					return isCertificateOwnedByTLSPolicy(e.Object)
				},
				GenericFunc: func(e event.TypedGenericEvent[*certmanagerv1.Certificate]) bool {
					return isCertificateOwnedByTLSPolicy(e.Object)
				},
			})),
		),
		controller.WithRunnable("issuers watcher", controller.Watch(
			&certmanagerv1.Issuer{},
			CertManagerIssuersResource,
			metav1.NamespaceAll,
			controller.WithPredicates(ctrlruntimepredicate.TypedFuncs[*certmanagerv1.Issuer]{
				UpdateFunc: func(e event.TypedUpdateEvent[*certmanagerv1.Issuer]) bool {
					oldStatus := e.ObjectOld.GetStatus()
					newStatus := e.ObjectOld.GetStatus()
					return !reflect.DeepEqual(oldStatus, newStatus)
				},
			})),
		),
		controller.WithRunnable("clusterissuers watcher", controller.Watch(
			&certmanagerv1.ClusterIssuer{},
			CertMangerClusterIssuersResource,
			metav1.NamespaceAll,
			controller.WithPredicates(ctrlruntimepredicate.TypedFuncs[*certmanagerv1.ClusterIssuer]{
				UpdateFunc: func(e event.TypedUpdateEvent[*certmanagerv1.ClusterIssuer]) bool {
					oldStatus := e.ObjectOld.GetStatus()
					newStatus := e.ObjectOld.GetStatus()
					return !reflect.DeepEqual(oldStatus, newStatus)
				},
			})),
		),
		controller.WithObjectKinds(
			CertManagerCertificateKind,
			CertManagerIssuerKind,
			CertManagerClusterIssuerKind,
		),
		controller.WithObjectLinks(
			LinkGatewayToCertificateFunc,
			LinkGatewayToIssuerFunc,
			LinkGatewayToClusterIssuerFunc,
		),
	}
}

func buildReconciler(manager ctrlruntime.Manager, client *dynamic.DynamicClient, isIstioInstalled, isEnvoyGatewayInstalled, isConsolePluginInstalled bool) controller.ReconcileFunc {
	mainWorkflow := &controller.Workflow{
		Precondition: initWorkflow(client, manager.GetRESTMapper()).Run,
		Tasks: []controller.ReconcileFunc{
			NewAuthorinoReconciler(client).Subscription().Reconcile,
			NewLimitadorReconciler(client).Subscription().Reconcile,
			NewDNSWorkflow().Run,
			NewTLSWorkflow(client).Run,
			NewAuthWorkflow().Run,
			NewRateLimitWorkflow().Run,
		},
		Postcondition: finalStepsWorkflow(client, isIstioInstalled, isEnvoyGatewayInstalled).Run,
	}

	if isConsolePluginInstalled {
		mainWorkflow.Tasks = append(mainWorkflow.Tasks,
			NewConsolePluginReconciler(manager, operatorNamespace).Subscription().Reconcile,
		)
	}

	return mainWorkflow.Run
}

func initWorkflow(client *dynamic.DynamicClient, restMapper meta.RESTMapper) *controller.Workflow {
	return &controller.Workflow{
		Precondition: NewEventLogger().Log,
		Tasks: []controller.ReconcileFunc{
			NewTopologyReconciler(client, operatorNamespace).Reconcile,
			NewIsCertManagerInstalledTask(restMapper).Reconcile,
		},
	}
}

func finalStepsWorkflow(client *dynamic.DynamicClient, isIstioInstalled, isEnvoyGatewayInstalled bool) *controller.Workflow {
	workflow := &controller.Workflow{
		Tasks: []controller.ReconcileFunc{
			NewGatewayPolicyDiscoverabilityReconciler(client).Subscription().Reconcile,
			NewHTTPRoutePolicyDiscoverabilityReconciler(client).Subscription().Reconcile,
		},
	}

	if isIstioInstalled {
		workflow.Tasks = append(workflow.Tasks, NewIstioExtensionsJanitor(client).Subscription().Reconcile)
	}

	if isEnvoyGatewayInstalled {
		workflow.Tasks = append(workflow.Tasks, NewEnvoyGatewayJanitor(client).Subscription().Reconcile)
	}

	return workflow
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

func isObjectOwnedByKind(o client.Object, ownerGroup, ownerKind string) bool {
	for _, o := range o.GetOwnerReferences() {
		oGV, err := schema.ParseGroupVersion(o.APIVersion)
		if err != nil {
			return false
		}

		if oGV.Group == ownerGroup && o.Kind == ownerKind {
			return true
		}
	}

	return false
}
