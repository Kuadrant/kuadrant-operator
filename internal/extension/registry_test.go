//go:build unit

package extension

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"

	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	"github.com/kuadrant/policy-machinery/machinery"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func testResourceID(kind, namespace, name string) ResourceID {
	return ResourceID{Kind: kind, Namespace: namespace, Name: name}
}

func TestRegisteredDataStore_Set_Get_Delete(t *testing.T) {
	store := NewRegisteredDataStore()

	entry := DataProviderEntry{
		Policy:     testResourceID("Extension", "ns1", "ext1"),
		Binding:    "user",
		Expression: "user.id",
		CAst:       nil,
	}

	mockTargetRef := createMockGatewayTargetRef()
	store.Set(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_UNSPECIFIED, "user", entry)

	retrieved, exists := store.Get(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_UNSPECIFIED, "user")
	if !exists {
		t.Fatal("Expected entry to exist")
	}

	if retrieved.Policy != entry.Policy {
		t.Errorf("Expected policy %+v, got %+v", entry.Policy, retrieved.Policy)
	}

	if retrieved.Binding != entry.Binding {
		t.Errorf("Expected binding %s, got %s", entry.Binding, retrieved.Binding)
	}

	entries := store.GetAllForTargetRef(mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_UNSPECIFIED)
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}

	deleted := store.Delete(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_UNSPECIFIED, "user")
	if !deleted {
		t.Error("Expected entry to be deleted")
	}
}

func TestRegisteredDataStore_SetSubscription(t *testing.T) {
	store := NewRegisteredDataStore()

	subscription := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}

	policyID := testResourceID("AuthPolicy", "test-ns", "test-policy")
	expression := "some.expression"
	store.SetSubscription(policyID, expression, subscription)

	retrieved, exists := store.GetSubscription(policyID, expression)
	if !exists {
		t.Fatal("Expected subscription to exist")
	}

	if policy, ok := retrieved.Input["self"].(*extpb.Policy); ok {
		if policy.Metadata.Kind != "AuthPolicy" {
			t.Errorf("Expected kind AuthPolicy, got %s", policy.Metadata.Kind)
		}
		if policy.Metadata.Namespace != "test-ns" {
			t.Errorf("Expected namespace test-ns, got %s", policy.Metadata.Namespace)
		}
		if policy.Metadata.Name != "test-policy" {
			t.Errorf("Expected name test-policy, got %s", policy.Metadata.Name)
		}
	} else {
		t.Error("Expected policy in subscription input")
	}

	allSubs := store.GetAllSubscriptions()
	if len(allSubs) != 1 {
		t.Errorf("Expected 1 subscription, got %d", len(allSubs))
	}

	deleted := store.DeleteSubscription(policyID, expression)
	if !deleted {
		t.Error("Expected subscription to be deleted")
	}

	_, exists = store.GetSubscription(policyID, expression)
	if exists {
		t.Error("Expected subscription to not exist after deletion")
	}
}

func TestRegisteredDataStore_ClearPolicyData(t *testing.T) {
	store := NewRegisteredDataStore()

	testPolicy := testResourceID("AuthPolicy", "test-ns", "test-policy")
	otherPolicy := testResourceID("AuthPolicy", "other-ns", "other-policy")

	entry1 := DataProviderEntry{
		Policy:     testPolicy,
		Binding:    "user_id",
		Expression: "user.id",
		CAst:       nil,
	}
	entry2 := DataProviderEntry{
		Policy:     testPolicy,
		Binding:    "user_email",
		Expression: "user.email",
		CAst:       nil,
	}
	entry3 := DataProviderEntry{
		Policy:     otherPolicy,
		Binding:    "role",
		Expression: "user.role",
		CAst:       nil,
	}

	mockTargetRef := createMockGatewayTargetRef()
	store.Set(testPolicy, mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "binding1", entry1)
	store.Set(testPolicy, mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "binding2", entry2)
	store.Set(otherPolicy, mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "binding3", entry3)

	subscription1 := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}
	subscription2 := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "other-ns",
					Name:      "other-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}

	store.SetSubscription(testPolicy, "expression1", subscription1)
	store.SetSubscription(otherPolicy, "expression2", subscription2)

	allSubs := store.GetAllSubscriptions()
	if len(allSubs) != 2 {
		t.Errorf("Expected 2 subscriptions, got %d", len(allSubs))
	}

	entries := store.GetAllForTargetRef(mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH)
	if len(entries) != 3 {
		t.Errorf("Expected 3 entries for target ref, got %d", len(entries))
	}

	clearedMutators, clearedSubscriptions, _ := store.ClearPolicyData(testPolicy)

	if clearedMutators != 2 {
		t.Errorf("Expected 2 cleared mutators, got %d", clearedMutators)
	}
	if clearedSubscriptions != 1 {
		t.Errorf("Expected 1 cleared subscription, got %d", clearedSubscriptions)
	}

	entries = store.GetAllForTargetRef(mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH)
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry after clear (other policy), got %d", len(entries))
	}

	_, exists := store.GetSubscription(testPolicy, "expression1")
	if exists {
		t.Error("Expected subscription1 to be cleared")
	}

	_, exists = store.GetSubscription(otherPolicy, "expression2")
	if !exists {
		t.Error("Expected subscription2 to still exist")
	}

	finalSubs := store.GetAllSubscriptions()
	if len(finalSubs) != 1 {
		t.Errorf("Expected 1 subscription after clear, got %d", len(finalSubs))
	}

	testSubsAfter := store.GetPolicySubscriptions(testPolicy)
	if len(testSubsAfter) != 0 {
		t.Error("Expected test policy to have no subscriptions after clear")
	}

	otherSubsAfter := store.GetPolicySubscriptions(otherPolicy)
	if len(otherSubsAfter) != 1 {
		t.Errorf("Expected other policy to have 1 subscription after clear, got %d", len(otherSubsAfter))
	}
}

