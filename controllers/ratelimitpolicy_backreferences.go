package controllers

import (
	"context"

	"github.com/go-logr/logr"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileDirectBackReferences(ctx context.Context) error {
	// TODO(eguzki): make this method generic to any policy of a kind
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	topology, err := rlptools.Topology(ctx, r.Client())
	if err != nil {
		return err
	}

	gwNodeList := topology.Gateways()

	logger.V(1).Info("reconcile back references", "#gateways", len(gwNodeList))

	for _, gwNode := range gwNodeList {
		err := kuadrant.ReconcilePolicyReferenceOnObject(
			ctx, r.Client(), kuadrantv1beta2.RateLimitPolicyGVK,
			gwNode.GetGateway(), gwNode.AttachedPolicies(),
		)
		if err != nil {
			return err
		}
	}

	routeNodeList := topology.Routes()

	logger.V(1).Info("reconcile back references", "#routes", len(routeNodeList))

	for _, routeNode := range routeNodeList {
		err := kuadrant.ReconcilePolicyReferenceOnObject(
			ctx, r.Client(), kuadrantv1beta2.RateLimitPolicyGVK,
			routeNode.Route(), routeNode.AttachedPolicies(),
		)
		if err != nil {
			return err
		}
	}

	return nil
}
