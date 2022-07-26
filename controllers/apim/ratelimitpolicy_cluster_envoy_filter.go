package apim

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-controller/pkg/istio"
	"github.com/kuadrant/kuadrant-controller/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileRateLimitingClusterEnvoyFilter(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy, gwDiffObj *gatewayDiff) error {
	logger, _ := logr.FromContext(ctx)

	for _, leftGateway := range gwDiffObj.LeftGateways {
		logger.V(1).Info("reconcileWASMPluginConf: left gateways", "gw key", leftGateway.Key())
		rlpRefs := leftGateway.RLPRefs()
		rlpKey := client.ObjectKeyFromObject(rlp)
		// Remove the RLP key from the reference list. Only if it exists (it should)
		if refID := common.FindObjectKey(rlpRefs, rlpKey); refID != len(rlpRefs) {
			// remove index
			rlpRefs = append(rlpRefs[:refID], rlpRefs[refID+1:]...)
		}

		ef, err := r.gatewayRateLimitingClusterEnvoyFilter(ctx, leftGateway.Gateway, rlpRefs)
		if err != nil {
			return err
		}
		err = r.ReconcileResource(ctx, &istioclientnetworkingv1alpha3.EnvoyFilter{}, ef, kuadrantistioutils.AlwaysUpdateEnvoyFilter)
		if err != nil {
			return err
		}
	}

	// Nothing to do for the gwDiffObj.SameGateways

	for _, newGateway := range gwDiffObj.NewGateways {
		logger.V(1).Info("reconcileWASMPluginConf: new gateways", "gw key", newGateway.Key())
		rlpRefs := newGateway.RLPRefs()
		rlpKey := client.ObjectKeyFromObject(rlp)
		// Add the RLP key to the reference list. Only if it does not exist (it should not)
		if !common.ContainsObjectKey(rlpRefs, rlpKey) {
			rlpRefs = append(newGateway.RLPRefs(), rlpKey)
		}
		ef, err := r.gatewayRateLimitingClusterEnvoyFilter(ctx, newGateway.Gateway, rlpRefs)
		if err != nil {
			return err
		}
		err = r.ReconcileResource(ctx, &istioclientnetworkingv1alpha3.EnvoyFilter{}, ef, kuadrantistioutils.AlwaysUpdateEnvoyFilter)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RateLimitPolicyReconciler) gatewayRateLimitingClusterEnvoyFilter(
	ctx context.Context, gw *gatewayapiv1alpha2.Gateway,
	rlpRefs []client.ObjectKey) (*istioclientnetworkingv1alpha3.EnvoyFilter, error) {
	logger, _ := logr.FromContext(ctx)
	gwKey := client.ObjectKeyFromObject(gw)
	logger.V(1).Info("gatewayRateLimitingClusterEnvoyFilter", "gwKey", gwKey, "rlpRefs", rlpRefs)

	// Load all relevant rate limit policies
	routeRLPList := make([]*apimv1alpha1.RateLimitPolicy, 0)
	for _, rlpKey := range rlpRefs {
		rlp := &apimv1alpha1.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("gatewayRateLimitingClusterEnvoyFilter", "get rlp", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		if rlp.IsForHTTPRoute() {
			routeRLPList = append(routeRLPList, rlp)
		}
	}

	ef := &istioclientnetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("kuadrant-ratelimiting-cluster-%s", gw.Name),
			Namespace: gw.Namespace,
		},
		Spec: istioapinetworkingv1alpha3.EnvoyFilter{
			WorkloadSelector: &istioapinetworkingv1alpha3.WorkloadSelector{
				Labels: gw.Labels,
			},
			ConfigPatches: nil,
		},
	}

	if len(routeRLPList) < 1 {
		common.TagObjectToDelete(ef)
		return ef, nil
	}

	limitadorKey := client.ObjectKey{Name: rlptools.LimitadorName, Namespace: rlptools.LimitadorNamespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err := r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("gatewayRateLimitingClusterEnvoyFilter", "get limitador", limitadorKey, "err", err)
	if err != nil {
		return nil, err
	}
	if !limitador.Status.Ready() {
		return nil, fmt.Errorf("limitador Status not ready")
	}
	configPatches, err := kuadrantistioutils.LimitadorClusterPatch(limitador.Status.Service.Host, int(limitador.Status.Service.Ports.GRPC))
	if err != nil {
		return nil, err
	}
	ef.Spec.ConfigPatches = configPatches

	return ef, nil
}
