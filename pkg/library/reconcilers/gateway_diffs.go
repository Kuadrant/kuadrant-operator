package reconcilers

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type GatewayDiffs struct {
	GatewaysMissingPolicyRef     []kuadrant.GatewayWrapper
	GatewaysWithValidPolicyRef   []kuadrant.GatewayWrapper
	GatewaysWithInvalidPolicyRef []kuadrant.GatewayWrapper
}

// ComputeGatewayDiffsDAG computes all the differences to reconcile regarding the gateways whose behaviors should/should not be extended by the policy.
// These include gateways directly referenced by the policy and gateways indirectly referenced through the policy's target network objects.
// * list of gateways to which the policy applies for the first time
// * list of gateways to which the policy no longer applies
// * list of gateways to which the policy still applies
func ComputeGatewayDiffsDAG(ctx context.Context, policy, targetNetworkObject client.Object, topology *kuadrantgatewayapi.Topology) (*GatewayDiffs, error) {
	logger, _ := logr.FromContext(ctx)

	var gwKeys []client.ObjectKey
	if policy.GetDeletionTimestamp() == nil {
		gwKeys = targetedGatewayKeys(targetNetworkObject)
	}

	gatewayNodeList := topology.Gateways()

	policyKind, ok := policy.(kuadrant.Referrer)
	if !ok {
		return nil, fmt.Errorf("policy %s is not a referrer", policy.GetObjectKind().GroupVersionKind())
	}

	gwDiff := &GatewayDiffs{
		GatewaysMissingPolicyRef:     gatewaysMissingPolicyRefDAG(gatewayNodeList, client.ObjectKeyFromObject(policy), gwKeys, policyKind),
		GatewaysWithValidPolicyRef:   gatewaysWithValidPolicyRefDAG(gatewayNodeList, client.ObjectKeyFromObject(policy), gwKeys, policyKind),
		GatewaysWithInvalidPolicyRef: gatewaysWithInvalidPolicyRefDAG(gatewayNodeList, client.ObjectKeyFromObject(policy), gwKeys, policyKind),
	}

	logger.V(1).Info("ComputeGatewayDiffs",
		"missing-policy-ref", len(gwDiff.GatewaysMissingPolicyRef),
		"valid-policy-ref", len(gwDiff.GatewaysWithValidPolicyRef),
		"invalid-policy-ref", len(gwDiff.GatewaysWithInvalidPolicyRef),
	)

	return gwDiff, nil
}