func TestRegisteredDataStore_PolicyDataLifecycle(t *testing.T) {
	store := NewRegisteredDataStore()

	mockTargetRef := createMockGatewayTargetRef()
	entries := store.GetAllForTargetRef(mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH)
	subscriptions := store.GetPolicySubscriptions(testResourceID("AuthPolicy", "test-ns", "test-policy"))
	if len(entries) != 0 || len(subscriptions) != 0 {
		t.Error("Expected no policy data initially")
	}

	entry := DataProviderEntry{
		Policy:     testResourceID("Extension", "ns1", "ext1"),
		Binding:    "user_id",
		Expression: "user.id",
		CAst:       nil,
	}
	store.Set(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "user_id", entry)

	entries = store.GetAllForTargetRef(mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH)
	if len(entries) == 0 {
		t.Error("Expected policy data after adding entry")
	}

	store.ClearPolicyData(testResourceID("Extension", "ns1", "ext1")) //nolint:dogsled

	subscription := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}
	store.SetSubscription(testResourceID("AuthPolicy", "test-ns", "test-policy"), "user.data", subscription)

	subscriptions = store.GetPolicySubscriptions(testResourceID("AuthPolicy", "test-ns", "test-policy"))
	if len(subscriptions) == 0 {
		t.Error("Expected policy subscription after adding subscription")
	}
}

func TestRegisteredDataStore_UpdateSubscriptionValue(t *testing.T) {
	store := NewRegisteredDataStore()

	subscription := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}

	store.SetSubscription(testResourceID("AuthPolicy", "test-ns", "test-policy"), "some.expression", subscription)

	newVal := ref.Val(nil)
	updated := store.UpdateSubscriptionValue(testResourceID("AuthPolicy", "test-ns", "test-policy"), "some.expression", newVal)
	if !updated {
		t.Error("Expected subscription value to be updated")
	}

	updated = store.UpdateSubscriptionValue(testResourceID("non-existent", "ns", "name"), "expression", newVal)
	if updated {
		t.Error("Expected update to fail for non-existent subscription")
	}
}

func TestRegisteredDataStore_GetPolicySubscriptions(t *testing.T) {
	store := NewRegisteredDataStore()

	subscription1 := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}
	subscription2 := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}
	subscription3 := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "other-ns",
					Name:      "other-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}

	store.SetSubscription(testResourceID("AuthPolicy", "test-ns", "test-policy"), "expression1", subscription1)
	store.SetSubscription(testResourceID("AuthPolicy", "test-ns", "test-policy"), "expression2", subscription2)
	store.SetSubscription(testResourceID("AuthPolicy", "other-ns", "other-policy"), "expression3", subscription3)

	subscriptions := store.GetPolicySubscriptions(testResourceID("AuthPolicy", "test-ns", "test-policy"))
	if len(subscriptions) != 2 {
		t.Errorf("Expected 2 subscriptions for test policy, got %d", len(subscriptions))
	}

	subscriptions = store.GetPolicySubscriptions(testResourceID("AuthPolicy", "other-ns", "other-policy"))
	if len(subscriptions) != 1 {
		t.Errorf("Expected 1 subscription for other policy, got %d", len(subscriptions))
	}

	subscriptions = store.GetPolicySubscriptions(testResourceID("AuthPolicy", "non-existent", "policy"))
	if len(subscriptions) != 0 {
		t.Errorf("Expected 0 subscriptions for non-existent policy, got %d", len(subscriptions))
	}
}

func TestRegisteredDataStore_ClearPolicySubscriptions(t *testing.T) {
	store := NewRegisteredDataStore()

	subscription1 := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}
	subscription2 := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}
	subscription3 := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "other-ns",
					Name:      "other-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}

	store.SetSubscription(testResourceID("AuthPolicy", "test-ns", "test-policy"), "expression1", subscription1)
	store.SetSubscription(testResourceID("AuthPolicy", "test-ns", "test-policy"), "expression2", subscription2)
	store.SetSubscription(testResourceID("AuthPolicy", "other-ns", "other-policy"), "expression3", subscription3)

	_, cleared, _ := store.ClearPolicyData(testResourceID("AuthPolicy", "test-ns", "test-policy"))
	if cleared != 2 {
		t.Errorf("Expected 2 cleared subscriptions, got %d", cleared)
	}

	subscriptions := store.GetPolicySubscriptions(testResourceID("AuthPolicy", "test-ns", "test-policy"))
	if len(subscriptions) != 0 {
		t.Errorf("Expected 0 subscriptions after clear, got %d", len(subscriptions))
	}

	subscriptions = store.GetPolicySubscriptions(testResourceID("AuthPolicy", "other-ns", "other-policy"))
	if len(subscriptions) != 1 {
		t.Errorf("Expected 1 subscription for other policy, got %d", len(subscriptions))
	}

	_, cleared, _ = store.ClearPolicyData(testResourceID("AuthPolicy", "non-existent", "policy"))
	if cleared != 0 {
		t.Errorf("Expected 0 cleared subscriptions for non-existent policy, got %d", cleared)
	}
}

