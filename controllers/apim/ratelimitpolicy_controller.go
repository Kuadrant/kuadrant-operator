/*
Copyright 2021 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apim

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/gogo/protobuf/types"
	"github.com/kuadrant/limitador-operator/api/v1alpha1"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

const (
	EnvoysHTTPPortNumber            = 8080
	EnvoysHTTPConnectionManagerName = "envoy.filters.network.http_connection_manager"

	VSAnnotationRateLimitPolicy = "kuadrant.io/ratelimitpolicy"

	InvalidNetworkingRefTypeErr = "unknown networking reference type"
)

// RateLimitPolicyReconciler reconciles a RateLimitPolicy object
type RateLimitPolicyReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;delete;update;patch
//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=ratelimits,verbs=get;list;watch;create;update;delete;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RateLimitPolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("RateLimitPolicy", req.NamespacedName)
	logger.Info("Reconciling RateLimitPolicy")
	ctx := logr.NewContext(eventCtx, logger)

	var rlp apimv1alpha1.RateLimitPolicy
	if err := r.Client().Get(ctx, req.NamespacedName, &rlp); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no RateLimitPolicy found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get RateLimitPolicy")
		return ctrl.Result{}, err
	}

	if rlp.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(&rlp, patchesFinalizer) {
		logger.Info("Removing finalizers")
		if err := r.finalizeEnvoyFilters(ctx, &rlp); err != nil {
			logger.Error(err, "failed to remove ownerRlp entry from filters patch")
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(&rlp, patchesFinalizer)
		if err := r.BaseReconciler.UpdateResource(ctx, &rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&rlp, patchesFinalizer) {
		controllerutil.AddFinalizer(&rlp, patchesFinalizer)
		if err := r.BaseReconciler.UpdateResource(ctx, &rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	if err := r.reconcileLimits(ctx, &rlp); err != nil {
		return ctrl.Result{}, err
	}

	for _, networkingRef := range rlp.Spec.NetworkingRef {
		switch networkingRef.Type {
		case apimv1alpha1.NetworkingRefTypeHR:
			logger.Info("HTTPRoute is not implemented yet") // TODO(rahulanand16nov)
			return ctrl.Result{}, nil
		case apimv1alpha1.NetworkingRefTypeVS:
			vsNamespacedName := client.ObjectKey{
				Namespace: rlp.Namespace, // VirtualService lookup is limited to RLP's namespace
				Name:      networkingRef.Name,
			}

			var vs istio.VirtualService
			if err := r.Client().Get(ctx, vsNamespacedName, &vs); err != nil {
				if apierrors.IsNotFound(err) {
					logger.Info("no VirtualService found", "lookup name", vsNamespacedName.String())
					return ctrl.Result{}, nil
				}
				logger.Error(err, "failed to get VirutalService")
				return ctrl.Result{}, err
			}

			if err := r.reconcileWithVirtualService(ctx, &vs, &rlp); err != nil {
				logger.Error(err, "failed to reconcile with VirtualService")
				return ctrl.Result{}, err
			}
		default:
			err := fmt.Errorf(InvalidNetworkingRefTypeErr)
			logger.Error(err, "networking reconciliation failed")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) reconcileWithVirtualService(ctx context.Context, vs *istio.VirtualService, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := r.Logger()
	rlpKey := client.ObjectKeyFromObject(rlp)

	_, ok := vs.Annotations[VSAnnotationRateLimitPolicy]
	if !ok {
		vs.Annotations[VSAnnotationRateLimitPolicy] = rlpKey.String()
		if err := r.Client().Update(ctx, vs); err != nil {
			logger.Error(err, "failed to add RateLimitPolicy annotation to VirtualService")
			return err
		}
		logger.V(1).Info("successfully added RateLimitPolicy annotation to VirtualService")
	}

	// TODO(rahulanand16nov): store context of virtualservice in RLP's status block and manage envoy patches.

	// create/update EnvoyFilter patches for each gateway
	for _, gw := range vs.Spec.Gateways {
		gwKey := common.NamespacedNameToObjectKey(gw, vs.Namespace) // Istio defaults to VirtualService's namespace
		gwLabels := gatewayLabels(ctx, r.Client(), gwKey)
		owner := rlpKey.String()

		// create/update ratelimit filters patch
		// fetch already existing filters patch or create a new one
		filtersPatchKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: rlFiltersPatchName(gwKey.Name)}
		filtersPatch := &istio.EnvoyFilter{}
		if err := r.Client().Get(ctx, filtersPatchKey, filtersPatch); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get ratelimit filters patch")
				return err
			}
			filtersPatch, err = ratelimitFiltersPatch(gwKey, gwLabels)
			if err != nil {
				logger.Error(err, "failed to form ratelimit filters patch")
				return err
			}
		}

		// make sure annotation map is initialized
		filtersPatchOwnerList := []string{}
		if filtersPatch.Annotations == nil {
			filtersPatch.Annotations = make(map[string]string)
		}

		if ogOwnerRlps, present := filtersPatch.Annotations[envoyFilterAnnotationOwnerRLPs]; present {
			filtersPatchOwnerList = strings.Split(ogOwnerRlps, ownerRlpSeparator)
		}

		// add the owner name if not present already
		if !common.Contains(filtersPatchOwnerList, owner) {
			filtersPatchOwnerList = append(filtersPatchOwnerList, owner)
		}
		finalOwnerVal := strings.Join(filtersPatchOwnerList, ownerRlpSeparator)

		filtersPatch.Annotations[envoyFilterAnnotationOwnerRLPs] = finalOwnerVal
		if err := r.ReconcileResource(ctx, &istio.EnvoyFilter{}, filtersPatch, alwaysUpdateEnvoyPatches); err != nil {
			logger.Error(err, "failed to create/update EnvoyFilter that patches-in ratelimit filters")
			return err
		}
		logger.Info("successfully created/updated ratelimit filters patch", "gateway", gwKey.String())

		// create/update ratelimits patch
		ratelimitsPatchKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: ratelimitsPatchName(rlp.Name, gwKey.Name)}
		ratelimitsPatch := &istio.EnvoyFilter{}
		if err := r.Client().Get(ctx, ratelimitsPatchKey, ratelimitsPatch); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get ratelimits patch")
				return err
			}
			ratelimitsPatch, err = envoyRatelimitsPatch(rlp, vs.Spec.Hosts, gwKey, gwLabels)
			if err != nil {
				logger.Error(err, "failed to form ratelimits patch")
				return err
			}
		}

		ratelimitsPatchOwnerList := []string{}
		if ratelimitsPatch.Annotations == nil {
			ratelimitsPatch.Annotations = make(map[string]string)
		}

		if ogOwnerRlps, present := ratelimitsPatch.Annotations[envoyFilterAnnotationOwnerRLPs]; present {
			ratelimitsPatchOwnerList = strings.Split(ogOwnerRlps, ownerRlpSeparator)
		}

		// add the owner name if not present already
		if !common.Contains(ratelimitsPatchOwnerList, owner) {
			ratelimitsPatchOwnerList = append(ratelimitsPatchOwnerList, owner)
		}
		finalOwnerVal = strings.Join(ratelimitsPatchOwnerList, ownerRlpSeparator)

		ratelimitsPatch.Annotations[envoyFilterAnnotationOwnerRLPs] = finalOwnerVal
		if err := r.ReconcileResource(ctx, &istio.EnvoyFilter{}, ratelimitsPatch, alwaysUpdateEnvoyPatches); err != nil {
			logger.Error(err, "failed to create/update EnvoyFilter that patches-in ratelimits")
			return err
		}
		logger.Info("successfully created/updated ratelimits patch", "gateway", gwKey.String())
	}

	logger.Info("successfully reconciled RateLimitPolicy using attached VirtualService")
	return nil
}

// rateLimitInitialPatch returns EnvoyFilter resource that patches-in
// - Add Preauth RateLimit Filter as the first http filter
// - Add PostAuth RateLimit Filter as the last http filter
// - Change cluster name pointing to Limitador in kuadrant-system namespace (temp)
// Note: please make sure this patch is only created once per gateway.
func ratelimitFiltersPatch(gwKey client.ObjectKey, gwLabels map[string]string) (*istio.EnvoyFilter, error) {
	patchUnstructured := map[string]interface{}{
		"operation": "INSERT_FIRST", // preauth should be the first filter in the chain
		"value": map[string]interface{}{
			"name": "envoy.filters.http.preauth.ratelimit",
			"typed_config": map[string]interface{}{
				"@type":             "type.googleapis.com/envoy.extensions.filters.http.ratelimit.v3.RateLimit",
				"domain":            "preauth",
				"stage":             common.PreAuthStage,
				"failure_mode_deny": true,
				// If not specified, returns success immediately (can be useful for us)
				"rate_limit_service": map[string]interface{}{
					"transport_api_version": "V3",
					"grpc_service": map[string]interface{}{
						"timeout": "3s",
						"envoy_grpc": map[string]string{
							"cluster_name": common.PatchedLimitadorClusterName,
						},
					},
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)
	prePatch := networkingv1alpha3.EnvoyFilter_Patch{}
	if err := prePatch.UnmarshalJSON(patchRaw); err != nil {
		return nil, err
	}

	postPatch := prePatch.DeepCopy()
	postPatch.Value.Fields["name"] = &types.Value{
		Kind: &types.Value_StringValue{
			StringValue: "envoy.filters.http.postauth.ratelimit",
		},
	}

	// update domain for postauth filter
	postPatch.Value.Fields["typed_config"].GetStructValue().Fields["domain"] = &types.Value{
		Kind: &types.Value_StringValue{
			StringValue: "postauth",
		},
	}
	// update stage for postauth filter
	postPatch.Value.Fields["typed_config"].GetStructValue().Fields["stage"] = &types.Value{
		Kind: &types.Value_NumberValue{
			NumberValue: float64(common.PostAuthStage),
		},
	}
	// update operation for postauth filter
	postPatch.Operation = networkingv1alpha3.EnvoyFilter_Patch_INSERT_BEFORE

	preAuthFilterPatch := &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: networkingv1alpha3.EnvoyFilter_HTTP_FILTER,
		Match: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: networkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &networkingv1alpha3.EnvoyFilter_ListenerMatch{
					PortNumber: EnvoysHTTPPortNumber, // Kuadrant-gateway listens on this port by default
					FilterChain: &networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: EnvoysHTTPConnectionManagerName,
						},
					},
				},
			},
		},
		Patch: &prePatch,
	}

	postAuthFilterPatch := preAuthFilterPatch.DeepCopy()
	postAuthFilterPatch.Patch = postPatch

	// postauth filter should be injected just before the router filter
	postAuthFilterPatch.Match.ObjectTypes = &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
		Listener: &networkingv1alpha3.EnvoyFilter_ListenerMatch{
			PortNumber: EnvoysHTTPPortNumber,
			FilterChain: &networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
				Filter: &networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
					Name: EnvoysHTTPConnectionManagerName,
					SubFilter: &networkingv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
						Name: "envoy.filters.http.router",
					},
				},
			},
		},
	}

	// Eventually, this should be dropped since it's a temp-fix for Kuadrant/limitador#53
	clusterPatch := common.LimitadorClusterEnvoyPatch()

	factory := common.EnvoyFilterFactory{
		ObjectName: rlFiltersPatchName(gwKey.Name),
		Namespace:  gwKey.Namespace,
		Patches: []*networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
			preAuthFilterPatch,
			postAuthFilterPatch,
			clusterPatch,
		},
		Labels: gwLabels,
	}

	return factory.EnvoyFilter(), nil
}

func envoyRatelimitsPatch(rlp *apimv1alpha1.RateLimitPolicy, vHostNames []string, gwKey client.ObjectKey, gwLabels map[string]string) (*istio.EnvoyFilter, error) {
	ratelimitsPatch := &istio.EnvoyFilter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ratelimitsPatchName(rlp.Name, gwKey.Name),
			Namespace: gwKey.Namespace,
		},
		Spec: networkingv1alpha3.EnvoyFilter{
			WorkloadSelector: &networkingv1alpha3.WorkloadSelector{
				Labels: gwLabels,
			},
			ConfigPatches: []*networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{},
		},
	}

	// route-level patches
	for _, host := range vHostNames {
		for _, route := range rlp.Spec.Routes {
			vHostName := host + ":80" // Istio names virtual host in this format: host + port

			routePatch, err := routeRateLimitsPatch(vHostName, route)
			if err != nil {
				return nil, err
			}
			ratelimitsPatch.Spec.ConfigPatches = append(ratelimitsPatch.Spec.ConfigPatches, routePatch)
		}
	}

	// TODO(rahulanand16nov): add the virtualhost-level patch
	return ratelimitsPatch, nil
}

// routeRateLimitsPatch returns an Envoy patch that add in ratelimits at the route level.
func routeRateLimitsPatch(vHostName string, route apimv1alpha1.Route) (*networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	stageValue := apimv1alpha1.RateLimitStageValue[apimv1alpha1.RateLimitStagePREAUTH]
	if route.Stage == apimv1alpha1.RateLimitStagePOSTAUTH {
		stageValue = apimv1alpha1.RateLimitStageValue[apimv1alpha1.RateLimitStagePOSTAUTH]
	}

	patchUnstructured := map[string]interface{}{
		"operation": "MERGE",
		"value": map[string]interface{}{
			"route": map[string]interface{}{
				"rate_limits": []map[string]interface{}{
					{
						"stage":   stageValue,
						"actions": "ACTIONS", // this is replaced with actual value below
					},
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)
	stringPatch := string(patchRaw)

	jsonActions, _ := json.Marshal(route.Actions)
	// A nice trick to make it easier add-in actions
	stringPatch = strings.Replace(stringPatch, "\"ACTIONS\"", string(jsonActions), 1)
	patchRaw = []byte(stringPatch)

	Patch := &networkingv1alpha3.EnvoyFilter_Patch{}
	if err := Patch.UnmarshalJSON(patchRaw); err != nil {
		return nil, err
	}

	if route.Stage == apimv1alpha1.RateLimitStageBOTH {
		routeCopy := Patch.DeepCopy().Value.Fields["route"]
		updatedRateLimits := routeCopy.GetStructValue().Fields["rate_limits"].GetListValue().Values
		updatedRateLimits[0].GetStructValue().Fields["stage"] = &types.Value{
			Kind: &types.Value_NumberValue{
				NumberValue: float64(stageValue ^ 1), // toggle between 1/0
			},
		}

		finalRoute := Patch.Value.Fields["route"]
		ogRateLimits := finalRoute.GetStructValue().Fields["rate_limits"].GetListValue().Values
		finalRateLimits := append(ogRateLimits, updatedRateLimits[0])
		finalRoute.GetStructValue().Fields["rate_limits"].GetListValue().Values = finalRateLimits
	}

	envoyFilterPatch := &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: networkingv1alpha3.EnvoyFilter_HTTP_ROUTE,
		Match: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: networkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &networkingv1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Vhost: &networkingv1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
						Name: vHostName,
						Route: &networkingv1alpha3.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
							Name: "." + route.Name, // Istio adds '.' infront of names
						},
					},
				},
			},
		},
		Patch: Patch,
	}

	return envoyFilterPatch, nil
}

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := r.Logger()

	// create the RateLimit resource
	for i, rlSpec := range rlp.Spec.Limits {
		ratelimitfactory := common.RateLimitFactory{
			Key: client.ObjectKey{
				Name:      fmt.Sprintf("%s-limit-%d", rlp.Name, i+1),
				Namespace: rlp.Namespace,
			},
			Conditions: rlSpec.Conditions,
			MaxValue:   rlSpec.MaxValue,
			Namespace:  rlSpec.Namespace,
			Variables:  rlSpec.Variables,
			Seconds:    rlSpec.Seconds,
		}

		ratelimit := ratelimitfactory.RateLimit()
		if err := controllerutil.SetOwnerReference(rlp, ratelimit, r.Client().Scheme()); err != nil {
			logger.Error(err, "failed to add owner ref to RateLimit resource")
			return err
		}
		err := r.BaseReconciler.ReconcileResource(ctx, &v1alpha1.RateLimit{}, ratelimit, alwaysUpdateRateLimit)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "ReconcileResource failed to create/update RateLimit resource")
			return err
		}
	}
	logger.Info("successfully created/updated RateLimit resources")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apimv1alpha1.RateLimitPolicy{}).
		Complete(r)
}
