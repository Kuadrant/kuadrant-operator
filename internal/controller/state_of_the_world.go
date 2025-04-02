package controllers

import (
	"fmt"
	"reflect"
	"sort"

	istiosecurity "istio.io/client-go/pkg/apis/security/v1"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	consolev1 "github.com/openshift/api/console/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/samber/lo"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrlruntimepredicate "sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/authorino"
	"github.com/kuadrant/kuadrant-operator/internal/envoygateway"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/internal/istio"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/observability"
	"github.com/kuadrant/kuadrant-operator/internal/openshift"
	"github.com/kuadrant/kuadrant-operator/internal/openshift/consoleplugin"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

var (
	operatorNamespace       = env.GetString("OPERATOR_NAMESPACE", "kuadrant-system")
	kuadrantManagedLabelKey = "kuadrant.io/managed"

	ConfigMapGroupKind = schema.GroupKind{Group: corev1.GroupName, Kind: "ConfigMap"}
)

// gateway-api permissions
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch

// kuadrant permissions
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants/finalizers,verbs=update
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants/status,verbs=get;update;patch

// core, apps, coordination.k8s,io permissions
//+kubebuilder:rbac:groups=core,resources=serviceaccounts;configmaps;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=configmaps;leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=leases,verbs=get;list;watch;create;update;patch;delete

func NewPolicyMachineryController(manager ctrlruntime.Manager, client *dynamic.DynamicClient, logger logr.Logger) *controller.Controller {
	// Base options
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
			&kuadrantv1.DNSPolicy{},
			kuadrantv1.DNSPoliciesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1.DNSPolicy]{}),
		)),
		controller.WithRunnable("tlspolicy watcher", controller.Watch(
			&kuadrantv1.TLSPolicy{},
			kuadrantv1.TLSPoliciesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1.TLSPolicy]{}),
		)),
		controller.WithRunnable("authpolicy watcher", controller.Watch(
			&kuadrantv1.AuthPolicy{},
			kuadrantv1.AuthPoliciesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1.AuthPolicy]{}),
		)),
		controller.WithRunnable("ratelimitpolicy watcher", controller.Watch(
			&kuadrantv1.RateLimitPolicy{},
			kuadrantv1.RateLimitPoliciesResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*kuadrantv1.RateLimitPolicy]{}),
		)),
		controller.WithRunnable("topology configmap watcher", controller.Watch(
			&corev1.ConfigMap{},
			controller.ConfigMapsResource,
			operatorNamespace,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*corev1.ConfigMap]{}),
			controller.FilterResourcesByLabel[*corev1.ConfigMap](fmt.Sprintf("%s=true", kuadrant.TopologyLabel)),
		)),
		controller.WithRunnable("limitador deployment watcher", controller.Watch(
			&appsv1.Deployment{},
			kuadrantv1beta1.DeploymentsResource,
			metav1.NamespaceAll,
			controller.WithPredicates(&ctrlruntimepredicate.TypedGenerationChangedPredicate[*appsv1.Deployment]{}),
			// the key of the label ("limitador-resource") is hardcoded. This deployment is owned by the limitador operator.
			// labels propagation pattern would be more reliable as the kuadrant operator would be owning these labels
			controller.FilterResourcesByLabel[*appsv1.Deployment](fmt.Sprintf("limitador-resource=%s", kuadrant.LimitadorName)),
			// the key and value of the label are hardcoded. This deployment is owned by the limitador operator.
			// labels propagation pattern would be more reliable as the kuadrant operator would be owning these labels
			controller.FilterResourcesByLabel[*appsv1.Deployment]("app=limitador"),
		)),
		controller.WithPolicyKinds(
			kuadrantv1.DNSPolicyGroupKind,
			kuadrantv1.TLSPolicyGroupKind,
			kuadrantv1.AuthPolicyGroupKind,
			kuadrantv1.RateLimitPolicyGroupKind,
		),
		controller.WithObjectKinds(
			kuadrantv1beta1.KuadrantGroupKind,
			ConfigMapGroupKind,
			kuadrantv1beta1.DeploymentGroupKind,
		),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses,
		),
	}

	// Boot options and reconciler based on detected dependencies
	bootOptions := NewBootOptionsBuilder(manager, client, logger)
	controllerOpts = append(controllerOpts, bootOptions.getOptions()...)
	controllerOpts = append(controllerOpts, controller.WithReconcile(bootOptions.Reconciler()))

	return controller.NewController(controllerOpts...)
}

