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
	"regexp"

	"github.com/go-logr/logr"
	"github.com/gogo/protobuf/types"
	"github.com/kuadrant/limitador-operator/api/v1alpha1"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-controller/pkg/istio"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

const (
	EnvoysHTTPPortNumber            = 8080
	EnvoysHTTPConnectionManagerName = "envoy.filters.network.http_connection_manager"

	VSAnnotationRateLimitPolicy = "kuadrant.io/ratelimitpolicy"
)

// RateLimitPolicyReconciler reconciles a RateLimitPolicy object
type RateLimitPolicyReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

// SignalingNetwork contains the relevant information about the signaling routing resource.
type SignalingNetwork struct {
	// Routing resource sending the signal
	NetworkName string
	// Routing resource kind (VirtualService/HTTPRoute)
	NetworkKind string
	// Gateways attached to the routing resource
	Gateways []string
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
		logger.V(1).Info("Handling removal of ratelimitpolicy object")
		if err := r.finalizeEnvoyFilters(ctx, &rlp); err != nil {
			logger.Error(err, "failed to remove ownerRlp entry from filters patch")
			return ctrl.Result{}, err
		}
		if err := r.deleteRateLimits(ctx, &rlp); err != nil {
			logger.Error(err, "failed to delete RateLimt objects")
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(&rlp, patchesFinalizer)
		if err := r.UpdateResource(ctx, &rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Ignore deleted resources, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if rlp.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&rlp, patchesFinalizer) {
		controllerutil.AddFinalizer(&rlp, patchesFinalizer)
		if err := r.UpdateResource(ctx, &rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	if err := r.reconcileLimits(ctx, &rlp); err != nil {
		return ctrl.Result{}, err
	}

	// Operation specific annotations must be removed if they are present.
	updateRequired := false
	var delNetwork *SignalingNetwork
	var addNetwork *SignalingNetwork

	// check for delete operation from virtualservice
	if signalingVSName, present := rlp.Annotations[KuadrantDeleteVSAnnotation]; present {
		delNetwork = &SignalingNetwork{
			NetworkName: signalingVSName,
			NetworkKind: common.VirtualServiceKind,
			Gateways:    rlp.Status.GetGateways(common.VirtualServiceKind, signalingVSName),
		}
	}

	// check for delete operation for httproute
	if signalingHRName, present := rlp.Annotations[KuadrantDeleteHRAnnotation]; present {
		delNetwork = &SignalingNetwork{
			NetworkName: signalingHRName,
			NetworkKind: common.HTTPRouteKind,
			Gateways:    rlp.Status.GetGateways(common.HTTPRouteKind, signalingHRName),
		}
	}

	if delNetwork != nil {
		networkObjKey := client.ObjectKey{Namespace: rlp.Namespace, Name: delNetwork.NetworkName}
		if err := r.detachFromNetwork(ctx, delNetwork.Gateways, networkObjKey.String(), &rlp); err != nil {
			logger.Error(err, "failed to detach RateLimitPolicy from routing resource")
			return ctrl.Result{}, err
		}

		if err := r.detachNetworkFromStatus(ctx, delNetwork, &rlp); err != nil {
			return ctrl.Result{}, err
		}

		delete(rlp.Annotations, KuadrantDeleteVSAnnotation)
		delete(rlp.Annotations, KuadrantDeleteHRAnnotation)
		updateRequired = true
	}

	// check for add operation for virtualservice
	if vsName, present := rlp.Annotations[KuadrantAddVSAnnotation]; present {
		vsKey := client.ObjectKey{
			Namespace: rlp.Namespace,
			Name:      vsName,
		}

		var vs istio.VirtualService
		if err := r.Client().Get(ctx, vsKey, &vs); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("no VirtualService found", "virtualservice", vsKey.String())
				return ctrl.Result{}, nil
			}
			logger.Error(err, "failed to get VirutalService")
			return ctrl.Result{}, err
		}

		fetchOperationsFromVS(&vs.Spec, &rlp.Spec)

		addNetwork = &SignalingNetwork{
			Gateways:    vs.Spec.Gateways,
			NetworkName: vsName,
			NetworkKind: common.VirtualServiceKind,
		}
	}

	// check for add operation for httproute
	if hrName, present := rlp.Annotations[KuadrantAddHRAnnotation]; present {
		hrKey := client.ObjectKey{
			Namespace: rlp.Namespace,
			Name:      hrName,
		}

		var hr gatewayapiv1alpha2.HTTPRoute
		if err := r.Client().Get(ctx, hrKey, &hr); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("no HTTPRoute found", "httproute", hrKey.String())
				return ctrl.Result{}, nil
			}
			logger.Error(err, "failed to get HTTPRoute")
			return ctrl.Result{}, err
		}

		// For Kuadrant, gateway obj will always be present in the system namespace
		var gws []string
		for _, parentRef := range hr.Spec.CommonRouteSpec.ParentRefs { //fix
			gws = append(gws, fmt.Sprintf("%s/%s", common.KuadrantNamespace, parentRef.Name))
		}
		addNetwork = &SignalingNetwork{
			Gateways:    gws,
			NetworkName: hrName,
			NetworkKind: common.HTTPRouteKind,
		}
	}

	if addNetwork != nil {
		networkObjKey := client.ObjectKey{Namespace: rlp.Namespace, Name: addNetwork.NetworkName}
		if err := r.attachToNetwork(ctx, addNetwork.Gateways, networkObjKey.String(), &rlp); err != nil {
			logger.Error(err, "failed to attach RateLimitPolicy to routing resource")
			return ctrl.Result{}, err
		}

		if err := r.attachNetworkToStatus(ctx, addNetwork, &rlp); err != nil {
			return ctrl.Result{}, err
		}

		delete(rlp.Annotations, KuadrantAddVSAnnotation)
		delete(rlp.Annotations, KuadrantAddHRAnnotation)
		updateRequired = true
	}

	if updateRequired {
		if err := r.Client().Update(ctx, &rlp); err != nil {
			logger.Error(err, "failed to remove operation specific annotations from RateLimitPolicy")
			return ctrl.Result{}, err
		}
		logger.Info("successfully removed operation specific annotations from RateLimitPolicy")
	}

	// TODO(rahulanand16nov): do the same as above for HTTPRoute
	logger.Info("successfully reconciled RateLimitPolicy")
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) detachFromNetwork(ctx context.Context, gateways []string, owner string, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
	logger.Info("Detaching RateLimitPolicy from a network")

	for _, gw := range gateways {
		gwKey := common.NamespacedNameToObjectKey(gw, rlp.Namespace)

		// fetch the filters patch
		wasmEnvoyFilterKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: rlFiltersPatchName(gwKey.Name)}
		wasmEnvoyFilter := &istio.EnvoyFilter{}
		if err := r.Client().Get(ctx, wasmEnvoyFilterKey, wasmEnvoyFilter); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get ratelimit filters patch")
				return err
			}
			return nil
		}

		// remove the parentRef entry on filters patch
		if err := r.removeParentRefEntry(ctx, wasmEnvoyFilter, owner); err != nil {
			logger.Error(err, "failed to remove parentRef entry on the ratelimit filters patch")
			return err
		}
		logger.Info("successfully deleted/updated ratelimit filters patch")
	}

	logger.Info("successfully detached RateLimitPolicy from specified gateways and hosts")
	return nil
}