func TestRegisteredDataStore_GetSubscriptionsForPolicyKind(t *testing.T) {
	store := NewRegisteredDataStore()

	authPolicySubscription := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "AuthPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "AuthPolicy",
	}

	rateLimitPolicySubscription := Subscription{
		CAst: &cel.Ast{},
		Input: map[string]any{
			"self": &extpb.Policy{
				Metadata: &extpb.Metadata{
					Kind:      "RateLimitPolicy",
					Namespace: "test-ns",
					Name:      "test-policy",
				},
			},
		},
		Val:        nil,
		PolicyKind: "RateLimitPolicy",
	}

	store.SetSubscription(testResourceID("AuthPolicy", "test-ns", "test-policy"), "expression1", authPolicySubscription)
	store.SetSubscription(testResourceID("RateLimitPolicy", "test-ns", "test-policy"), "expression1", rateLimitPolicySubscription)

	authSubscriptions := store.GetSubscriptionsForPolicyKind("AuthPolicy")
	if len(authSubscriptions) != 1 {
		t.Errorf("Expected 1 AuthPolicy subscription, got %d", len(authSubscriptions))
	}

	rateLimitSubscriptions := store.GetSubscriptionsForPolicyKind("RateLimitPolicy")
	if len(rateLimitSubscriptions) != 1 {
		t.Errorf("Expected 1 RateLimitPolicy subscription, got %d", len(rateLimitSubscriptions))
	}

	nonExistentSubscriptions := store.GetSubscriptionsForPolicyKind("NonExistentPolicy")
	if len(nonExistentSubscriptions) != 0 {
		t.Errorf("Expected 0 NonExistentPolicy subscriptions, got %d", len(nonExistentSubscriptions))
	}

	for key, sub := range authSubscriptions {
		if sub.PolicyKind != "AuthPolicy" {
			t.Errorf("Expected AuthPolicy subscription, got %s", sub.PolicyKind)
		}
		expectedKey := SubscriptionKey{Policy: testResourceID("AuthPolicy", "test-ns", "test-policy"), Expression: "expression1"}
		if key != expectedKey {
			t.Errorf("Expected key %+v, got %+v", expectedKey, key)
		}
	}
}