// NewBootOptionsBuilder is used to return a list of controller.ControllerOption and a controller.ReconcileFunc that depend
// on if external dependent CRDs are installed at boot time
func NewBootOptionsBuilder(manager ctrlruntime.Manager, client *dynamic.DynamicClient, logger logr.Logger) *BootOptionsBuilder {
	return &BootOptionsBuilder{
		manager: manager,
		client:  client,
		logger:  logger,
	}
}

type BootOptionsBuilder struct {
	logger  logr.Logger
	manager ctrlruntime.Manager
	client  *dynamic.DynamicClient

	// Internal configurations
	isGatewayAPIInstalled         bool
	isEnvoyGatewayInstalled       bool
	isIstioInstalled              bool
	isCertManagerInstalled        bool
	isConsolePluginInstalled      bool
	isDNSOperatorInstalled        bool
	isLimitadorOperatorInstalled  bool
	isAuthorinoOperatorInstalled  bool
	isPrometheusOperatorInstalled bool
}

func (b *BootOptionsBuilder) getOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	opts = append(opts, b.getGatewayAPIOptions()...)
	opts = append(opts, b.getIstioOptions()...)
	opts = append(opts, b.getEnvoyGatewayOptions()...)
	opts = append(opts, b.getCertManagerOptions()...)
	opts = append(opts, b.getConsolePluginOptions()...)
	opts = append(opts, b.getDNSOperatorOptions()...)
	opts = append(opts, b.getLimitadorOperatorOptions()...)
	opts = append(opts, b.getAuthorinoOperatorOptions()...)
	opts = append(opts, b.getObservabilityOptions()...)

	return opts
}

func (b *BootOptionsBuilder) getGatewayAPIOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error
	b.isGatewayAPIInstalled, err = kuadrantgatewayapi.IsGatewayAPIInstalled(b.manager.GetRESTMapper())
	if err != nil || !b.isGatewayAPIInstalled {
		b.logger.Info("gateway api is not installed, skipping watches and reconcilers", "err", err)
		return opts
	}

	opts = append(opts,
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

	return opts
}

func (b *BootOptionsBuilder) getEnvoyGatewayOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error
	b.isEnvoyGatewayInstalled, err = envoygateway.IsEnvoyGatewayInstalled(b.manager.GetRESTMapper())
	if err != nil || !b.isEnvoyGatewayInstalled {
		b.logger.Info("envoygateway is not installed, skipping related watches and reconcilers", "err", err)
		return opts
	}

	opts = append(opts,
		controller.WithRunnable("envoypatchpolicy watcher", controller.Watch(
			&egv1alpha1.EnvoyPatchPolicy{},
			envoygateway.EnvoyPatchPoliciesResource,
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*egv1alpha1.EnvoyPatchPolicy](fmt.Sprintf("%s=true", kuadrantManagedLabelKey)),
		)),
		controller.WithRunnable("envoyextensionpolicy watcher", controller.Watch(
			&egv1alpha1.EnvoyExtensionPolicy{},
			envoygateway.EnvoyExtensionPoliciesResource,
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*egv1alpha1.EnvoyExtensionPolicy](fmt.Sprintf("%s=true", kuadrantManagedLabelKey)),
		)),
		controller.WithObjectKinds(
			envoygateway.EnvoyPatchPolicyGroupKind,
			envoygateway.EnvoyExtensionPolicyGroupKind,
		),
		controller.WithObjectLinks(
			envoygateway.LinkGatewayToEnvoyPatchPolicy,
			envoygateway.LinkGatewayToEnvoyExtensionPolicy,
		),
	)

	return opts
}