func (r *RateLimitPolicyReconciler) attachToNetwork(ctx context.Context, gateways []string, owner string, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
	logger.Info("Attaching RateLimitPolicy to a network")

	for _, gw := range gateways {
		gwKey := common.NamespacedNameToObjectKey(gw, rlp.Namespace)
		gwLabels := gatewayLabels(ctx, r.Client(), gwKey)

		// create/update ratelimit filters patch
		// fetch already existing filters patch or create a new one
		wasmEnvoyFilterKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: rlFiltersPatchName(gwKey.Name)}
		wasmEnvoyFilter := &istio.EnvoyFilter{}
		if err := r.Client().Get(ctx, wasmEnvoyFilterKey, wasmEnvoyFilter); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get ratelimit filters patch")
				return err
			}
		}

		if err := updateEnvoyFilter(ctx, wasmEnvoyFilter, rlp, gwKey, gwLabels); err != nil {
			logger.Error(err, "failed to create/update EnvoyFilter containing wasm filters")
			return err
		}

		if err := r.addParentRefEntry(ctx, wasmEnvoyFilter, owner); err != nil {
			logger.Error(err, "failed to add ownerRLP entry to the ratelimit filters patch")
			return err
		}

		logger.Info("successfully created/updated ratelimit filters patch", "gateway", gwKey.String())
	}
	logger.Info("successfully attached RateLimitPolicy to specified gateways and hosts")
	return nil
}

