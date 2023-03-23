package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

func (r *RateLimitPolicyReconciler) reconcileRateLimitingClusterEnvoyFilter(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy, gwDiffObj *reconcilers.GatewayDiff) error {
	logger, _ := logr.FromContext(ctx)

	for _, gw := range gwDiffObj.GatewaysWithInvalidPolicyRef {
		logger.V(1).Info("reconcileRateLimitingClusterEnvoyFilter: gateway with invalid policy ref", "gw key", gw.Key())
		rlpRefs := gw.PolicyRefs()
		rlpKey := client.ObjectKeyFromObject(rlp)
		// Remove the RLP key from the reference list. Only if it exists (it should)
		if refID := common.FindObjectKey(rlpRefs, rlpKey); refID != len(rlpRefs) {
			// remove index
			rlpRefs = append(rlpRefs[:refID], rlpRefs[refID+1:]...)
		}

		ef, err := r.gatewayRateLimitingClusterEnvoyFilter(ctx, gw.Gateway, rlpRefs)
		if err != nil {
			return err
		}
		err = r.ReconcileResource(ctx, &istioclientnetworkingv1alpha3.EnvoyFilter{}, ef, kuadrantistioutils.AlwaysUpdateEnvoyFilter)
		if err != nil {
			return err
		}
	}

	// Nothing to do for the gwDiffObj.GatewaysWithValidPolicyRef

	for _, gw := range gwDiffObj.GatewaysMissingPolicyRef {
		logger.V(1).Info("reconcileRateLimitingClusterEnvoyFilter: gateway missing policy ref", "gw key", gw.Key())
		rlpRefs := gw.PolicyRefs()
		rlpKey := client.ObjectKeyFromObject(rlp)
		// Add the RLP key to the reference list. Only if it does not exist (it should not)
		if !common.ContainsObjectKey(rlpRefs, rlpKey) {
			rlpRefs = append(gw.PolicyRefs(), rlpKey)
		}
		ef, err := r.gatewayRateLimitingClusterEnvoyFilter(ctx, gw.Gateway, rlpRefs)
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

func (r *RateLimitPolicyReconciler) gatewayRateLimitingClusterEnvoyFilter(ctx context.Context, gw *gatewayapiv1beta1.Gateway, rlpRefs []client.ObjectKey) (*istioclientnetworkingv1alpha3.EnvoyFilter, error) {
	logger, _ := logr.FromContext(ctx)
	gwKey := client.ObjectKeyFromObject(gw)
	logger.V(1).Info("gatewayRateLimitingClusterEnvoyFilter", "gwKey", gwKey, "rlpRefs", rlpRefs)

	kuadrantNamespace, err := common.GetKuadrantNamespace(gw)
	if err != nil {
		return nil, errors.NewInternalError(fmt.Errorf("gateway is not Kuadrant managed"))
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
				Labels: common.IstioWorkloadSelectorFromGateway(ctx, r.Client(), gw).MatchLabels,
			},
			ConfigPatches: nil,
		},
	}

	if len(rlpRefs) < 1 {
		common.TagObjectToDelete(ef)
		return ef, nil
	}

	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantNamespace}

	limitador := &limitadorv1alpha1.Limitador{}
	err = r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("gatewayRateLimitingClusterEnvoyFilter", "get limitador", limitadorKey, "err", err)
	if err != nil {
		return nil, err
	}

	if !meta.IsStatusConditionTrue(limitador.Status.Conditions, "Ready") {
		return nil, fmt.Errorf("limitador Status not ready")
	}

	configPatches, err := kuadrantistioutils.LimitadorClusterPatch(limitador.Status.Service.Host, int(limitador.Status.Service.Ports.GRPC))
	if err != nil {
		return nil, err
	}
	ef.Spec.ConfigPatches = configPatches

	return ef, nil
}