func (b *BootOptionsBuilder) getIstioOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error
	b.isIstioInstalled, err = istio.IsIstioInstalled(b.manager.GetRESTMapper())
	if err != nil || !b.isIstioInstalled {
		b.logger.Info("istio is not installed, skipping related watches and reconcilers", "err", err)
		return opts
	}

	opts = append(opts,
		controller.WithRunnable("envoyfilter watcher", controller.Watch(
			&istioclientnetworkingv1alpha3.EnvoyFilter{},
			istio.EnvoyFiltersResource,
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*istioclientnetworkingv1alpha3.EnvoyFilter](fmt.Sprintf("%s=true", kuadrantManagedLabelKey)),
		)),
		controller.WithRunnable("peerauthentication watcher", controller.Watch(
			&istiosecurity.PeerAuthentication{},
			istio.PeerAuthenticationResource,
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*istiosecurity.PeerAuthentication](fmt.Sprintf("%s=true", kuadrantManagedLabelKey)),
		)),
		controller.WithRunnable("wasmplugin watcher", controller.Watch(
			&istioclientgoextensionv1alpha1.WasmPlugin{},
			istio.WasmPluginsResource,
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*istioclientgoextensionv1alpha1.WasmPlugin](fmt.Sprintf("%s=true", kuadrantManagedLabelKey)),
		)),
		controller.WithObjectKinds(
			istio.EnvoyFilterGroupKind,
			istio.WasmPluginGroupKind,
			istio.PeerAuthenticationGroupKind,
		),
		controller.WithObjectLinks(
			istio.LinkGatewayToEnvoyFilter,
			istio.LinkGatewayToWasmPlugin,
			//istio.LinkPeerAuthenticationToGateway,
		),
	)

	return opts
}

func (b *BootOptionsBuilder) getCertManagerOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error
	b.isCertManagerInstalled, err = kuadrantgatewayapi.IsCertManagerInstalled(b.manager.GetRESTMapper(), b.logger)
	if err != nil || !b.isCertManagerInstalled {
		b.logger.Info("cert manager is not installed, skipping related watches and reconcilers", "err", err)
		return opts
	}

	opts = append(opts, certManagerControllerOpts()...)

	return opts
}

func (b *BootOptionsBuilder) getConsolePluginOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error
	b.isConsolePluginInstalled, err = openshift.IsConsolePluginInstalled(b.manager.GetRESTMapper())
	if err != nil || !b.isConsolePluginInstalled {
		b.logger.Info("console plugin is not installed, skipping related watches and reconcilers", "err", err)
		return opts
	}

	opts = append(opts,
		controller.WithRunnable("consoleplugin watcher", controller.Watch(
			&consolev1.ConsolePlugin{}, openshift.ConsolePluginsResource, metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*consolev1.ConsolePlugin](fmt.Sprintf("%s=%s", consoleplugin.AppLabelKey, consoleplugin.AppLabelValue)))),
		controller.WithObjectKinds(openshift.ConsolePluginGVK.GroupKind()),
	)

	return opts
}

func (b *BootOptionsBuilder) getDNSOperatorOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error
	b.isDNSOperatorInstalled, err = utils.IsCRDInstalled(b.manager.GetRESTMapper(), DNSRecordGroupKind.Group, DNSRecordGroupKind.Kind, kuadrantdnsv1alpha1.GroupVersion.Version)
	if err != nil || !b.isDNSOperatorInstalled {
		b.logger.Info("dns operator is not installed, skipping related watches and reconcilers", "err", err)
		return opts
	}

	opts = append(opts,
		controller.WithRunnable("dnsrecord watcher", controller.Watch(
			&kuadrantdnsv1alpha1.DNSRecord{}, DNSRecordResource, metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*kuadrantdnsv1alpha1.DNSRecord](fmt.Sprintf("%s=%s", AppLabelKey, AppLabelValue)))),
		controller.WithObjectKinds(
			DNSRecordGroupKind,
		),
		controller.WithObjectLinks(
			LinkListenerToDNSRecord,
			LinkDNSPolicyToDNSRecord,
		),
	)

	return opts
}

func (b *BootOptionsBuilder) getLimitadorOperatorOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error
	b.isLimitadorOperatorInstalled, err = utils.IsCRDInstalled(b.manager.GetRESTMapper(), kuadrantv1beta1.LimitadorGroupKind.Group, kuadrantv1beta1.LimitadorGroupKind.Kind, limitadorv1alpha1.GroupVersion.Version)
	if err != nil || !b.isLimitadorOperatorInstalled {
		b.logger.Info("limitador operator is not installed, skipping related watches and reconcilers", "err", err)
		return opts
	}

	opts = append(opts,
		controller.WithRunnable("limitador watcher", controller.Watch(
			&limitadorv1alpha1.Limitador{},
			kuadrantv1beta1.LimitadorsResource,
			metav1.NamespaceAll,
		)),
		controller.WithObjectKinds(
			kuadrantv1beta1.LimitadorGroupKind,
		),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToLimitador,
			kuadrantv1beta1.LinkLimitadorToDeployment,
		),
	)

	return opts
}

