package controllers

import (
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/client-go/dynamic"
)

type EnvoyGatewayJanitor struct {
	Client *dynamic.DynamicClient
}

func NewEnvoyGatewayJanitor(client *dynamic.DynamicClient) *EnvoyGatewayJanitor {
	return &EnvoyGatewayJanitor{Client: client}
}

func (r *EnvoyGatewayJanitor) Subscription() *controller.Subscription {
	return &controller.Subscription{}
}