// gatewaysMissingPolicyRefDAG returns gateways referenced by the policy but that miss the reference to it the annotations
func gatewaysMissingPolicyRefDAG(gwList []kuadrantgatewayapi.GatewayNode, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind kuadrant.Referrer) []kuadrant.GatewayWrapper {
	gateways := make([]kuadrant.GatewayWrapper, 0)
	for i := range gwList {
		gateway := gwList[i].Gateway
		gw := kuadrant.GatewayWrapper{Gateway: gateway, Referrer: policyKind}
		if slices.Contains(policyGwKeys, client.ObjectKeyFromObject(gateway)) && !gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// gatewaysWithValidPolicyRefDAG returns gateways referenced by the policy that also have the reference in the annotations
func gatewaysWithValidPolicyRefDAG(gwList []kuadrantgatewayapi.GatewayNode, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind kuadrant.Referrer) []kuadrant.GatewayWrapper {
	gateways := make([]kuadrant.GatewayWrapper, 0)
	for i := range gwList {
		gateway := gwList[i].Gateway
		gw := kuadrant.GatewayWrapper{Gateway: gateway, Referrer: policyKind}
		if slices.Contains(policyGwKeys, client.ObjectKeyFromObject(gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// gatewaysWithInvalidPolicyRefDAG returns gateways not referenced by the policy that still have the reference in the annotations
func gatewaysWithInvalidPolicyRefDAG(gwList []kuadrantgatewayapi.GatewayNode, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind kuadrant.Referrer) []kuadrant.GatewayWrapper {
	gateways := make([]kuadrant.GatewayWrapper, 0)
	for i := range gwList {
		gateway := gwList[i].Gateway
		gw := kuadrant.GatewayWrapper{Gateway: gateway, Referrer: policyKind}
		if !slices.Contains(policyGwKeys, client.ObjectKeyFromObject(gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// ComputeGatewayDiffs computes all the differences to reconcile regarding the gateways whose behaviors should/should not be extended by the policy.
// These include gateways directly referenced by the policy and gateways indirectly referenced through the policy's target network objects.
// * list of gateways to which the policy applies for the first time
// * list of gateways to which the policy no longer applies
// * list of gateways to which the policy still applies
// TODO(@guicassolato): unit test
// Deprecated: This is being improved with use of the DAG, but as it cross multiple controller not every instances is ready to use the DAG.
// Try use ComputeGatewayDiffsDAG is possible.
func ComputeGatewayDiffs(ctx context.Context, k8sClient client.Reader, policy, targetNetworkObject client.Object) (*GatewayDiffs, error) {
	logger, _ := logr.FromContext(ctx)

	var gwKeys []client.ObjectKey
	if policy.GetDeletionTimestamp() == nil {
		gwKeys = targetedGatewayKeys(targetNetworkObject)
	}

	// TODO(rahulanand16nov): maybe think about optimizing it with a label later
	allGwList := &gatewayapiv1.GatewayList{}
	err := k8sClient.List(ctx, allGwList)
	if err != nil {
		return nil, err
	}

	policyKind, ok := policy.(kuadrant.Referrer)
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
// Deprecated: This is being improved with use of the DAG, but as it cross multiple controller not every instance is ready to use the DAG.
// Try use gatewaysMissingPolicyRefDAG is possible.
func gatewaysMissingPolicyRef(gwList *gatewayapiv1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind kuadrant.Referrer) []kuadrant.GatewayWrapper {
	gateways := make([]kuadrant.GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := kuadrant.GatewayWrapper{Gateway: &gateway, Referrer: policyKind}
		if slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && !gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// gatewaysWithValidPolicyRef returns gateways referenced by the policy that also have the reference in the annotations
// Deprecated: This is being improved with use of the DAG, but as it cross multiple controller not every instance is ready to use the DAG.
// Try use gatewaysWithValidPolicyRefDAG is possible.
func gatewaysWithValidPolicyRef(gwList *gatewayapiv1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind kuadrant.Referrer) []kuadrant.GatewayWrapper {
	gateways := make([]kuadrant.GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := kuadrant.GatewayWrapper{Gateway: &gateway, Referrer: policyKind}
		if slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// gatewaysWithInvalidPolicyRef returns gateways not referenced by the policy that still have the reference in the annotations
// Deprecated: This is being improved with use of the DAG, but as it cross multiple controller not every instance is ready to use the DAG.
// Try use gatewaysWithInvalidPolicyRefDAG is possible.
func gatewaysWithInvalidPolicyRef(gwList *gatewayapiv1.GatewayList, policyKey client.ObjectKey, policyGwKeys []client.ObjectKey, policyKind kuadrant.Referrer) []kuadrant.GatewayWrapper {
	gateways := make([]kuadrant.GatewayWrapper, 0)
	for i := range gwList.Items {
		gateway := gwList.Items[i]
		gw := kuadrant.GatewayWrapper{Gateway: &gateway, Referrer: policyKind}
		if !slices.Contains(policyGwKeys, client.ObjectKeyFromObject(&gateway)) && gw.ContainsPolicy(policyKey) {
			gateways = append(gateways, gw)
		}
	}
	return gateways
}

// targetedGatewayKeys returns the list of gateways in the hierarchy of a target network object
func targetedGatewayKeys(targetNetworkObject client.Object) []client.ObjectKey {
	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1.HTTPRoute:
		gwKeys := make([]client.ObjectKey, 0)
		for _, parentRef := range obj.Spec.CommonRouteSpec.ParentRefs {
			gwKey := client.ObjectKey{Name: string(parentRef.Name), Namespace: obj.Namespace}
			if parentRef.Namespace != nil {
				gwKey.Namespace = string(*parentRef.Namespace)
			}
			gwKeys = append(gwKeys, gwKey)
		}
		return gwKeys

	case *gatewayapiv1.Gateway:
		return []client.ObjectKey{client.ObjectKeyFromObject(targetNetworkObject)}

	// If the targetNetworkObject is nil, we don't fail; instead, we return an empty slice of gateway keys.
	// This is for supporting a smooth cleanup in cases where the network object has been deleted already
	default:
		return []client.ObjectKey{}
	}
}
