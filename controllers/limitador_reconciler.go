package controllers

import (
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/client-go/dynamic"
)

type LimitadorReconciler struct {
	Client *dynamic.DynamicClient
}

func NewLimitadorReconciler(client *dynamic.DynamicClient) *LimitadorReconciler {
	return &LimitadorReconciler{Client: client}
}

func (r *LimitadorReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{}
}
