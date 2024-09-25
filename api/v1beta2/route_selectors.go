package v1beta2

import (
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
)

// +kubebuilder:object:generate=false
type RouteSelector = kuadrantv1beta3.RouteSelector

// +kubebuilder:object:generate=false
type RouteSelectorsGetter interface {
	GetRouteSelectors() []RouteSelector
}