func TestRegisteredDataMutator(t *testing.T) {
	t.Run("mutate with empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()
		mutator := NewRegisteredDataMutator[*authorinov1beta3.AuthConfig](store)

		authConfig := &authorinov1beta3.AuthConfig{}

		targetRefs := []machinery.PolicyTargetReference{createMockGatewayTargetRef()}
		err := mutator.Mutate(authConfig, targetRefs)
		if err != nil {
			t.Errorf("Expected no error with empty store: %v", err)
		}

		if authConfig.Spec.Response != nil {
			t.Error("Expected AuthConfig to remain unmodified when store is empty")
		}
	})

	t.Run("mutate with registered data", func(t *testing.T) {
		store := NewRegisteredDataStore()
		mutator := NewRegisteredDataMutator[*authorinov1beta3.AuthConfig](store)

		entry1 := DataProviderEntry{
			Policy:     testResourceID("Extension", "ns1", "ext1"),
			Binding:    "user",
			Expression: "user.id",
			CAst:       nil,
		}
		entry2 := DataProviderEntry{
			Policy:     testResourceID("Extension", "ns1", "ext2"),
			Binding:    "role",
			Expression: "user.role",
			CAst:       nil,
		}

		mockTargetRef := createMockGatewayTargetRef()
		store.Set(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "user", entry1)
		store.Set(testResourceID("Extension", "ns1", "ext2"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "role", entry2)

		authConfig := &authorinov1beta3.AuthConfig{}

		err := mutator.Mutate(authConfig, []machinery.PolicyTargetReference{createMockGatewayTargetRef()})
		if err != nil {
			t.Errorf("Expected no error: %v", err)
		}

		if authConfig.Spec.Response == nil {
			t.Fatal("Expected Response to be set")
		}
		if authConfig.Spec.Response.Success.DynamicMetadata == nil {
			t.Fatal("Expected DynamicMetadata to be set")
		}

		kuadrantMetadata, exists := authConfig.Spec.Response.Success.DynamicMetadata["kuadrant"]
		if !exists {
			t.Fatal("Expected 'kuadrant' metadata to be set")
		}

		if kuadrantMetadata.Json == nil {
			t.Fatal("Expected Json to be set")
		}

		if len(kuadrantMetadata.Json.Properties) != 2 {
			t.Errorf("Expected 2 properties, got %d", len(kuadrantMetadata.Json.Properties))
		}

		userProp, exists := kuadrantMetadata.Json.Properties["user"]
		if !exists {
			t.Error("Expected 'user' property to exist")
		} else if string(userProp.Expression) != "user.id" {
			t.Errorf("Expected expression 'user.id', got '%s'", userProp.Expression)
		}

		roleProp, exists := kuadrantMetadata.Json.Properties["role"]
		if !exists {
			t.Error("Expected 'role' property to exist")
		} else if string(roleProp.Expression) != "user.role" {
			t.Errorf("Expected expression 'user.role', got '%s'", roleProp.Expression)
		}
	})

	t.Run("mutate with existing response config", func(t *testing.T) {
		store := NewRegisteredDataStore()
		mutator := NewRegisteredDataMutator[*authorinov1beta3.AuthConfig](store)

		entry := DataProviderEntry{
			Policy:     testResourceID("Extension", "ns1", "ext1"),
			Binding:    "custom_data",
			Expression: "custom.expression",
			CAst:       nil,
		}
		mockTargetRef := createMockGatewayTargetRef()
		store.Set(testResourceID("AuthPolicy", "test-namespace", "test-policy"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "custom_data", entry)

		// AuthConfig with existing response configuration
		authConfig := &authorinov1beta3.AuthConfig{
			Spec: authorinov1beta3.AuthConfigSpec{
				Response: &authorinov1beta3.ResponseSpec{
					Success: authorinov1beta3.WrappedSuccessResponseSpec{
						DynamicMetadata: map[string]authorinov1beta3.SuccessResponseSpec{
							"existing": {
								AuthResponseMethodSpec: authorinov1beta3.AuthResponseMethodSpec{
									Json: &authorinov1beta3.JsonAuthResponseSpec{
										Properties: map[string]authorinov1beta3.ValueOrSelector{
											"existing_prop": {
												Expression: "existing.expression",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		err := mutator.Mutate(authConfig, []machinery.PolicyTargetReference{createMockGatewayTargetRef()})
		if err != nil {
			t.Errorf("Expected no error: %v", err)
		}

		if len(authConfig.Spec.Response.Success.DynamicMetadata) != 2 {
			t.Errorf("Expected 2 metadata entries, got %d", len(authConfig.Spec.Response.Success.DynamicMetadata))
		}

		existingMetadata, exists := authConfig.Spec.Response.Success.DynamicMetadata["existing"]
		if !exists {
			t.Error("Expected existing metadata to be preserved")
		} else if len(existingMetadata.Json.Properties) != 1 {
			t.Errorf("Expected 1 existing property, got %d", len(existingMetadata.Json.Properties))
		}

		kuadrantMetadata, exists := authConfig.Spec.Response.Success.DynamicMetadata["kuadrant"]
		if !exists {
			t.Error("Expected kuadrant metadata to be added")
		} else if len(kuadrantMetadata.Json.Properties) != 1 {
			t.Errorf("Expected 1 kuadrant property, got %d", len(kuadrantMetadata.Json.Properties))
		}
	})
}

func TestMutatorRegistry(t *testing.T) {
	t.Run("register and apply mutators", func(t *testing.T) {
		registry := &MutatorRegistry{}

		mutator1Called := false
		mutator2Called := false

		mutator1 := &mockAuthConfigMutator{
			mutateFn: func(authConfig *authorinov1beta3.AuthConfig, targetRefs []machinery.PolicyTargetReference) error {
				mutator1Called = true
				return nil
			},
		}

		mutator2 := &mockAuthConfigMutator{
			mutateFn: func(authConfig *authorinov1beta3.AuthConfig, targetRefs []machinery.PolicyTargetReference) error {
				mutator2Called = true
				return nil
			},
		}

		registry.RegisterAuthConfigMutator(mutator1)
		registry.RegisterAuthConfigMutator(mutator2)

		authConfig := &authorinov1beta3.AuthConfig{}
		targetRefs := []machinery.PolicyTargetReference{createMockGatewayTargetRef()}

		err := registry.applyMutatorsWithTargetRefs(authConfig, targetRefs)
		if err != nil {
			t.Errorf("Expected no error: %v", err)
		}

		if !mutator1Called {
			t.Error("Expected mutator1 to be called")
		}
		if !mutator2Called {
			t.Error("Expected mutator2 to be called")
		}
	})

	t.Run("mutator error handling", func(t *testing.T) {
		registry := &MutatorRegistry{}

		errorMutator := &mockAuthConfigMutator{
			mutateFn: func(authConfig *authorinov1beta3.AuthConfig, targetRefs []machinery.PolicyTargetReference) error {
				return fmt.Errorf("mutator error")
			},
		}

		registry.RegisterAuthConfigMutator(errorMutator)

		authConfig := &authorinov1beta3.AuthConfig{}
		targetRefs := []machinery.PolicyTargetReference{createMockGatewayTargetRef()}

		err := registry.applyMutatorsWithTargetRefs(authConfig, targetRefs)
		if err == nil {
			t.Error("Expected error from failing mutator")
		}
		if err.Error() != "mutator error" {
			t.Errorf("Expected 'mutator error', got '%s'", err.Error())
		}
	})
}

func TestRegisteredDataStoreEdgeCases(t *testing.T) {
	t.Run("concurrent operations", func(t *testing.T) {
		store := NewRegisteredDataStore()

		var wg sync.WaitGroup
		mockTargetRef := createMockGatewayTargetRef()

		for i := range 10 {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				entry := DataProviderEntry{
					Policy:     testResourceID("TestPolicy", "ns", "policy"),
					Binding:    fmt.Sprintf("binding%d", index),
					Expression: fmt.Sprintf("expression%d", index),
					CAst:       nil,
				}
				store.Set(testResourceID("TestPolicy", "ns", "policy"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, fmt.Sprintf("binding%d", index), entry)
			}(i)
		}

		wg.Wait()

		entries := store.GetAllForTargetRef(mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH)
		if len(entries) != 10 {
			t.Errorf("Expected 10 entries, got %d", len(entries))
		}
	})

	t.Run("delete from empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()

		mockTargetRef := createMockGatewayTargetRef()
		deleted := store.Delete(testResourceID("non-existent", "ns", "name"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "non-existent")
		if deleted {
			t.Error("Expected delete to return false for non-existent entry")
		}
	})

	t.Run("get from empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()

		mockTargetRef := createMockGatewayTargetRef()
		_, exists := store.Get(testResourceID("non-existent", "ns", "name"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "non-existent")
		if exists {
			t.Error("Expected get to return false for non-existent entry")
		}

		exists = store.Exists(testResourceID("non-existent", "ns", "name"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "non-existent")
		if exists {
			t.Error("Expected exists to return false for non-existent entry")
		}
	})

	t.Run("clear empty target", func(t *testing.T) {
		store := NewRegisteredDataStore()

		cleared, _, _ := store.ClearPolicyData(testResourceID("non-existent", "ns", "name"))
		if cleared != 0 {
			t.Errorf("Expected 0 cleared entries, got %d", cleared)
		}
	})

	t.Run("set and delete maintaining map structure", func(t *testing.T) {
		store := NewRegisteredDataStore()

		entry := DataProviderEntry{
			Policy:     testResourceID("Extension", "ns1", "ext1"),
			Binding:    "binding1",
			Expression: "expression1",
			CAst:       nil,
		}

		mockTargetRef := createMockGatewayTargetRef()
		store.Set(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "binding1", entry)

		if !store.Exists(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "binding1") {
			t.Error("Expected entry to exist after setting")
		}

		deleted := store.Delete(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "binding1")
		if !deleted {
			t.Error("Expected delete to return true")
		}

		if store.Exists(testResourceID("Extension", "ns1", "ext1"), mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "binding1") {
			t.Error("Expected entry to not exist after deleting")
		}

		entries := store.GetAllForTargetRef(mockTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH)
		if len(entries) != 0 {
			t.Error("Expected no entries for cleaned up target")
		}
	})
}

func TestRegisteredDataMutatorLookup(t *testing.T) {
	t.Run("mutator lookup with HTTPRoute and Gateway", func(t *testing.T) {
		store := NewRegisteredDataStore()
		mutator := NewRegisteredDataMutator[*authorinov1beta3.AuthConfig](store)

		httpRouteEntry := DataProviderEntry{
			Policy:     testResourceID("PlanPolicy", "ns1", "plan1"),
			Binding:    "plan",
			Expression: `"premium"`,
			CAst:       nil,
		}
		httpRouteTargetRef := createMockHTTPRouteTargetRef()
		store.Set(httpRouteEntry.Policy, httpRouteTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "plan", httpRouteEntry)

		gatewayEntry := DataProviderEntry{
			Policy:     testResourceID("GlobalPolicy", "ns1", "global1"),
			Binding:    "global",
			Expression: `"enterprise"`,
			CAst:       nil,
		}
		gatewayTargetRef := createMockGatewayTargetRef()
		store.Set(gatewayEntry.Policy, gatewayTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "global", gatewayEntry)

		authConfig := &authorinov1beta3.AuthConfig{}
		targetRefs := []machinery.PolicyTargetReference{
			httpRouteTargetRef,
			gatewayTargetRef,
		}

		err := mutator.Mutate(authConfig, targetRefs)
		if err != nil {
			t.Errorf("Expected no error with mutator lookup: %v", err)
		}

		if authConfig.Spec.Response == nil {
			t.Error("Expected response spec to be set")
		}
		if authConfig.Spec.Response.Success.DynamicMetadata == nil {
			t.Error("Expected dynamic metadata to be set")
		}
		kuadrantData, exists := authConfig.Spec.Response.Success.DynamicMetadata[KuadrantDataNamespace]
		if !exists {
			t.Error("Expected kuadrant data namespace to exist")
		}
		if kuadrantData.Json == nil || kuadrantData.Json.Properties == nil {
			t.Error("Expected JSON properties to be set")
		}
		if _, exists := kuadrantData.Json.Properties["plan"]; !exists {
			t.Error("Expected 'plan' property from HTTPRoute-level policy")
		}
		if _, exists := kuadrantData.Json.Properties["global"]; !exists {
			t.Error("Expected 'global' property from Gateway-level policy")
		}
	})
}

// Mock mutator
type mockAuthConfigMutator struct {
	mutateFn func(*authorinov1beta3.AuthConfig, []machinery.PolicyTargetReference) error
}

func (m *mockAuthConfigMutator) Mutate(authConfig *authorinov1beta3.AuthConfig, targetRefs []machinery.PolicyTargetReference) error {
	return m.mutateFn(authConfig, targetRefs)
}

func createMockGatewayTargetRef() machinery.PolicyTargetReference {
	return machinery.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReferenceWithSectionName: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  "Gateway",
				Name:  "test-gateway",
			},
		},
		PolicyNamespace: "test-namespace",
	}
}

func createMockHTTPRouteTargetRef() machinery.PolicyTargetReference {
	return machinery.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReferenceWithSectionName: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  "HTTPRoute",
				Name:  "test-route",
			},
		},
		PolicyNamespace: "test-namespace",
	}
}

func TestRegisteredDataStore_SetUpstream_GetUpstream_DeleteUpstream(t *testing.T) {
	store := NewRegisteredDataStore()

	policy := testResourceID("DemoPolicy", "default", "demo-auth")
	key := RegisteredUpstreamKey{Policy: policy, URL: "grpc://my-service:8081"}
	entry := RegisteredUpstreamEntry{
		ClusterName: "ext-my-service-8081",
		Host:        "my-service",
		Port:        8081,
		TargetRef:   TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "my-api", Namespace: "default"},
		FailureMode: "deny",
		Timeout:     "100ms",
	}

	store.SetUpstream(key, entry)

	retrieved, exists := store.GetUpstream(key)
	if !exists {
		t.Fatal("Expected upstream to exist")
	}
	if retrieved.ClusterName != "ext-my-service-8081" {
		t.Errorf("Expected cluster name ext-my-service-8081, got %s", retrieved.ClusterName)
	}
	if retrieved.TargetRef.Name != "my-api" {
		t.Errorf("Expected target ref name my-api, got %s", retrieved.TargetRef.Name)
	}

	all := store.GetAllUpstreams()
	if len(all) != 1 {
		t.Errorf("Expected 1 upstream, got %d", len(all))
	}

	deleted := store.DeleteUpstream(key)
	if !deleted {
		t.Error("Expected upstream to be deleted")
	}

	_, exists = store.GetUpstream(key)
	if exists {
		t.Error("Expected upstream to not exist after deletion")
	}
}

func TestRegisteredDataStore_GetUpstreamsByTargetRef(t *testing.T) {
	store := NewRegisteredDataStore()

	targetRef1 := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route-a", Namespace: "default"}
	targetRef2 := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route-b", Namespace: "default"}

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("Policy", "default", "p1"), URL: "grpc://svc1:8081"},
		RegisteredUpstreamEntry{Host: "svc1", Port: 8081, ClusterName: "ext-svc1-8081", TargetRef: targetRef1},
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("Policy", "default", "p2"), URL: "grpc://svc2:8082"},
		RegisteredUpstreamEntry{Host: "svc2", Port: 8082, ClusterName: "ext-svc2-8082", TargetRef: targetRef1},
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("Policy", "default", "p3"), URL: "grpc://svc3:8083"},
		RegisteredUpstreamEntry{Host: "svc3", Port: 8083, ClusterName: "ext-svc3-8083", TargetRef: targetRef2},
	)

	results := store.GetUpstreamsByTargetRef(targetRef1)
	if len(results) != 2 {
		t.Errorf("Expected 2 upstreams for route-a, got %d", len(results))
	}

	results = store.GetUpstreamsByTargetRef(targetRef2)
	if len(results) != 1 {
		t.Errorf("Expected 1 upstream for route-b, got %d", len(results))
	}

	results = store.GetUpstreamsByTargetRef(TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "non-existent", Namespace: "default"})
	if len(results) != 0 {
		t.Errorf("Expected 0 upstreams for non-existent route, got %d", len(results))
	}
}

