//go:build unit

package kuadrant

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
)

const (
	NS = "nsA"
)

type FakePolicy struct {
	client.Object
	Hosts     []string
	targetRef gatewayapiv1alpha2.LocalPolicyTargetReference
}

func (p *FakePolicy) GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference {
	return p.targetRef
}

func (p *FakePolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &FakePolicyStatus{}
}

func (p *FakePolicy) Kind() string {
	return "FakePolicy"
}

func (p *FakePolicy) List(ctx context.Context, c client.Client, namespace string) []kuadrantgatewayapi.Policy {
	return nil
}

type FakePolicyStatus struct{}

func (s *FakePolicyStatus) GetConditions() []metav1.Condition {
	return nil
}
