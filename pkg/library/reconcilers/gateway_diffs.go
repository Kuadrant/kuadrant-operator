package reconcilers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/library/common"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type GatewayDiffs struct {
	GatewaysMissingPolicyRef     []GatewayWrapper
	GatewaysWithValidPolicyRef   []GatewayWrapper
	GatewaysWithInvalidPolicyRef []GatewayWrapper
}

// ComputeGatewayDiffs computes all the differences to reconcile regarding the gateways whose behaviors should/should not be extended by the policy.
// These include gateways directly referenced by the policy and gateways indirectly referenced through the policy's target network objects.
// * list of gateways to which the policy applies for the first time
// * list of gateways to which the policy no longer applies
// * list of gateways to which the policy still applies
// TODO(@guicassolato): unit test
func ComputeGatewayDiffs(ctx context.Context, k8sClient client.Reader, policy, targetNetworkObject client.Object) (*GatewayDiffs, error) {
	logger, _ := logr.FromContext(ctx)

	var gwKeys []client.ObjectKey
	if policy.GetDeletionTimestamp() == nil {
		gwKeys = targetedGatewayKeys(targetNetworkObject)
	}

	// TODO(rahulanand16nov): maybe think about optimizing it with a label later
	allGwList := &gatewayapiv1beta1.GatewayList{}
	err := k8sClient.List(ctx, allGwList)
	if err != nil {
		return nil, err
	}

	policyKind, ok := policy.(common.Referrer)
	if !ok {
		return nil, fmt.Errorf("policy %s is not a referrer", policy.GetObjectKind().GroupVersionKind())
	}

	gwDiff := &GatewayDiffs{
		GatewaysMissingPolicyRef:     gatewaysMissingPolicyRef(allGwList, client.ObjectKeyFromObject(policy), gwKeys, policyKind),
		GatewaysWithValidPolicyRef:   gatewaysWithValidPolicyRef(allGwList, client.ObjectKeyFromObject(policy), gwKeys, policyKind),
		GatewaysWithInvalidPolicyRef: gatewaysWithInvalidPolicyRef(allGwList, client.ObjectKeyFromObject(policy), gwKeys, policyKind),
	}

	logger.V(1).Info("ComputeGatewayDiffs",
		"missing-policy-ref", len(gwDiff.GatewaysMissingPolicyRef),
		"valid-policy-ref", len(gwDiff.GatewaysWithValidPolicyRef),
		"invalid-policy-ref", len(gwDiff.GatewaysWithInvalidPolicyRef),
	)

	return gwDiff, nil
}

// gatewaysMissingPolicyRef returns gateways referenced by the policy but that miss the reference to it the annotations
func gatewaysMissingPolicyRef(gwList *gatewayapiv1beta1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind common.Referrer) []GatewayWrapper {
	gateways := make([]GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := GatewayWrapper{&gateway, policyKind}
		if slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && !gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// gatewaysWithValidPolicyRef returns gateways referenced by the policy that also have the reference in the annotations
func gatewaysWithValidPolicyRef(gwList *gatewayapiv1beta1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind common.Referrer) []GatewayWrapper {
	gateways := make([]GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := GatewayWrapper{&gateway, policyKind}
		if slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// gatewaysWithInvalidPolicyRef returns gateways not referenced by the policy that still have the reference in the annotations
func gatewaysWithInvalidPolicyRef(gwList *gatewayapiv1beta1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind common.Referrer) []GatewayWrapper {
	gateways := make([]GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := GatewayWrapper{&gateway, policyKind}
		if !slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// targetedGatewayKeys returns the list of gateways in the hierarchy of a target network object
func targetedGatewayKeys(targetNetworkObject client.Object) []client.ObjectKey {
	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1beta1.HTTPRoute:
		gwKeys := make([]client.ObjectKey, 0)
		for _, parentRef := range obj.Spec.CommonRouteSpec.ParentRefs {
			gwKey := client.ObjectKey{Name: string(parentRef.Name), Namespace: obj.Namespace}
			if parentRef.Namespace != nil {
				gwKey.Namespace = string(*parentRef.Namespace)
			}
			gwKeys = append(gwKeys, gwKey)
		}
		return gwKeys

	case *gatewayapiv1beta1.Gateway:
		return []client.ObjectKey{client.ObjectKeyFromObject(targetNetworkObject)}

	// If the targetNetworkObject is nil, we don't fail; instead, we return an empty slice of gateway keys.
	// This is for supporting a smooth cleanup in cases where the network object has been deleted already
	default:
		return []client.ObjectKey{}
	}
}