func TestRegisteredDataStore_ClearPolicyData_WithUpstreams(t *testing.T) {
	store := NewRegisteredDataStore()

	policy1 := testResourceID("DemoPolicy", "default", "demo-1")
	policy2 := testResourceID("DemoPolicy", "default", "demo-2")
	targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "my-api", Namespace: "default"}

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policy1, URL: "grpc://svc1:8081"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRef},
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policy1, URL: "grpc://svc2:8082"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc2-8082", Host: "svc2", Port: 8082, TargetRef: targetRef},
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policy2, URL: "grpc://svc3:8083"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc3-8083", Host: "svc3", Port: 8083, TargetRef: targetRef},
	)

	_, _, clearedUpstreams := store.ClearPolicyData(policy1)
	if clearedUpstreams != 2 {
		t.Errorf("Expected 2 cleared upstreams, got %d", clearedUpstreams)
	}

	all := store.GetAllUpstreams()
	if len(all) != 1 {
		t.Errorf("Expected 1 remaining upstream, got %d", len(all))
	}

	// Verify the remaining upstream belongs to policy2
	key := RegisteredUpstreamKey{Policy: policy2, URL: "grpc://svc3:8083"}
	_, exists := store.GetUpstream(key)
	if !exists {
		t.Error("Expected policy2 upstream to still exist")
	}
}

