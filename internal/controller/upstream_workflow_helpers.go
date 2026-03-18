package controllers

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
)

const upstreamObjectLabelKey = "kuadrant.io/upstream"

func UpstreamObjectLabels() labels.Set {
	m := KuadrantManagedObjectLabels()
	m[upstreamObjectLabelKey] = "true"
	return m
}

func UpstreamClusterName(gatewayName string) string {
	return fmt.Sprintf("kuadrant-upstream-%s", gatewayName)
}
