package apim

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	istioextensionv1alpha3 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-controller/pkg/istio"
)

const (
	// RateLimitPolicy finalizer
	rateLimitPolicyFinalizer = "ratelimitpolicy.kuadrant.io/finalizer"
)

// finalizeWASMPlugins removes the configuration of this RLP from each gateway's WASMPlugins.
func (r *RateLimitPolicyReconciler) finalizeWASMPlugins(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)

	httpRoute, err := r.fetchHTTPRoute(ctx, rlp)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("targetRef HTTPRoute not found")
			return nil
		}
		return err
	}

	for _, parentRef := range httpRoute.Spec.CommonRouteSpec.ParentRefs {
		gwKey := client.ObjectKey{Name: string(parentRef.Name), Namespace: httpRoute.Namespace}
		if parentRef.Namespace != nil {
			gwKey.Namespace = string(*parentRef.Namespace)
		}
		gateway := &gatewayapiv1alpha2.Gateway{}
		err := r.Client().Get(ctx, gwKey, gateway)
		logger.V(1).Info("finalizeWASMPlugins: get Gateway", "gateway", gwKey, "err", err)
		if apierrors.IsNotFound(err) {
			logger.Info("parentRef Gateway not found", "parentRef", gwKey)
			continue
		}
		if err != nil {
			return err
		}

		desiredWPs, err := kuadrantistioutils.WasmPlugins(rlp, gwKey, gateway.GetLabels(), httpRoute.Spec.Hostnames)
		if err != nil {
			return err
		}

		wasmPluginDeleted := false
		for _, desiredWP := range desiredWPs {
			wasmPlugin := &istioextensionv1alpha3.WasmPlugin{}
			err = r.Client().Get(ctx, client.ObjectKeyFromObject(desiredWP), wasmPlugin)
			logger.V(1).Info("finalizeWASMPlugins: get WasmPlugin", "wasmplugin", client.ObjectKeyFromObject(desiredWP), "err", err)
			if apierrors.IsNotFound(err) {
				logger.Info("finalizeWASMPlugins: wasmplugin not found", "wasmplguin", client.ObjectKeyFromObject(desiredWP))
				continue
			}
			if err != nil {
				return err
			}
			wasmPluginDeleted, err = r.finalizeSingleWASMPlugins(ctx, rlp, wasmPlugin)
			if err != nil {
				return err
			}
		}

		// finalize pre-requisite of WasmPlugin i.e. EnvoyFilter adding the limitador cluster entry
		if wasmPluginDeleted {
			ef := &istionetworkingv1alpha3.EnvoyFilter{}
			efKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: kuadrantistioutils.LimitadorClusterEnvoyFilterName}
			err := r.Client().Get(ctx, efKey, ef)
			logger.V(1).Info("finalizeWASMPlugins: get EnvoyFilter", "envoyfilter", efKey, "err", err)
			if apierrors.IsNotFound(err) {
				logger.Info("finalizeWASMPlugins: envoyfilter not found", "envoyFilter", efKey)
				continue
			}
			err = r.DeleteResource(ctx, ef)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// finalizeSingleWASMPlugins removes the configuration of this RLP
// If the WASMPlugin ends up with empty conf, the resource and its pre-requisite will be removed.
func (r *RateLimitPolicyReconciler) finalizeSingleWASMPlugins(
	ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy, wasmPlugin *istioextensionv1alpha3.WasmPlugin,
) (bool, error) {
	updateObject := false
	configEmpty := false
	// Deserialize config into PluginConfig struct
	configJSON, err := wasmPlugin.Spec.PluginConfig.MarshalJSON()
	if err != nil {
		return false, err
	}
	pluginConfig := &kuadrantistioutils.PluginConfig{}
	if err := json.Unmarshal(configJSON, pluginConfig); err != nil {
		return false, err
	}

	pluginKey := client.ObjectKeyFromObject(rlp).String()
	if _, ok := pluginConfig.PluginPolicies[pluginKey]; ok {
		delete(pluginConfig.PluginPolicies, pluginKey)
		updateObject = true
		finalPluginConfig, err := kuadrantistioutils.PluginConfigToWasmPluginStruct(pluginConfig)
		if err != nil {
			return false, err
		}
		wasmPlugin.Spec.PluginConfig = finalPluginConfig

		configEmpty = len(pluginConfig.PluginPolicies) == 0
	}

	if configEmpty {
		return true, r.DeleteResource(ctx, wasmPlugin)
	} else if updateObject {
		return false, r.UpdateResource(ctx, wasmPlugin)
	}

	return false, nil
}

func (r *RateLimitPolicyReconciler) deleteRateLimits(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)
	rlpKey := client.ObjectKeyFromObject(rlp)
	for i := range rlp.Spec.Limits {
		ratelimitfactory := common.RateLimitFactory{
			Key: client.ObjectKey{
				Name: limitadorRatelimitsName(rlpKey, i+1),
				// Currently, Limitador Operator (v0.2.0) will configure limitador services with
				// RateLimit CRs created in the same namespace.
				Namespace: common.KuadrantNamespace,
			},
			// rest of the parameters empty
		}

		rateLimit := ratelimitfactory.RateLimit()
		err := r.DeleteResource(ctx, rateLimit)
		logger.V(1).Info("Removing rate limit", "ratelimit", client.ObjectKeyFromObject(rateLimit), "error", err)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *RateLimitPolicyReconciler) deleteNetworkResourceBackReference(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)
	httpRoute, err := r.fetchHTTPRoute(ctx, rlp)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("targetRef HTTPRoute not found")
			return nil
		}
		return err
	}

	// Reconcile the back reference:
	httpRouteAnnotations := httpRoute.GetAnnotations()
	if httpRouteAnnotations == nil {
		httpRouteAnnotations = map[string]string{}
	}

	if _, ok := httpRouteAnnotations[common.RateLimitPolicyBackRefAnnotation]; ok {
		delete(httpRouteAnnotations, common.RateLimitPolicyBackRefAnnotation)
		httpRoute.SetAnnotations(httpRouteAnnotations)
		err := r.UpdateResource(ctx, httpRoute)
		logger.V(1).Info("deleteNetworkResourceBackReference: update HTTPRoute", "httpRoute", client.ObjectKeyFromObject(httpRoute), "err", err)
		if err != nil {
			return err
		}
	}
	return nil
}