func TestRegisteredDataStore_UpstreamConcurrentOperations(t *testing.T) {
	store := NewRegisteredDataStore()
	targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "my-api", Namespace: "default"}

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := RegisteredUpstreamKey{
				Policy: testResourceID("Policy", "ns", "policy"),
				URL:    fmt.Sprintf("grpc://svc%d:8081", index),
			}
			entry := RegisteredUpstreamEntry{
				ClusterName: fmt.Sprintf("ext-svc%d-8081", index),
				Host:        fmt.Sprintf("svc%d", index),
				Port:        8081,
				TargetRef:   targetRef,
			}
			store.SetUpstream(key, entry)
		}(i)
	}
	wg.Wait()

	all := store.GetAllUpstreams()
	if len(all) != 10 {
		t.Errorf("Expected 10 upstreams, got %d", len(all))
	}
}

func TestRegisteredDataStore_UpstreamEdgeCases(t *testing.T) {
	t.Run("get from empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()
		key := RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns", "name"), URL: "grpc://svc:8081"}
		_, exists := store.GetUpstream(key)
		if exists {
			t.Error("Expected get to return false for non-existent upstream")
		}
	})

	t.Run("delete from empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()
		key := RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns", "name"), URL: "grpc://svc:8081"}
		deleted := store.DeleteUpstream(key)
		if deleted {
			t.Error("Expected delete to return false for non-existent upstream")
		}
	})

	t.Run("overwrite existing upstream", func(t *testing.T) {
		store := NewRegisteredDataStore()
		key := RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns", "p1"), URL: "grpc://svc:8081"}
		targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route", Namespace: "ns"}

		store.SetUpstream(key, RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: targetRef, Timeout: "100ms"})
		store.SetUpstream(key, RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: targetRef, Timeout: "200ms"})

		entry, _ := store.GetUpstream(key)
		if entry.Timeout != "200ms" {
			t.Errorf("Expected overwritten timeout 200ms, got %s", entry.Timeout)
		}

		all := store.GetAllUpstreams()
		if len(all) != 1 {
			t.Errorf("Expected 1 upstream after overwrite, got %d", len(all))
		}
	})
}