func (b *BootOptionsBuilder) getAuthorinoOperatorOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error
	b.isAuthorinoOperatorInstalled, err = authorino.IsAuthorinoOperatorInstalled(b.manager.GetRESTMapper(), b.logger)
	if err != nil || !b.isAuthorinoOperatorInstalled {
		b.logger.Info("authorino operator is not installed, skipping related watches and reconcilers", "err", err)
		return opts
	}

	opts = append(opts,
		controller.WithRunnable("authorino watcher", controller.Watch(
			&authorinooperatorv1beta1.Authorino{},
			kuadrantv1beta1.AuthorinosResource,
			metav1.NamespaceAll,
		)),
		controller.WithRunnable("authconfig watcher", controller.Watch(
			&authorinov1beta3.AuthConfig{},
			authorino.AuthConfigsResource,
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*authorinov1beta3.AuthConfig](fmt.Sprintf("%s=true", kuadrantManagedLabelKey)),
		)),
		controller.WithObjectKinds(
			kuadrantv1beta1.AuthorinoGroupKind,
			authorino.AuthConfigGroupKind,
		),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToAuthorino,
			authorino.LinkHTTPRouteRuleToAuthConfig,
		),
	)

	return opts
}

func (b *BootOptionsBuilder) getObservabilityOptions() []controller.ControllerOption {
	var opts []controller.ControllerOption
	var err error

	b.isPrometheusOperatorInstalled, err = utils.IsCRDInstalled(b.manager.GetRESTMapper(), monitoringv1.SchemeGroupVersion.Group, monitoringv1.ServiceMonitorsKind, monitoringv1.SchemeGroupVersion.Version)
	if err != nil || !b.isPrometheusOperatorInstalled {
		b.logger.Info("prometheus operator is not installed (ServiceMonitor CRD not found), skipping related watches and reconcilers", "err", err)
		return opts
	}
	b.isPrometheusOperatorInstalled, err = utils.IsCRDInstalled(b.manager.GetRESTMapper(), monitoringv1.SchemeGroupVersion.Group, monitoringv1.PodMonitorsKind, monitoringv1.SchemeGroupVersion.Version)
	if err != nil || !b.isPrometheusOperatorInstalled {
		b.logger.Info("prometheus operator is not installed (PodMonitor CRD not found), skipping related watches and reconcilers", "err", err)
		return opts
	}
	opts = append(opts,
		controller.WithRunnable("servicemonitor watcher", controller.Watch(
			&monitoringv1.ServiceMonitor{},
			monitoringv1.SchemeGroupVersion.WithResource("servicemonitors"),
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*monitoringv1.ServiceMonitor](kuadrant.ObservabilityLabel),
		)),
		controller.WithRunnable("podmonitor watcher", controller.Watch(
			&monitoringv1.PodMonitor{},
			monitoringv1.SchemeGroupVersion.WithResource("podmonitors"),
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*monitoringv1.PodMonitor](kuadrant.ObservabilityLabel),
		)),
		controller.WithObjectKinds(
			observability.ServiceMonitorGroupKind,
			observability.PodMonitorGroupKind,
		),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToServiceMonitor,
			kuadrantv1beta1.LinkKuadrantToPodMonitor,
		),
	)

	return opts
}

func (b *BootOptionsBuilder) isGatewayProviderInstalled() bool {
	return b.isIstioInstalled || b.isEnvoyGatewayInstalled
}

