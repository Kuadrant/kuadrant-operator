package controllers

import (
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/client-go/dynamic"
)

type HTTPRoutePolicyDiscoverabilityReconciler struct {
	Client *dynamic.DynamicClient
}

func NewHTTPRoutePolicyDiscoverabilityReconciler(client *dynamic.DynamicClient) *HTTPRoutePolicyDiscoverabilityReconciler {
	return &HTTPRoutePolicyDiscoverabilityReconciler{Client: client}
}

func (r *HTTPRoutePolicyDiscoverabilityReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{}
}