func TestHashUpstreamServiceConfig(t *testing.T) {
	timeout := "100ms"
	svc := wasm.Service{
		Endpoint:    "ext-my-service-8081",
		Type:        wasm.AuthServiceType,
		FailureMode: wasm.FailureModeDeny,
		Timeout:     &timeout,
	}

	hash1 := HashUpstreamServiceConfig(svc)
	hash2 := HashUpstreamServiceConfig(svc)
	if hash1 != hash2 {
		t.Errorf("Expected identical hashes for identical configs, got %s and %s", hash1, hash2)
	}

	// Different endpoint should produce a different hash
	svc2 := svc
	svc2.Endpoint = "ext-other-service-8082"
	hash3 := HashUpstreamServiceConfig(svc2)
	if hash1 == hash3 {
		t.Errorf("Expected different hashes for different endpoints")
	}

	// Different timeout should produce a different hash
	timeout2 := "200ms"
	svc3 := svc
	svc3.Timeout = &timeout2
	hash4 := HashUpstreamServiceConfig(svc3)
	if hash1 == hash4 {
		t.Errorf("Expected different hashes for different timeouts")
	}

	// Nil timeout
	svc4 := svc
	svc4.Timeout = nil
	hash5 := HashUpstreamServiceConfig(svc4)
	if hash1 == hash5 {
		t.Errorf("Expected different hash for nil timeout")
	}
}

func TestMutateWasmConfig_InjectsUpstreams(t *testing.T) {
	store := NewRegisteredDataStore()
	mockTargetRef := createMockGatewayTargetRef()
	targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: mockTargetRef.GetName(), Namespace: mockTargetRef.GetNamespace()}

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo-1"), URL: "grpc://svc1:8081"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms"},
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo-2"), URL: "grpc://svc2:8082"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc2-8082", Host: "svc2", Port: 8082, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms"},
	)

	mutator := NewRegisteredDataMutator[*wasm.Config](store)
	wasmConfig := &wasm.Config{
		Services: make(map[string]wasm.Service),
	}

	err := mutator.Mutate(wasmConfig, []machinery.PolicyTargetReference{mockTargetRef})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(wasmConfig.Services) != 2 {
		t.Errorf("Expected 2 services in wasm config, got %d", len(wasmConfig.Services))
	}

	// Verify all services have ext- prefix keys
	for key, svc := range wasmConfig.Services {
		if key[:4] != "ext-" {
			t.Errorf("Expected service key to start with ext-, got %s", key)
		}
		if svc.Type != wasm.AuthServiceType {
			t.Errorf("Expected auth service type, got %s", svc.Type)
		}
		if svc.FailureMode != wasm.FailureModeDeny {
			t.Errorf("Expected deny failure mode, got %s", svc.FailureMode)
		}
	}
}