// updateEnvoyFilter returns an EnvoyFilter resource that patches in order:
// - Pre-Authorization ratelimit wasm filter
// - Post-Authorization ratelimit wasm filter
// - Limitador cluster (tmp-fix)
// - Wasm cluster
func updateEnvoyFilter(ctx context.Context, existingObj *istio.EnvoyFilter, rlp *apimv1alpha1.RateLimitPolicy, gwKey client.ObjectKey, gwLabels map[string]string) error {
	logger := logr.FromContext(ctx)

	rlpKey := client.ObjectKeyFromObject(rlp)

	preAuthPluginPolicy := kuadrantistioutils.PluginPolicyFromRateLimitPolicy(rlp, apimv1alpha1.RateLimitStagePREAUTH)
	postAuthPluginPolicy := kuadrantistioutils.PluginPolicyFromRateLimitPolicy(rlp, apimv1alpha1.RateLimitStagePOSTAUTH)

	finalPatches := []*networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{}

	// first time creating the EnvoyFilter i.e. wasm filters.
	// newly initialised object should have name field as empty string.
	if len(existingObj.Name) == 0 {
		logger.Info("Initialising EnvoyFilter")

		preAuthPluginConfig := kuadrantistioutils.PluginConfig{
			FailureModeDeny: true,
			PluginPolicies: map[string]kuadrantistioutils.PluginPolicy{
				rlpKey.String(): *preAuthPluginPolicy,
			},
		}
		preAuthJSON, err := json.Marshal(preAuthPluginConfig)
		if err != nil {
			return fmt.Errorf("failed to marshall preauth plugin config into json")
		}

		postAuthPluginConfig := kuadrantistioutils.PluginConfig{
			FailureModeDeny: true,
			PluginPolicies: map[string]kuadrantistioutils.PluginPolicy{
				rlpKey.String(): *postAuthPluginPolicy,
			},
		}
		postAuthJSON, err := json.Marshal(postAuthPluginConfig)
		if err != nil {
			return fmt.Errorf("failed to marshall preauth plugin config into json")
		}

		patchUnstructured := map[string]interface{}{
			"operation": "INSERT_FIRST", // preauth should be the first filter in the chain
			"value": map[string]interface{}{
				"name": "envoy.filters.http.preauth.wasm",
				"typed_config": map[string]interface{}{
					"@type":   "type.googleapis.com/udpa.type.v1.TypedStruct",
					"typeUrl": "type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm",
					"value": map[string]interface{}{
						"config": map[string]interface{}{
							"configuration": map[string]interface{}{
								"@type": "type.googleapis.com/google.protobuf.StringValue",
								"value": string(preAuthJSON),
							},
							"name": "preauth-wasm",
							"vm_config": map[string]interface{}{
								"code": map[string]interface{}{
									"remote": map[string]interface{}{
										"http_uri": map[string]interface{}{
											"uri":     "https://raw.githubusercontent.com/rahulanand16nov/wasm-shim/new-api/deploy/wasm_shim.wasm",
											"cluster": kuadrantistioutils.PatchedWasmClusterName,
											"timeout": "10s",
										},
										"sha256": "335a05fbb0fcd4e68856c9c48aac2d8f4d07d7cf3f2f49a8be35c69b384daf9d",
										"retry_policy": map[string]interface{}{
											"num_retries": 10,
										},
									},
								},
								"allow_precompiled": true,
								"runtime":           "envoy.wasm.runtime.v8",
							},
						},
					},
				},
			},
		}

		patchRaw, _ := json.Marshal(patchUnstructured)
		prePatch := networkingv1alpha3.EnvoyFilter_Patch{}
		if err := prePatch.UnmarshalJSON(patchRaw); err != nil {
			return err
		}

		postPatch := prePatch.DeepCopy()

		// update filter name
		postPatch.Value.Fields["name"] = &types.Value{
			Kind: &types.Value_StringValue{
				StringValue: "envoy.filters.http.postauth.wasm",
			},
		}

		// update operation for postauth filter
		postPatch.Operation = networkingv1alpha3.EnvoyFilter_Patch_INSERT_BEFORE

		pluginConfig := postPatch.Value.Fields["typed_config"].GetStructValue().Fields["value"].GetStructValue().Fields["config"]

		// update plugin config for postauth filter
		pluginConfig.GetStructValue().Fields["configuration"].GetStructValue().Fields["value"] = &types.Value{
			Kind: &types.Value_StringValue{
				StringValue: string(postAuthJSON),
			},
		}

		// update plugin name
		pluginConfig.GetStructValue().Fields["name"] = &types.Value{
			Kind: &types.Value_StringValue{
				StringValue: "postauth-wasm",
			},
		}

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

		// since it's the first time, add the Limitador and Wasm cluster into the patches
		finalPatches = append(finalPatches, preAuthFilterPatch, postAuthFilterPatch,
			kuadrantistioutils.LimitadorClusterEnvoyPatch(), kuadrantistioutils.WasmClusterEnvoyPatch())
	} else {
		logger.Info("Updating EnvoyFilter")

		// use the old patches but update the wasm plugin configs
		finalPatches = append(finalPatches, existingObj.Spec.ConfigPatches...)
		for stage := 0; stage < 2; stage++ {
			patchValue := finalPatches[stage].Patch.Value
			pluginConfig := patchValue.Fields["typed_config"].GetStructValue().Fields["value"].GetStructValue().Fields["config"]
			pluginConfigString := pluginConfig.GetStructValue().Fields["configuration"].GetStructValue().Fields["value"].GetStringValue()

			parsedPluginConfig := &kuadrantistioutils.PluginConfig{}
			if err := json.Unmarshal([]byte(pluginConfigString), parsedPluginConfig); err != nil {
				return fmt.Errorf("failed to unmarshall existing plugin config")
			}

			if parsedPluginConfig.PluginPolicies == nil {
				parsedPluginConfig.PluginPolicies = make(map[string]kuadrantistioutils.PluginPolicy)
			}
			parsedPluginConfig.PluginPolicies[rlpKey.String()] = *preAuthPluginPolicy
			if stage == 1 {
				parsedPluginConfig.PluginPolicies[rlpKey.String()] = *postAuthPluginPolicy
			}
		}
	}

	factory := kuadrantistioutils.EnvoyFilterFactory{
		ObjectName: rlFiltersPatchName(gwKey.Name),
		Namespace:  gwKey.Namespace,
		Patches:    finalPatches,
		Labels:     gwLabels,
	}

	*existingObj = *factory.EnvoyFilter()

	logger.Info("successfully created/updated EnvoyFilter")
	return nil
}

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
	rlpKey := client.ObjectKeyFromObject(rlp)

	// create the RateLimit resource
	for i, rlSpec := range rlp.Spec.Limits {
		ratelimitfactory := common.RateLimitFactory{
			Key: client.ObjectKey{
				Name: limitadorRatelimitsName(rlpKey, i+1),
				// Currently, Limitador Operator (v0.2.0) will configure limitador services with
				// RateLimit CRs created in the same namespace.
				Namespace: common.KuadrantNamespace,
			},
			Conditions: rlSpec.Conditions,
			MaxValue:   rlSpec.MaxValue,
			Namespace:  rlSpec.Namespace,
			Variables:  rlSpec.Variables,
			Seconds:    rlSpec.Seconds,
		}

		ratelimit := ratelimitfactory.RateLimit()
		err := r.ReconcileResource(ctx, &v1alpha1.RateLimit{}, ratelimit, alwaysUpdateRateLimit)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "ReconcileResource failed to create/update RateLimit resource")
			return err
		}
	}
	logger.Info("successfully created/updated RateLimit resources")
	return nil
}

