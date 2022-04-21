package apim

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	protobuftypes "github.com/gogo/protobuf/types"
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

// finalizeWASMEnvoyFilters removes the configuration of this RLP from each gateway's EnvoyFilter.
func (r *RateLimitPolicyReconciler) finalizeWASMEnvoyFilters(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)

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
		logger.V(1).Info("finalizeWASMEnvoyFilters: get Gateway", "gateway", gwKey, "err", err)
		if apierrors.IsNotFound(err) {
			logger.Info("parentRef Gateway not found", "parentRef", gwKey)
			continue
		}
		if err != nil {
			return err
		}

		desiredEF, err := kuadrantistioutils.WASMEnvoyFilter(rlp, gwKey, gateway.GetLabels(), httpRoute.Spec.Hostnames)
		if err != nil {
			return err
		}

		envoyFilter := &istionetworkingv1alpha3.EnvoyFilter{}
		err = r.Client().Get(ctx, client.ObjectKeyFromObject(desiredEF), envoyFilter)
		logger.V(1).Info("finalizeWASMEnvoyFilters: get EnvoyFilter", "envoyFilter", client.ObjectKeyFromObject(desiredEF), "err", err)
		if apierrors.IsNotFound(err) {
			logger.Info("finalizeWASMEnvoyFilters: envoyfilter not found", "envoyfilter", client.ObjectKeyFromObject(desiredEF))
			continue
		}
		if err != nil {
			return err
		}
		err = r.finalizeSingleWASMEnvoyFilter(ctx, rlp, envoyFilter)
		if err != nil {
			return err
		}
	}

	return nil
}

// finalizeSingleWASMEnvoyFilter removes the configuration of this RLP
// If the envoyfilter ends up with empty conf, the resource will be removed.
func (r *RateLimitPolicyReconciler) finalizeSingleWASMEnvoyFilter(
	ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy, envoyFilter *istionetworkingv1alpha3.EnvoyFilter,
) error {
	// first patch is PRE
	// second patch is POST
	configEmpty := []bool{false, false}
	updateObject := false
	for idx := 0; idx < 2; idx++ {
		patch := envoyFilter.Spec.ConfigPatches[idx]
		pluginConfigFields := patch.Patch.Value.Fields["typed_config"].
			GetStructValue().Fields["value"].GetStructValue().Fields["config"].GetStructValue().
			Fields["configuration"].GetStructValue().Fields
		pluginConfigStr := pluginConfigFields["value"].GetStringValue()
		pluginConfig := &kuadrantistioutils.PluginConfig{}
		if err := json.Unmarshal([]byte(pluginConfigStr), pluginConfig); err != nil {
			return fmt.Errorf("finalizeSingleWASMEnvoyFilter: failed to unmarshal plugin config: %w", err)
		}

		pluginKey := client.ObjectKeyFromObject(rlp).String()
		if _, ok := pluginConfig.PluginPolicies[pluginKey]; ok {
			delete(pluginConfig.PluginPolicies, pluginKey)
			updateObject = true
			newPluginConfigSerialized, err := json.Marshal(pluginConfig)
			if err != nil {
				return fmt.Errorf("finalizeSingleWASMEnvoyFilter: failed to marshall new plugin config into json: %w", err)
			}
			// Update existing envoyfilter patch value
			pluginConfigFields["value"] = &protobuftypes.Value{
				Kind: &protobuftypes.Value_StringValue{
					StringValue: string(newPluginConfigSerialized),
				},
			}
		}
		configEmpty[idx] = len(pluginConfig.PluginPolicies) == 0
	}

	allConfigEmpty := true
	for idx := range configEmpty {
		allConfigEmpty = allConfigEmpty && configEmpty[idx]
	}

	if allConfigEmpty {
		return r.DeleteResource(ctx, envoyFilter)
	} else if updateObject {
		return r.UpdateResource(ctx, envoyFilter)
	}

	return nil
}

func (r *RateLimitPolicyReconciler) deleteRateLimits(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
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
	logger := logr.FromContext(ctx)
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
