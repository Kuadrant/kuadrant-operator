package apim

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy, gwDiffObj *gatewayDiff) error {
	logger, _ := logr.FromContext(ctx)
	limitadorKey := client.ObjectKey{Name: rlptools.LimitadorName, Namespace: rlptools.LimitadorNamespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err := r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("reconcileLimits", "get limitador", limitadorKey, "err", err)
	if err != nil {
		return err
	}

	limitIdx := rlptools.NewLimitadorIndex(limitador, logger)

	for _, leftGateway := range gwDiffObj.LeftGateways {
		logger.V(1).Info("reconcileLimits: left gateways", "key", leftGateway.Key())
		limitIdx.DeleteGateway(leftGateway.Key())
	}

	for _, sameGateway := range gwDiffObj.SameGateways {
		gwLimits, err := r.gatewayLimits(ctx, sameGateway.RLPRefs())
		if err != nil {
			return err
		}

		logger.V(1).Info("reconcileLimits: same gateways", "rlpRefs", sameGateway.RLPRefs())

		// delete first to detect when limits have been deleted.
		// For instance, gw A has 3 limits
		// one limit has been deleted for gwA (coming from a limit deletion in one RLP)
		// gw A has now 2 limits
		// Deleting the 3 original limits the resulting index will contain only 2 limits as expected
		limitIdx.DeleteGateway(sameGateway.Key())
		limitIdx.AddGatewayLimits(sameGateway.Key(), gwLimits)
	}

	for _, newGateway := range gwDiffObj.NewGateways {
		rlpRefs := append(newGateway.RLPRefs(), client.ObjectKeyFromObject(rlp))
		gwLimits, err := r.gatewayLimits(ctx, rlpRefs)
		if err != nil {
			return err
		}

		logger.V(1).Info("reconcileLimits: new gateways", "rlpRefs", rlpRefs)

		// The gw A had X limits from N RLPs
		// now there there are N+1 RLPs
		// r.gatewayLimits will compute all the limits for the given gateway with the N+1 RLPs
		// the existing limits need to be deleted first,
		// otherwise they would be added again and will be duplicated in the index
		limitIdx.DeleteGateway(newGateway.Key())
		limitIdx.AddGatewayLimits(newGateway.Key(), gwLimits)
	}

	// Build a new index with the original content of limitador to compare with the new limits
	originalLimitIndex := rlptools.NewLimitadorIndex(limitador, logger)

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(originalLimitIndex.ToLimits(), "", "  ")
		if err != nil {
			return err
		}
		logger.V(1).Info("reconcileLimits: original limit index")
		logger.V(1).Info(string(jsonData))

		jsonData, err = json.MarshalIndent(limitIdx.ToLimits(), "", "  ")
		if err != nil {
			return err
		}
		logger.V(1).Info("reconcileLimits: new limit index")
		logger.V(1).Info(string(jsonData))
	}

	equalIndexes := originalLimitIndex.Equals(limitIdx)
	logger.V(1).Info("reconcileLimits", "equal index", equalIndexes)

	if !equalIndexes {
		limitador.Spec.Limits = limitIdx.ToLimits()
		err := r.UpdateResource(ctx, limitador)
		logger.V(1).Info("reconcileLimits: update limitador", "limitador", limitadorKey, "err", err)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RateLimitPolicyReconciler) gatewayLimits(ctx context.Context, rlpRefs []client.ObjectKey) (rlptools.LimitsByDomain, error) {
	logger, _ := logr.FromContext(ctx)

	// Load all rate limit policies
	routeRLPList := make([]*apimv1alpha1.RateLimitPolicy, 0)
	gwRLPList := make([]*apimv1alpha1.RateLimitPolicy, 0)
	for _, rlpKey := range rlpRefs {
		rlp := &apimv1alpha1.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("gatewayLimits", "get rlp", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		if rlp.IsForHTTPRoute() {
			routeRLPList = append(routeRLPList, rlp)
		} else if rlp.IsForGateway() {
			gwRLPList = append(gwRLPList, rlp)
		}
	}

	limits := rlptools.LimitsByDomain{}

	// iterate over HTTPRoute RLP's.
	// Gateway level RLP's alone do not make sense.
	// The request would be rejected by the router with 404
	for _, httpRouteRLP := range routeRLPList {
		httpRoute, err := r.fetchHTTPRoute(ctx, httpRouteRLP)
		if err != nil {
			return nil, err
		}

		// gateways limits merged with the route level limits
		mergedLimits := mergeLimits(httpRouteRLP, gwRLPList)
		// routeLimits referenced by multiple hostnames
		for _, hostname := range httpRoute.Spec.Hostnames {
			limits[string(hostname)] = append(limits[string(hostname)], mergedLimits...)
		}
	}

	return limits, nil
}

// merged currently implemented with list append operation
func mergeLimits(routeRLP *apimv1alpha1.RateLimitPolicy, gwRLPs []*apimv1alpha1.RateLimitPolicy) []apimv1alpha1.Limit {
	limits := make([]apimv1alpha1.Limit, 0)

	// add route level limits
	// flatten Limit list from RateLimit list
	for idx := range routeRLP.Spec.RateLimits {
		limits = append(limits, routeRLP.Spec.RateLimits[idx].Limits...)
	}

	// add gateway level limits
	for _, gwRLP := range gwRLPs {
		// flatten Limit list from RateLimit list
		for idx := range gwRLP.Spec.RateLimits {
			limits = append(limits, gwRLP.Spec.RateLimits[idx].Limits...)
		}
	}

	return limits
}
