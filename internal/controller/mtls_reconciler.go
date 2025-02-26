package controllers

import (
	"context"
	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/structpb"
	v1alpha32 "istio.io/api/networking/v1alpha3"
	securityv1beta1 "istio.io/api/security/v1"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurity "istio.io/client-go/pkg/apis/security/v1"
	v12 "k8s.io/api/apps/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"strings"
	"sync"
)

type PeerAuthentication istiosecurity.PeerAuthentication

func (pa *PeerAuthentication) GetLocator() string {
	return machinery.LocatorFromObject(pa)
}

type MTLSReconciler struct {
	*reconcilers.BaseReconciler

	Client *dynamic.DynamicClient
}

func NewMTLSReconciler(mgr controllerruntime.Manager, client *dynamic.DynamicClient) *MTLSReconciler {
	return &MTLSReconciler{
		Client: client,
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
			log.Log.WithName("mtls"),
		),
	}
}

func (r *MTLSReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{ReconcileFunc: r.Run, Events: []controller.ResourceEventMatcher{
		{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind)},
		{Kind: ptr.To(kuadrantv1.RateLimitPolicyGroupKind)},
		{Kind: ptr.To(kuadrantv1.AuthPolicyGroupKind)},
		{Kind: ptr.To(machinery.HTTPRouteGroupKind)},
		{Kind: ptr.To(machinery.GatewayGroupKind)},
		{Kind: ptr.To(istio.PeerAuthenticationGroupKind)},
	},
	}
}

//+kubebuilder:rbac:groups=security.istio.io,resources=peerauthentications,verbs=get;list;watch;create;update;patch;delete