func TestGetRegisteredUpstreamsByTargetRef(t *testing.T) {
	// Save and restore GlobalMutatorRegistry
	originalRegistry := GlobalMutatorRegistry
	defer func() { GlobalMutatorRegistry = originalRegistry }()

	targetRefA := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "gw-a", Namespace: "ns1"}
	targetRefB := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "gw-b", Namespace: "ns2"}

	t.Run("empty registry", func(t *testing.T) {
		GlobalMutatorRegistry = &MutatorRegistry{}

		result := GetRegisteredUpstreamsByTargetRef(targetRefA)
		if len(result) != 0 {
			t.Errorf("Expected 0 upstreams from empty registry, got %d", len(result))
		}
	})

	t.Run("single store filters by targetRef", func(t *testing.T) {
		GlobalMutatorRegistry = &MutatorRegistry{}

		store := NewRegisteredDataStore()
		store.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns1", "p1"), URL: "grpc://svc1:8081"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRefA},
		)
		store.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns2", "p2"), URL: "grpc://svc2:8082"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc2-8082", Host: "svc2", Port: 8082, TargetRef: targetRefB},
		)
		GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

		result := GetRegisteredUpstreamsByTargetRef(targetRefA)
		if len(result) != 1 {
			t.Fatalf("Expected 1 upstream for gw-a, got %d", len(result))
		}
		if result[0].ClusterName != "ext-svc1-8081" {
			t.Errorf("Expected cluster name ext-svc1-8081, got %s", result[0].ClusterName)
		}
	})

	t.Run("multiple stores aggregate matching upstreams", func(t *testing.T) {
		GlobalMutatorRegistry = &MutatorRegistry{}

		store1 := NewRegisteredDataStore()
		store1.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("PolicyA", "ns1", "p1"), URL: "grpc://svc1:8081"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRefA},
		)

		store2 := NewRegisteredDataStore()
		store2.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("PolicyB", "ns1", "p2"), URL: "grpc://svc2:8082"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc2-8082", Host: "svc2", Port: 8082, TargetRef: targetRefA},
		)
		store2.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("PolicyC", "ns2", "p3"), URL: "grpc://svc3:8083"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc3-8083", Host: "svc3", Port: 8083, TargetRef: targetRefB},
		)

		GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store1))
		GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store2))

		result := GetRegisteredUpstreamsByTargetRef(targetRefA)
		if len(result) != 2 {
			t.Fatalf("Expected 2 upstreams for gw-a across stores, got %d", len(result))
		}

		result = GetRegisteredUpstreamsByTargetRef(targetRefB)
		if len(result) != 1 {
			t.Fatalf("Expected 1 upstream for gw-b, got %d", len(result))
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		GlobalMutatorRegistry = &MutatorRegistry{}

		store := NewRegisteredDataStore()
		store.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns1", "p1"), URL: "grpc://svc1:8081"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRefA},
		)
		GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

		result := GetRegisteredUpstreamsByTargetRef(targetRefB)
		if len(result) != 0 {
			t.Errorf("Expected 0 upstreams for non-matching ref, got %d", len(result))
		}
	})

	t.Run("skips non-RegisteredDataMutator wasm mutators", func(t *testing.T) {
		GlobalMutatorRegistry = &MutatorRegistry{}

		mockMutator := &mockWasmConfigMutator{
			mutateFn: func(_ *wasm.Config, _ []machinery.PolicyTargetReference) error { return nil },
		}
		GlobalMutatorRegistry.RegisterWasmConfigMutator(mockMutator)

		store := NewRegisteredDataStore()
		store.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns", "p1"), URL: "grpc://svc:8081"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: targetRefA},
		)
		GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

		result := GetRegisteredUpstreamsByTargetRef(targetRefA)
		if len(result) != 1 {
			t.Errorf("Expected 1 upstream (skipping mock mutator), got %d", len(result))
		}
	})
}

// Mock wasm config mutator (non-RegisteredDataMutator)
type mockWasmConfigMutator struct {
	mutateFn func(*wasm.Config, []machinery.PolicyTargetReference) error
}

func (m *mockWasmConfigMutator) Mutate(config *wasm.Config, targetRefs []machinery.PolicyTargetReference) error {
	return m.mutateFn(config, targetRefs)
}

func TestMutateWasmConfig_DeduplicatesIdenticalUpstreams(t *testing.T) {
	store := NewRegisteredDataStore()
	mockTargetRef := createMockGatewayTargetRef()
	targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: mockTargetRef.GetName(), Namespace: mockTargetRef.GetNamespace()}

	// Two policies registering the same URL → same cluster name → same service config → same hash
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo-1"), URL: "grpc://svc1:8081"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms"},
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo-2"), URL: "grpc://svc1:8081"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms"},
	)

	mutator := NewRegisteredDataMutator[*wasm.Config](store)
	wasmConfig := &wasm.Config{
		Services: make(map[string]wasm.Service),
	}

	err := mutator.Mutate(wasmConfig, []machinery.PolicyTargetReference{mockTargetRef})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Same service config → same hash key → deduplicated to 1
	if len(wasmConfig.Services) != 1 {
		t.Errorf("Expected 1 deduplicated service, got %d", len(wasmConfig.Services))
	}
}