func (b *BootOptionsBuilder) Reconciler() controller.ReconcileFunc {
	mainWorkflow := &controller.Workflow{
		Precondition: initWorkflow(b.client).Run,
		Tasks: []controller.ReconcileFunc{
			NewDNSWorkflow(b.client, b.manager.GetScheme(), b.isGatewayAPIInstalled, b.isDNSOperatorInstalled).Run,
			NewTLSWorkflow(b.client, b.manager.GetScheme(), b.isGatewayAPIInstalled, b.isCertManagerInstalled).Run,
			NewDataPlanePoliciesWorkflow(b.client, b.isGatewayAPIInstalled, b.isIstioInstalled, b.isEnvoyGatewayInstalled, b.isLimitadorOperatorInstalled, b.isAuthorinoOperatorInstalled).Run,
			NewKuadrantStatusUpdater(b.client, b.isGatewayAPIInstalled, b.isGatewayProviderInstalled(), b.isLimitadorOperatorInstalled, b.isAuthorinoOperatorInstalled).Subscription().Reconcile,
			NewObservabilityReconciler(b.client, b.manager, operatorNamespace).Subscription().Reconcile,
		},
		Postcondition: finalStepsWorkflow(b.client, b.isGatewayAPIInstalled, b.isIstioInstalled, b.isEnvoyGatewayInstalled).Run,
	}

	if b.isConsolePluginInstalled {
		mainWorkflow.Tasks = append(mainWorkflow.Tasks,
			NewConsolePluginReconciler(b.manager, operatorNamespace).Subscription().Reconcile,
		)
	}

	if b.isLimitadorOperatorInstalled {
		mainWorkflow.Tasks = append(mainWorkflow.Tasks,
			NewLimitadorReconciler(b.client).Subscription().Reconcile,
		)
	}

	if b.isAuthorinoOperatorInstalled {
		mainWorkflow.Tasks = append(mainWorkflow.Tasks,
			NewAuthorinoReconciler(b.client).Subscription().Reconcile)
	}

	if b.isIstioInstalled && b.isAuthorinoOperatorInstalled && b.isLimitadorOperatorInstalled {
		mainWorkflow.Tasks = append(mainWorkflow.Tasks,
			NewPeerAuthenticationReconciler(b.manager, b.client).Subscription().Reconcile,
			NewLimitadorIstioSidecarReconciler(b.manager, b.client).Subscription().Reconcile,
			NewAuthorinoIstioSidecarReconciler(b.manager, b.client).Subscription().Reconcile,
		)
	}
	return mainWorkflow.Run
}

func certManagerControllerOpts() []controller.ControllerOption {
	return []controller.ControllerOption{
		controller.WithRunnable("certificate watcher", controller.Watch(
			&certmanagerv1.Certificate{},
			CertManagerCertificatesResource,
			metav1.NamespaceAll,
			controller.FilterResourcesByLabel[*certmanagerv1.Certificate](fmt.Sprintf("%s=%s", AppLabelKey, AppLabelValue))),
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
			LinkListenerToCertificateFunc,
			LinkTLSPolicyToIssuerFunc,
			LinkTLSPolicyToClusterIssuerFunc,
		),
	}
}

func initWorkflow(client *dynamic.DynamicClient) *controller.Workflow {
	return &controller.Workflow{
		Precondition: NewEventLogger().Log,
		Tasks: []controller.ReconcileFunc{
			NewTopologyReconciler(client, operatorNamespace).Reconcile,
		},
	}
}

func finalStepsWorkflow(client *dynamic.DynamicClient, isGatewayAPIInstalled, isIstioInstalled, isEnvoyGatewayInstalled bool) *controller.Workflow {
	workflow := &controller.Workflow{
		Tasks: []controller.ReconcileFunc{},
	}

	if isGatewayAPIInstalled {
		workflow.Tasks = append(workflow.Tasks,
			NewGatewayPolicyDiscoverabilityReconciler(client).Subscription().Reconcile,
			NewHTTPRoutePolicyDiscoverabilityReconciler(client).Subscription().Reconcile,
		)
	}

	if isIstioInstalled {
		workflow.Tasks = append(workflow.Tasks, NewIstioExtensionsJanitor(client).Subscription().Reconcile)
	}

	if isEnvoyGatewayInstalled {
		workflow.Tasks = append(workflow.Tasks, NewEnvoyGatewayJanitor(client).Subscription().Reconcile)
	}

	return workflow
}

func GetKuadrantFromTopology(topology *machinery.Topology) *kuadrantv1beta1.Kuadrant {
	kuadrants := lo.FilterMap(topology.Objects().Roots(), func(root machinery.Object, _ int) (controller.Object, bool) {
		o, isSortable := root.(controller.Object)
		return o, isSortable && root.GroupVersionKind().GroupKind() == kuadrantv1beta1.KuadrantGroupKind && o.GetDeletionTimestamp() == nil
	})
	if len(kuadrants) == 0 {
		return nil
	}
	sort.Sort(controller.ObjectsByCreationTimestamp(kuadrants))
	kuadrant, _ := kuadrants[0].(*kuadrantv1beta1.Kuadrant)
	return kuadrant
}

func KuadrantManagedObjectLabels() labels.Set {
	return map[string]string{
		kuadrantManagedLabelKey: "true",
	}
}
