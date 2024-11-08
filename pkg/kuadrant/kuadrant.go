package kuadrant

import (
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/gatewayapi"
)

const (
	ControllerName               = "kuadrant.io/policy-controller"
	TopologyLabel                = "kuadrant.io/topology"
	KuadrantRateLimitClusterName = "kuadrant-ratelimit-service"
	KuadrantAuthClusterName      = "kuadrant-auth-service"
	LimitadorName                = "limitador"
)

type Policy interface {
	kuadrantgatewayapi.Policy
	Kind() string
}
