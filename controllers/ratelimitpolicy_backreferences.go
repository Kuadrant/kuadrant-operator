package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileDirectBackReferences(ctx context.Context) error {
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
		val := desiredBackReferenceAnnotation()
		// TODO
		// no annotation -> delete only if exists
		// annotation -> if does not exist -> create
		//            -> if exists and equal -> NO OP
		//            -> if exists and diff -> update
	}

	routeNodeList := topology.Routes()

	logger.V(1).Info("reconcile back references", "#routes", len(routeNodeList))

	for _, routeNode := range routeNodeList {
		val := desiredBackReferenceAnnotation()
	}

	return nil
}

func desiredBackReferenceAnnotation() *string {
	return nil
}