// fetchOperationsFromVS captures match rules from VirtualService and fill into RateLimitPolicy
func fetchOperationsFromVS(vs *networkingv1alpha3.VirtualService, rlp *apimv1alpha1.RateLimitPolicySpec) {
	for _, rule := range rlp.Rules {
		if len(rule.Name) > 0 {
			for _, httpRoute := range vs.Http {
				routeMatched, _ := regexp.MatchString(rule.Name, httpRoute.Name)

				for _, httpMatchReq := range httpRoute.Match {
					reqMatched, _ := regexp.MatchString(rule.Name, httpMatchReq.Name)

					if routeMatched || reqMatched {
						operation := apimv1alpha1.Operation{}

						if normalizedURI := normalizeStringMatch(httpMatchReq.Uri); normalizedURI != "" {
							operation.Paths = append(operation.Paths, normalizedURI)
						}

						if normalizedMethod := normalizeStringMatch(httpMatchReq.Method); normalizedMethod != "" {
							operation.Methods = append(operation.Methods, normalizedMethod)
						}

						rule.Operations = append(rule.Operations, &operation)
					}
				}
			}
		}
	}
}

func (r *RateLimitPolicyReconciler) attachNetworkToStatus(ctx context.Context, network *SignalingNetwork, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
	networkKind := network.NetworkKind
	networkName := network.NetworkName

	logger.V(1).Info("attaching network to status", "network kind", networkKind, "network name", networkName)
	rlp.Status.AddEntry(networkKind, networkName, network.Gateways)

	return r.Client().Status().Update(ctx, rlp)
}

func (r *RateLimitPolicyReconciler) detachNetworkFromStatus(ctx context.Context, network *SignalingNetwork, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
	networkKind := network.NetworkKind
	networkName := network.NetworkName

	logger.V(1).Info("detaching network from status", "network kind", networkKind, "network name", networkName)
	rlp.Status.DeleteEntry(networkKind, networkName)

	return r.Client().Status().Update(ctx, rlp)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apimv1alpha1.RateLimitPolicy{}).
		Complete(r)
}
