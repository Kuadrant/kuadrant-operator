package controllers

import (
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/client-go/dynamic"
)

type GatewayPolicyDiscoverabilityReconciler struct {
	Client *dynamic.DynamicClient
}

func NewGatewayPolicyDiscoverabilityReconciler(client *dynamic.DynamicClient) *GatewayPolicyDiscoverabilityReconciler {
	return &GatewayPolicyDiscoverabilityReconciler{Client: client}
}

func (r *GatewayPolicyDiscoverabilityReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{}
}