func (r *MTLSReconciler) Run(eventCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {

	logger := controller.LoggerFromContext(eventCtx).WithName("MTLSReconciler")
	logger.V(1).Info("reconciling mtls", "status", "started")
	defer logger.V(1).Info("reconciling mtls", "status", "completed")

	// Check that a kuadrant resource exists, and mtls enabled,
	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil || !kObj.Spec.MTLS.Enable {
		defer logger.V(1).Info("mtls not enabled, finishing", "status", "completed")
		return nil
	}

	// only gateway associated with gatewayclass of type istio
	// path may be able to take list of gateways and list rules

	// the path is from a specific gateway to HttpRouteRule
	targetables := topology.Targetables()
	gateways := targetables.Items(func(o machinery.Object) bool {
		gateway, ok := o.(*machinery.Gateway)
		return ok && gateway.Spec.GatewayClassName == "istio"
	})
	httpRouteRules := targetables.Items(func(o machinery.Object) bool {
		_, ok := o.(*machinery.HTTPRouteRule)
		return ok
	})
	anyEffectivePolicy := false
	policies := make([]machinery.Policy, 0)
outer:
	for _, gateway := range gateways {
		for _, httpRouteRule := range httpRouteRules {
			paths := targetables.Paths(gateway, httpRouteRule)
			for _, path := range paths {
				policies = kuadrantv1.PoliciesInPath(path, func(policy machinery.Policy) bool {
					if _, ok := policy.(*kuadrantv1.AuthPolicy); ok {
						return true
					}
					if _, ok := policy.(*kuadrantv1.RateLimitPolicy); ok {
						return true
					}
					return false
				})
				if len(policies) > 0 {
					anyEffectivePolicy = true
					break outer
				}
			}
		}

	}
	if anyEffectivePolicy == false {
		logger.Info("no effective policy found")
		return nil
	}

	// find an authorino object, then find and update the associated deployment
	aobjs := lo.FilterMap(topology.Objects().Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().Kind == kuadrantv1beta1.AuthorinoGroupKind.Kind {
			return item, true
		}
		return nil, false
	})
	// add label to authorino deployment {"sidecar.istio.io/inject":"true"}}}}}
	aDeployment := &v12.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			// TODO can't be hardcoded, this is just one example
			Name:      "authorino",
			Namespace: aobjs[0].GetNamespace(),
		},
	}
	aDeploymentMutators := make([]reconcilers.DeploymentMutateFn, 0)
	aDeploymentMutators = append(aDeploymentMutators, reconcilers.DeploymentTemplateLabelIstioInjectMutator)
	err := r.ReconcileResource(eventCtx, &v12.Deployment{}, aDeployment, reconcilers.DeploymentMutator(aDeploymentMutators...))

	// find a limitador object, then find and update the associated deployment
	lobjs := lo.FilterMap(topology.Objects().Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().Kind == kuadrantv1beta1.LimitadorGroupKind.Kind {
			return item, true
		}
		return nil, false
	})
	// add label to limitador deployment {"sidecar.istio.io/inject":"true"}}}}}
	lDeployment := &v12.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			// TODO can't be hardcoded, this is just one example
			Name:      "limitador-limitador",
			Namespace: lobjs[0].GetNamespace(),
		},
	}
	lDeploymentMutators := make([]reconcilers.DeploymentMutateFn, 0)
	lDeploymentMutators = append(lDeploymentMutators, reconcilers.DeploymentTemplateLabelIstioInjectMutator)
	err = r.ReconcileResource(eventCtx, &v12.Deployment{}, lDeployment, reconcilers.DeploymentMutator(lDeploymentMutators...))

	valueMap := map[string]interface{}{
		"transport_socket": map[string]interface{}{
			"name": "envoy.transport_sockets.tls",
			"typed_config": map[string]interface{}{
				"@type": "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext",
				"common_tls_context": map[string]interface{}{
					"tls_certificate_sds_secret_configs": []map[string]interface{}{
						{
							"name": "default",
							"sds_config": map[string]interface{}{
								"api_config_source": map[string]interface{}{
									"api_type": "GRPC",
									"grpc_services": []map[string]interface{}{
										{
											"envoy_grpc": map[string]interface{}{
												"cluster_name": "sds-grpc",
											},
										},
									},
								},
							},
						},
					},
					"validation_context_sds_secret_config": map[string]interface{}{
						"name": "ROOTCA",
						"sds_config": map[string]interface{}{
							"api_config_source": map[string]interface{}{
								"api_type": "GRPC",
								"grpc_services": []map[string]interface{}{
									{
										"envoy_grpc": map[string]interface{}{
											"cluster_name": "sds-grpc",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	valueStruct, err := structpb.NewStruct(valueMap)
	if err != nil {
		logger.Info("problem processing patch for Envoy Filter")
		return err
	}
	patch := &v1alpha32.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: v1alpha32.EnvoyFilter_CLUSTER,
		Patch: &v1alpha32.EnvoyFilter_Patch{
			Operation: v1alpha32.EnvoyFilter_Patch_MERGE,
			Value:     valueStruct,
		},
	}
	// add the patch to each EnvoyFilter with prefix kuadrant- in the same namespace as your gateway.
	for _, gateway := range gateways {

		envoyFilters := lo.FilterMap(topology.Objects().Items(), func(item machinery.Object, _ int) (*v1alpha3.EnvoyFilter, bool) {
			if rObj, isObj := item.(*controller.RuntimeObject); isObj {
				if record, isRec := rObj.Object.(*v1alpha3.EnvoyFilter); isRec {
					if strings.HasPrefix(record.Name, "kuadrant-") && gateway.GetNamespace() == record.Namespace {
						return record, true
					}
				}
			}
			return nil, false
		})

		for _, envoyFilter := range envoyFilters {
			envoyFilter.Spec.ConfigPatches = append(envoyFilter.Spec.ConfigPatches, patch)
			unstructuredEnvoyFilter, err := controller.Destruct(envoyFilter)
			if err != nil {
				logger.Error(err, "failed to destruct limitador", "status", "error")
				return err
			}
			logger.Info("creating limitador resource", "status", "processing")
			_, err = r.Client.Resource(istio.EnvoyFiltersResource).Namespace(envoyFilter.Namespace).Update(eventCtx, unstructuredEnvoyFilter, metav1.UpdateOptions{})
			if err != nil && !apiErrors.IsAlreadyExists(err) {
				logger.Error(err, "failed to update envoyfilter resource", "status", "error")
				return err
			}
		}

	}

	peerAuth := &istiosecurity.PeerAuthentication{
		ObjectMeta: controllerruntime.ObjectMeta{
			Labels: KuadrantManagedObjectLabels(),
		},
		Spec: securityv1beta1.PeerAuthentication{
			Mtls: &securityv1beta1.PeerAuthentication_MutualTLS{
				Mode: securityv1beta1.PeerAuthentication_MutualTLS_STRICT,
			},
		},
	}

	unstructuredPeerAuth, err := controller.Destruct(peerAuth)
	if err != nil {
		logger.Error(err, "failed to destruct peer authentication", "status", "error")
		return err
	}
	logger.Info("creating peer authentication resource", "status", "processing")
	_, err = r.Client.Resource(istio.PeerAuthenticationResource).Namespace(kObj.Namespace).Create(eventCtx, unstructuredPeerAuth, metav1.CreateOptions{})
	if err != nil {
		if apiErrors.IsAlreadyExists(err) {
			logger.Info("already created peer authentication resource", "status", "acceptable")
		} else {
			logger.Error(err, "failed to create peer authentication resource", "status", "error")
			return err
		}
	}

	return nil
}
