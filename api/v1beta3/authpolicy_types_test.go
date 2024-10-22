//go:build unit

package v1beta3

import (
	"testing"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func TestAuthPolicyListGetItems(t *testing.T) {
	list := &AuthPolicyList{}
	if len(list.GetItems()) != 0 {
		t.Errorf("Expected empty list of items")
	}
	policy := AuthPolicy{}
	list.Items = []AuthPolicy{policy}
	result := list.GetItems()
	if len(result) != 1 {
		t.Errorf("Expected 1 item, got %d", len(result))
	}
	_, ok := result[0].(kuadrant.Policy)
	if !ok {
		t.Errorf("Expected item to be a Policy")
	}
}
