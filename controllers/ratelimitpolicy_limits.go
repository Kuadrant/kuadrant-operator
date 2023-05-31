package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, gwDiffObj *reconcilers.GatewayDiff) error {
	logger, _ := logr.FromContext(ctx)

	logger.V(1).Info("Getting Kuadrant namespace")
	var kuadrantNamespace string
	kuadrantNamespace, isSet := common.GetNamespaceFromPolicy(rlp)
	if !isSet {
		var err error
		kuadrantNamespace, err = common.GetNamespaceFromPolicyTargetRef(ctx, r.Client(), rlp)
		if err != nil {
			logger.Error(err, "failed to get Kuadrant namespace")
			return err
		}
		common.AnnotateObject(rlp, kuadrantNamespace)
		err = r.UpdateResource(ctx, rlp)
		if err != nil {
			logger.Error(err, "failed to update policy, re-queuing")
			return err
		}
	}

	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantNamespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err := r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("reconcileLimits", "get limitador", limitadorKey, "err", err)
	if err != nil {
		return err
	}

	limitIdx := rlptools.NewLimitadorIndex(limitador, logger)

	for _, gw := range gwDiffObj.GatewaysWithInvalidPolicyRef {
		logger.V(1).Info("reconcileLimits: gateway with invalid policy ref", "key", gw.Key())
		limitIdx.DeleteGateway(gw.Key())
	}

	for _, gw := range gwDiffObj.GatewaysWithValidPolicyRef {
		logger.V(1).Info("reconcileLimits: gateway with valid policy ref", "rlpRefs", gw.PolicyRefs())

		gwLimits, err := r.gatewayLimits(ctx, gw, gw.PolicyRefs())
		if err != nil {
			return err
		}

		// delete first to detect when limits have been deleted.
		// For instance, gw A has 3 limits
		// one limit has been deleted for gwA (coming from a limit deletion in one RLP)
		// gw A has now 2 limits
		// Deleting the 3 original limits the resulting index will contain only 2 limits as expected
		limitIdx.DeleteGateway(gw.Key())
		limitIdx.AddGatewayLimits(gw.Key(), gwLimits)
	}

	for _, gw := range gwDiffObj.GatewaysMissingPolicyRef {
		rlpRefs := append(gw.PolicyRefs(), client.ObjectKeyFromObject(rlp))
		logger.V(1).Info("reconcileLimits: gateway missing policy ref", "rlpRefs", rlpRefs)

		gwLimits, err := r.gatewayLimits(ctx, gw, rlpRefs)
		if err != nil {
			return err
		}

		// The gw A had X limits from N RLPs
		// now there there are N+1 RLPs
		// r.gatewayLimits will compute all the limits for the given gateway with the N+1 RLPs
		// the existing limits need to be deleted first,
		// otherwise they would be added again and will be duplicated in the index
		limitIdx.DeleteGateway(gw.Key())
		limitIdx.AddGatewayLimits(gw.Key(), gwLimits)
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

func (r *RateLimitPolicyReconciler) gatewayLimits(ctx context.Context,
	gw common.GatewayWrapper, rlpRefs []client.ObjectKey) (rlptools.LimitsByDomain, error) {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("gatewayLimits", "gwKey", gw.Key(), "rlpRefs", rlpRefs)

	// Load all rate limit policies
	routeRLPList := make([]*kuadrantv1beta2.RateLimitPolicy, 0)
	var gwRLP *kuadrantv1beta2.RateLimitPolicy
	for _, rlpKey := range rlpRefs {
		rlp := &kuadrantv1beta2.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("gatewayLimits", "get rlp", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		if common.IsTargetRefHTTPRoute(rlp.Spec.TargetRef) {
			routeRLPList = append(routeRLPList, rlp)
		} else if common.IsTargetRefGateway(rlp.Spec.TargetRef) {
			if gwRLP == nil {
				gwRLP = rlp
			} else {
				return nil, fmt.Errorf("gatewayLimits: multiple gateway RLP found and only one expected. rlp keys: %v", rlpRefs)
			}
		}
	}

	limits := rlptools.LimitsByDomain{}

	if gwRLP != nil {
		if len(gw.Hostnames()) == 0 {
			// wildcard domain
			limits["*"] = append(limits["*"], rlptools.ReadLimitsFromRLP(gwRLP)...)
		} else {
			for _, gwHostname := range gw.Hostnames() {
				limits[gwHostname] = append(limits[gwHostname], rlptools.ReadLimitsFromRLP(gwRLP)...)
			}
		}
	}

	for _, httpRouteRLP := range routeRLPList {
		httpRoute, err := r.FetchValidHTTPRoute(ctx, httpRouteRLP.TargetKey())
		if err != nil {
			return nil, err
		}

		// gateways limits merged with the route level limits
		mergedLimits := mergeLimits(httpRouteRLP, gwRLP)
		// routeLimits referenced by multiple hostnames
		for _, hostname := range httpRoute.Spec.Hostnames {
			limits[string(hostname)] = append(limits[string(hostname)], mergedLimits...)
		}
	}

	return limits, nil
}

// merged currently implemented with list append operation
func mergeLimits(routeRLP *kuadrantv1beta2.RateLimitPolicy, gwRLP *kuadrantv1beta2.RateLimitPolicy) []rlptools.Limit {
	limits := rlptools.ReadLimitsFromRLP(routeRLP)

	if gwRLP == nil {
		return limits
	}

	// add gateway level limits
	return append(limits, rlptools.ReadLimitsFromRLP(gwRLP)...)
}
