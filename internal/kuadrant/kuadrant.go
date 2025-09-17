package kuadrant

import (
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
)

const (
	ControllerName     = "kuadrant.io/policy-controller"
	TopologyLabel      = "kuadrant.io/topology"
	ObservabilityLabel = "kuadrant.io/observability"
	LimitadorName      = "limitador"
)

type Policy interface {
	kuadrantgatewayapi.Policy
	Kind() string
}
