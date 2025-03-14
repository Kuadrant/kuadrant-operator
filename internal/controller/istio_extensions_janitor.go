package controllers

import (
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/client-go/dynamic"
)

type IstioExtensionsJanitor struct {
	Client *dynamic.DynamicClient
}

func NewIstioExtensionsJanitor(client *dynamic.DynamicClient) *IstioExtensionsJanitor {
	return &IstioExtensionsJanitor{Client: client}
}

func (r *IstioExtensionsJanitor) Subscription() *controller.Subscription {
	return &controller.Subscription{}
}
