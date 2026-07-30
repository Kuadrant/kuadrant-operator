//go:build unit

package extension

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/samber/lo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"k8s.io/utils/ptr"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	"github.com/kuadrant/policy-machinery/machinery"
)

func testResourceID(kind, namespace, name string) ResourceID {
	return ResourceID{Kind: kind, Namespace: namespace, Name: name}
}

func testFileDescriptorSet() *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: proto.String("test.proto")},
		},
	}
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

	clearedMutators, clearedSubscriptions, _, clearedPipelineActions := store.ClearPolicyData(testPolicy)

	if clearedMutators != 2 {
		t.Errorf("Expected 2 cleared mutators, got %d", clearedMutators)
	}
	if clearedSubscriptions != 1 {
		t.Errorf("Expected 1 cleared subscription, got %d", clearedSubscriptions)
	}
	if clearedPipelineActions != 0 {
		t.Errorf("Expected 0 cleared pipeline actions, got %d", clearedPipelineActions)
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

	store.ClearPolicyData(testResourceID("Extension", "ns1", "ext1")) //nolint:dogsled,exhaustruct

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

	_, cleared, _, _ := store.ClearPolicyData(testResourceID("AuthPolicy", "test-ns", "test-policy"))
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

	_, cleared, _, _ = store.ClearPolicyData(testResourceID("AuthPolicy", "non-existent", "policy"))
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

		cleared, _, _, _ := store.ClearPolicyData(testResourceID("non-existent", "ns", "name"))
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

	t.Run("mutator lookup with GRPCRoute and Gateway", func(t *testing.T) {
		store := NewRegisteredDataStore()
		mutator := NewRegisteredDataMutator[*authorinov1beta3.AuthConfig](store)

		grpcRouteEntry := DataProviderEntry{
			Policy:     testResourceID("PlanPolicy", "ns1", "plan1"),
			Binding:    "plan",
			Expression: `"premium"`,
			CAst:       nil,
		}
		grpcRouteTargetRef := createMockGRPCRouteTargetRef()
		store.Set(grpcRouteEntry.Policy, grpcRouteTargetRef.GetLocator(), extpb.Domain_DOMAIN_AUTH, "plan", grpcRouteEntry)

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
			grpcRouteTargetRef,
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
			t.Error("Expected 'plan' property from GRPCRoute-level policy")
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

func createMockGRPCRouteTargetRef() machinery.PolicyTargetReference {
	return machinery.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReferenceWithSectionName: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  "GRPCRoute",
				Name:  "test-grpc-route",
			},
		},
		PolicyNamespace: "test-namespace",
	}
}

func TestRegisteredDataStore_SetUpstream_GetUpstream_DeleteUpstream(t *testing.T) {
	store := NewRegisteredDataStore()

	policy := testResourceID("DemoPolicy", "default", "demo-auth")
	key := RegisteredUpstreamKey{Policy: policy, URL: "grpc://my-service:8081", Service: "test.Service", Method: "TestMethod"}
	entry := RegisteredUpstreamEntry{
		ClusterName: "ext-my-service-8081",
		Host:        "my-service",
		Port:        8081,
		Service:     "test.Service",
		Method:      "TestMethod",
		TargetRef:   TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "my-api", Namespace: "default"},
		FailureMode: "deny",
		Timeout:     "100ms",
	}

	store.SetUpstream(key, entry, testFileDescriptorSet())

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
		RegisteredUpstreamKey{Policy: testResourceID("Policy", "default", "p1"), URL: "grpc://svc1:8081", Service: "test.Service", Method: "Method1"},
		RegisteredUpstreamEntry{Host: "svc1", Port: 8081, ClusterName: "ext-svc1-8081", TargetRef: targetRef1, Service: "test.Service", Method: "Method1"},
		testFileDescriptorSet(),
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("Policy", "default", "p2"), URL: "grpc://svc2:8082", Service: "test.Service", Method: "Method2"},
		RegisteredUpstreamEntry{Host: "svc2", Port: 8082, ClusterName: "ext-svc2-8082", TargetRef: targetRef1, Service: "test.Service", Method: "Method2"},
		testFileDescriptorSet(),
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("Policy", "default", "p3"), URL: "grpc://svc3:8083", Service: "test.Service", Method: "Method3"},
		RegisteredUpstreamEntry{Host: "svc3", Port: 8083, ClusterName: "ext-svc3-8083", TargetRef: targetRef2, Service: "test.Service", Method: "Method3"},
		testFileDescriptorSet(),
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

	fds1a := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{Name: proto.String("svc1.proto")}},
	}
	fds1b := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{Name: proto.String("svc2.proto")}},
	}
	fds2 := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{Name: proto.String("svc3.proto")}},
	}

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policy1, Name: "method-a", URL: "grpc://svc1:8081", Service: "test.ServiceA", Method: "MethodA"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRef, Service: "test.ServiceA", Method: "MethodA"},
		fds1a,
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policy1, Name: "method-b", URL: "grpc://svc2:8082", Service: "test.ServiceB", Method: "MethodB"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc2-8082", Host: "svc2", Port: 8082, TargetRef: targetRef, Service: "test.ServiceB", Method: "MethodB"},
		fds1b,
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policy2, Name: "method-c", URL: "grpc://svc3:8083", Service: "test.ServiceC", Method: "MethodC"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc3-8083", Host: "svc3", Port: 8083, TargetRef: targetRef, Service: "test.ServiceC", Method: "MethodC"},
		fds2,
	)

	cacheKey1a := ProtoCacheKey{ClusterName: "ext-svc1-8081", Service: "test.ServiceA"}
	cacheKey1b := ProtoCacheKey{ClusterName: "ext-svc2-8082", Service: "test.ServiceB"}
	cacheKey2 := ProtoCacheKey{ClusterName: "ext-svc3-8083", Service: "test.ServiceC"}

	_, _, clearedUpstreams, _ := store.ClearPolicyData(policy1)
	if clearedUpstreams != 2 {
		t.Errorf("Expected 2 cleared upstreams, got %d", clearedUpstreams)
	}

	if upstreams := store.GetUpstreamsForPolicy(policy1); len(upstreams) != 0 {
		t.Errorf("Expected no upstreams for policy1 after clear, got %d", len(upstreams))
	}

	if _, exists := store.GetProtoDescriptor(cacheKey1a); exists {
		t.Error("Expected proto descriptor for policy1 ServiceA to be removed")
	}
	if _, exists := store.GetProtoDescriptor(cacheKey1b); exists {
		t.Error("Expected proto descriptor for policy1 ServiceB to be removed")
	}

	if upstreams := store.GetUpstreamsForPolicy(policy2); len(upstreams) != 1 {
		t.Errorf("Expected 1 upstream for policy2, got %d", len(upstreams))
	}
	if _, exists := store.GetProtoDescriptor(cacheKey2); !exists {
		t.Error("Expected proto descriptor for policy2 to still exist")
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
				Policy:  testResourceID("Policy", "ns", "policy"),
				URL:     fmt.Sprintf("grpc://svc%d:8081", index),
				Service: "test.Service",
				Method:  fmt.Sprintf("Method%d", index),
			}
			entry := RegisteredUpstreamEntry{
				ClusterName: fmt.Sprintf("ext-svc%d-8081", index),
				Host:        fmt.Sprintf("svc%d", index),
				Port:        8081,
				Service:     "test.Service",
				Method:      fmt.Sprintf("Method%d", index),
				TargetRef:   targetRef,
			}
			store.SetUpstream(key, entry, testFileDescriptorSet())
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
		key := RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns", "name"), URL: "grpc://svc:8081", Service: "test.Service", Method: "TestMethod"}
		_, exists := store.GetUpstream(key)
		if exists {
			t.Error("Expected get to return false for non-existent upstream")
		}
	})

	t.Run("delete from empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()
		key := RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns", "name"), URL: "grpc://svc:8081", Service: "test.Service", Method: "TestMethod"}
		deleted := store.DeleteUpstream(key)
		if deleted {
			t.Error("Expected delete to return false for non-existent upstream")
		}
	})

	t.Run("overwrite existing upstream", func(t *testing.T) {
		store := NewRegisteredDataStore()
		key := RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns", "p1"), URL: "grpc://svc:8081", Service: "test.Service", Method: "TestMethod"}
		targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route", Namespace: "ns"}

		store.SetUpstream(key, RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, Service: "test.Service", Method: "TestMethod", TargetRef: targetRef, Timeout: "100ms"}, testFileDescriptorSet())
		store.SetUpstream(key, RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, Service: "test.Service", Method: "TestMethod", TargetRef: targetRef, Timeout: "200ms"}, testFileDescriptorSet())

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

	// Track the upstream entries we register for verification
	upstreamEntries := []RegisteredUpstreamEntry{
		{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms", Service: "test.Service", Method: "Method1"},
		{ClusterName: "ext-svc2-8082", Host: "svc2", Port: 8082, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms", Service: "test.Service", Method: "Method2"},
	}

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo-1"), URL: "grpc://svc1:8081", Service: "test.Service", Method: "Method1"},
		upstreamEntries[0],
		testFileDescriptorSet(),
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo-2"), URL: "grpc://svc2:8082", Service: "test.Service", Method: "Method2"},
		upstreamEntries[1],
		testFileDescriptorSet(),
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

	// Verify descriptor service is set to the expected value
	if wasmConfig.DescriptorService != "kuadrant-operator-grpc" {
		t.Errorf("Expected DescriptorService to be 'kuadrant-operator-grpc', got %q", wasmConfig.DescriptorService)
	}

	// Build a map of cluster names to upstream entries for easy lookup
	upstreamsByCluster := make(map[string]RegisteredUpstreamEntry)
	for _, entry := range upstreamEntries {
		upstreamsByCluster[entry.ClusterName] = entry
	}

	// Verify each wasm service matches its corresponding upstream entry
	for key, svc := range wasmConfig.Services {
		if len(key) < 4 || key[:4] != "ext-" {
			t.Errorf("Expected service key to start with ext-, got %s", key)
			continue
		}

		// Verify service type
		if svc.Type != wasm.DynamicServiceType {
			t.Errorf("Service %s: expected dynamic service type, got %s", key, svc.Type)
		}

		// Find the corresponding upstream entry by endpoint (cluster name)
		upstream, found := upstreamsByCluster[svc.Endpoint]
		if !found {
			t.Errorf("Service %s: endpoint %s does not match any registered upstream cluster", key, svc.Endpoint)
			continue
		}

		// Verify endpoint matches cluster name
		if svc.Endpoint != upstream.ClusterName {
			t.Errorf("Service %s: expected endpoint %s, got %s", key, upstream.ClusterName, svc.Endpoint)
		}

		// Verify failure mode
		if string(svc.FailureMode) != upstream.FailureMode {
			t.Errorf("Service %s: expected failure mode %s, got %s", key, upstream.FailureMode, svc.FailureMode)
		}

		// Verify timeout
		if svc.Timeout == nil {
			t.Errorf("Service %s: expected timeout to be set", key)
		} else if *svc.Timeout != upstream.Timeout {
			t.Errorf("Service %s: expected timeout %s, got %s", key, upstream.Timeout, *svc.Timeout)
		}

		// Verify gRPC service matches upstream entry
		if svc.GrpcService == nil {
			t.Errorf("Service %s: expected GrpcService to be set", key)
		} else if *svc.GrpcService != upstream.Service {
			t.Errorf("Service %s: expected GrpcService %s, got %s", key, upstream.Service, *svc.GrpcService)
		}

		// Verify gRPC method matches upstream entry
		if svc.GrpcMethod == nil {
			t.Errorf("Service %s: expected GrpcMethod to be set", key)
		} else if *svc.GrpcMethod != upstream.Method {
			t.Errorf("Service %s: expected GrpcMethod %s, got %s", key, upstream.Method, *svc.GrpcMethod)
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
			RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns1", "p1"), URL: "grpc://svc1:8081", Service: "test.Service", Method: "Method1"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, TargetRef: targetRefA, Service: "test.Service", Method: "Method1"},
			testFileDescriptorSet(),
		)
		store.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns2", "p2"), URL: "grpc://svc2:8082", Service: "test.Service", Method: "Method2"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc2-8082", Host: "svc2", Port: 8082, TargetRef: targetRefB, Service: "test.Service", Method: "Method2"},
			testFileDescriptorSet(),
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
			RegisteredUpstreamKey{Policy: testResourceID("PolicyA", "ns1", "p1"), URL: "grpc://svc1:8081", Service: "test.Service", Method: "MethodA"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, Service: "test.Service", Method: "MethodA", TargetRef: targetRefA},
			testFileDescriptorSet(),
		)

		store2 := NewRegisteredDataStore()
		store2.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("PolicyB", "ns1", "p2"), URL: "grpc://svc2:8082", Service: "test.Service", Method: "MethodB"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc2-8082", Host: "svc2", Port: 8082, Service: "test.Service", Method: "MethodB", TargetRef: targetRefA},
			testFileDescriptorSet(),
		)
		store2.SetUpstream(
			RegisteredUpstreamKey{Policy: testResourceID("PolicyC", "ns2", "p3"), URL: "grpc://svc3:8083", Service: "test.Service", Method: "MethodC"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc3-8083", Host: "svc3", Port: 8083, Service: "test.Service", Method: "MethodC", TargetRef: targetRefB},
			testFileDescriptorSet(),
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
			RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns1", "p1"), URL: "grpc://svc1:8081", Service: "test.Service", Method: "TestMethod"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, Service: "test.Service", Method: "TestMethod", TargetRef: targetRefA},
			testFileDescriptorSet(),
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
			RegisteredUpstreamKey{Policy: testResourceID("Policy", "ns", "p1"), URL: "grpc://svc:8081", Service: "test.Service", Method: "TestMethod"},
			RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, Service: "test.Service", Method: "TestMethod", TargetRef: targetRefA},
			testFileDescriptorSet(),
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

func TestPipelineActionStore_AppendAndGet(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	actions := []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "checkThreat", Var: "threatResponse"},
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, WithStatus: 403},
	}

	startIdx := store.AppendPipelineActions(policy, PipelinePhaseRequest, actions)
	if startIdx != 0 {
		t.Errorf("Expected start index 0, got %d", startIdx)
	}

	retrieved := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if len(retrieved) != 2 {
		t.Fatalf("Expected 2 actions, got %d", len(retrieved))
	}
	if retrieved[0].Index != 0 {
		t.Errorf("First action index = %d, want 0", retrieved[0].Index)
	}
	if retrieved[1].Index != 1 {
		t.Errorf("Second action index = %d, want 1", retrieved[1].Index)
	}
	if retrieved[0].Method != "checkThreat" {
		t.Errorf("First action method = %q, want %q", retrieved[0].Method, "checkThreat")
	}
	if retrieved[1].ActionType != extpb.ActionType_ACTION_TYPE_DENY {
		t.Errorf("Second action type = %v, want DENY", retrieved[1].ActionType)
	}
}

func TestPipelineActionStore_SequentialAppends(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	// First append
	startIdx := store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "check1"},
	})
	if startIdx != 0 {
		t.Errorf("First append start index = %d, want 0", startIdx)
	}

	// Second append continues index sequence
	startIdx = store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, WithStatus: 403},
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "check2"},
	})
	if startIdx != 1 {
		t.Errorf("Second append start index = %d, want 1", startIdx)
	}

	all := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if len(all) != 3 {
		t.Fatalf("Expected 3 total actions, got %d", len(all))
	}
	for i, a := range all {
		if a.Index != i {
			t.Errorf("Action %d has index %d, want %d", i, a.Index, i)
		}
	}
}

func TestPipelineActionStore_SeparatePhases(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "check"},
	})
	store.AppendPipelineActions(policy, PipelinePhaseResponse, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, HeadersToAdd: "{'x-checked': 'true'}"},
		{ActionType: extpb.ActionType_ACTION_TYPE_FAIL, LogMessage: "blocked"},
	})

	reqActions := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if len(reqActions) != 1 {
		t.Errorf("Expected 1 request action, got %d", len(reqActions))
	}

	respActions := store.GetPipelineActions(policy, PipelinePhaseResponse)
	if len(respActions) != 2 {
		t.Errorf("Expected 2 response actions, got %d", len(respActions))
	}
	// Response phase has its own index sequence
	if respActions[0].Index != 0 {
		t.Errorf("First response action index = %d, want 0", respActions[0].Index)
	}
}

func TestPipelineActionStore_SeparatePolicies(t *testing.T) {
	store := NewRegisteredDataStore()
	policy1 := testResourceID("ThreatPolicy", "default", "policy-1")
	policy2 := testResourceID("ThreatPolicy", "default", "policy-2")

	store.AppendPipelineActions(policy1, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "check1"},
	})
	store.AppendPipelineActions(policy2, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, WithStatus: 403},
	})

	p1Actions := store.GetPipelineActions(policy1, PipelinePhaseRequest)
	p2Actions := store.GetPipelineActions(policy2, PipelinePhaseRequest)

	if len(p1Actions) != 1 || len(p2Actions) != 1 {
		t.Fatalf("Expected 1 action each, got %d and %d", len(p1Actions), len(p2Actions))
	}
	if p1Actions[0].Method != "check1" {
		t.Errorf("Policy1 action method = %q, want %q", p1Actions[0].Method, "check1")
	}
	if p2Actions[0].ActionType != extpb.ActionType_ACTION_TYPE_DENY {
		t.Errorf("Policy2 action type = %v, want DENY", p2Actions[0].ActionType)
	}
}

func TestPipelineActionStore_ClearPipelineActions(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")
	otherPolicy := testResourceID("ThreatPolicy", "default", "other-policy")

	store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "check"},
	})
	store.AppendPipelineActions(policy, PipelinePhaseResponse, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, HeadersToAdd: "{'x': '1'}"},
	})
	store.AppendPipelineActions(otherPolicy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, WithStatus: 403},
	})

	cleared := store.ClearPipelineActions(policy)
	if cleared != 2 {
		t.Errorf("Expected 2 cleared actions, got %d", cleared)
	}

	if actions := store.GetPipelineActions(policy, PipelinePhaseRequest); actions != nil {
		t.Errorf("Expected nil request actions after clear, got %v", actions)
	}
	if actions := store.GetPipelineActions(policy, PipelinePhaseResponse); actions != nil {
		t.Errorf("Expected nil response actions after clear, got %v", actions)
	}

	// Other policy unaffected
	otherActions := store.GetPipelineActions(otherPolicy, PipelinePhaseRequest)
	if len(otherActions) != 1 {
		t.Errorf("Expected other policy actions to remain, got %d", len(otherActions))
	}
}

func TestPipelineActionStore_ClearPolicyDataIncludesPipeline(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "check"},
	})

	_, _, _, clearedPipelineActions := store.ClearPolicyData(policy)

	if clearedPipelineActions != 1 {
		t.Errorf("Expected 1 cleared pipeline action, got %d", clearedPipelineActions)
	}
	if actions := store.GetPipelineActions(policy, PipelinePhaseRequest); actions != nil {
		t.Errorf("Expected pipeline actions to be cleared by ClearPolicyData, got %v", actions)
	}
}

func TestPipelineActionStore_CounterResetsAfterClear(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD},
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, WithStatus: 403},
	})

	store.ClearPipelineActions(policy)

	// After clear, index should restart at 0
	startIdx := store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "new"},
	})
	if startIdx != 0 {
		t.Errorf("Expected start index 0 after clear, got %d", startIdx)
	}

	actions := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if len(actions) != 1 {
		t.Fatalf("Expected 1 action after re-append, got %d", len(actions))
	}
	if actions[0].Index != 0 {
		t.Errorf("Action index = %d, want 0", actions[0].Index)
	}
}

func TestPipelineActionStore_GetFromEmptyStore(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	actions := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if actions != nil {
		t.Errorf("Expected nil from empty store, got %v", actions)
	}
}

func TestPipelineActionStore_ConcurrentAppends(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
				{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: fmt.Sprintf("method-%d", index)},
			})
		}(i)
	}
	wg.Wait()

	actions := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if len(actions) != 10 {
		t.Errorf("Expected 10 actions after concurrent appends, got %d", len(actions))
	}

	// Verify all indices are unique
	seen := make(map[int]bool)
	for _, a := range actions {
		if seen[a.Index] {
			t.Errorf("Duplicate index %d found", a.Index)
		}
		seen[a.Index] = true
	}
}

func TestPipelineActionStore_PredicatePreserved(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{
			ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD,
			Predicate:  "request.headers['check'] == '1' && request.method == 'GET'",
			Method:     "checkThreat",
			Var:        "threatResponse",
		},
	})

	actions := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if actions[0].Predicate != "request.headers['check'] == '1' && request.method == 'GET'" {
		t.Errorf("Predicate = %q, unexpected", actions[0].Predicate)
	}
}

func TestPipelineActionStore_GetReturnsCopy(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "original"},
	})

	// Mutating the returned slice should not affect the store
	retrieved := store.GetPipelineActions(policy, PipelinePhaseRequest)
	retrieved[0].Method = "mutated"

	original := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if original[0].Method != "original" {
		t.Errorf("Store was mutated through returned slice, method = %q", original[0].Method)
	}
}

func TestMutateWasmConfig_DeduplicatesIdenticalUpstreams(t *testing.T) {
	store := NewRegisteredDataStore()
	mockTargetRef := createMockGatewayTargetRef()
	targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: mockTargetRef.GetName(), Namespace: mockTargetRef.GetNamespace()}

	// Two policies registering the same URL/Service/Method → same cluster name → same service config → same hash
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo-1"), URL: "grpc://svc1:8081", Service: "test.Service", Method: "TestMethod"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, Service: "test.Service", Method: "TestMethod", TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms"},
		testFileDescriptorSet(),
	)
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo-2"), URL: "grpc://svc1:8081", Service: "test.Service", Method: "TestMethod"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc1-8081", Host: "svc1", Port: 8081, Service: "test.Service", Method: "TestMethod", TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms"},
		testFileDescriptorSet(),
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

func TestCollectRouteUpstreams(t *testing.T) {
	// Build a topology with listeners between gateways and routes (matching real operator topology)
	buildTopology := func(gateways []*gwapiv1.Gateway, httpRoutes []*gwapiv1.HTTPRoute, grpcRoutes []*gwapiv1.GRPCRoute) *machinery.Topology {
		mGateways := lo.Map(gateways, func(g *gwapiv1.Gateway, _ int) *machinery.Gateway {
			return &machinery.Gateway{Gateway: g}
		})
		listeners := lo.FlatMap(mGateways, machinery.ListenersFromGatewayFunc)
		mHTTPRoutes := lo.Map(httpRoutes, func(r *gwapiv1.HTTPRoute, _ int) *machinery.HTTPRoute {
			return &machinery.HTTPRoute{HTTPRoute: r}
		})
		mGRPCRoutes := lo.Map(grpcRoutes, func(r *gwapiv1.GRPCRoute, _ int) *machinery.GRPCRoute {
			return &machinery.GRPCRoute{GRPCRoute: r}
		})

		topology, err := machinery.NewTopology(
			machinery.WithTargetables(mGateways...),
			machinery.WithTargetables(listeners...),
			machinery.WithTargetables(mHTTPRoutes...),
			machinery.WithTargetables(mGRPCRoutes...),
			machinery.WithLinks(
				machinery.LinkGatewayToListenerFunc(),
				machinery.LinkListenerToHTTPRouteFunc(mGateways, listeners),
				machinery.LinkListenerToGRPCRouteFunc(mGateways, listeners),
			),
		)
		if err != nil {
			t.Fatalf("Failed to build topology: %v", err)
		}
		return topology
	}

	// Helper to find a gateway in the topology
	findGateway := func(topology *machinery.Topology, name string) *machinery.Gateway {
		for _, obj := range topology.Targetables().Items() {
			if gw, ok := obj.(*machinery.Gateway); ok && gw.GetName() == name {
				return gw
			}
		}
		t.Fatalf("Gateway %s not found in topology", name)
		return nil
	}

	tests := []struct {
		name          string
		gateways      []*gwapiv1.Gateway
		httpRoutes    []*gwapiv1.HTTPRoute
		grpcRoutes    []*gwapiv1.GRPCRoute
		upstreams     map[TargetRef]RegisteredUpstreamEntry
		gatewayName   string
		expectedCount int
		expectedNames []string
	}{
		{
			name: "collects HTTPRoute upstreams",
			gateways: []*gwapiv1.Gateway{
				BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-1" }),
			},
			httpRoutes: []*gwapiv1.HTTPRoute{
				BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
					r.Name = "http-route-1"
					r.Spec.ParentRefs[0].Name = "gw-1"
				}),
			},
			upstreams: map[TargetRef]RegisteredUpstreamEntry{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "http-route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-http-svc", Host: "svc1", Port: 8081,
				},
			},
			gatewayName:   "gw-1",
			expectedCount: 1,
			expectedNames: []string{"ext-http-svc"},
		},
		{
			name: "collects GRPCRoute upstreams",
			gateways: []*gwapiv1.Gateway{
				BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-1" }),
			},
			grpcRoutes: []*gwapiv1.GRPCRoute{
				BuildGRPCRoute(func(r *gwapiv1.GRPCRoute) {
					r.Name = "grpc-route-1"
					r.Spec.ParentRefs[0].Name = "gw-1"
				}),
			},
			upstreams: map[TargetRef]RegisteredUpstreamEntry{
				{Group: "gateway.networking.k8s.io", Kind: "GRPCRoute", Name: "grpc-route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-grpc-svc", Host: "svc2", Port: 8082,
				},
			},
			gatewayName:   "gw-1",
			expectedCount: 1,
			expectedNames: []string{"ext-grpc-svc"},
		},
		{
			name: "collects both HTTPRoute and GRPCRoute upstreams",
			gateways: []*gwapiv1.Gateway{
				BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-1" }),
			},
			httpRoutes: []*gwapiv1.HTTPRoute{
				BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
					r.Name = "http-route-1"
					r.Spec.ParentRefs[0].Name = "gw-1"
				}),
			},
			grpcRoutes: []*gwapiv1.GRPCRoute{
				BuildGRPCRoute(func(r *gwapiv1.GRPCRoute) {
					r.Name = "grpc-route-1"
					r.Spec.ParentRefs[0].Name = "gw-1"
				}),
			},
			upstreams: map[TargetRef]RegisteredUpstreamEntry{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "http-route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-http-svc", Host: "svc1", Port: 8081,
				},
				{Group: "gateway.networking.k8s.io", Kind: "GRPCRoute", Name: "grpc-route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-grpc-svc", Host: "svc2", Port: 8082,
				},
			},
			gatewayName:   "gw-1",
			expectedCount: 2,
			expectedNames: []string{"ext-http-svc", "ext-grpc-svc"},
		},
		{
			name: "no upstreams for gateway without routes",
			gateways: []*gwapiv1.Gateway{
				BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-1" }),
				BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-2" }),
			},
			httpRoutes: []*gwapiv1.HTTPRoute{
				BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
					r.Name = "http-route-1"
					r.Spec.ParentRefs[0].Name = "gw-1"
				}),
			},
			upstreams: map[TargetRef]RegisteredUpstreamEntry{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "http-route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-http-svc", Host: "svc1", Port: 8081,
				},
			},
			gatewayName:   "gw-2",
			expectedCount: 0,
		},
		{
			name: "deduplicates routes on multiple listeners",
			gateways: []*gwapiv1.Gateway{
				BuildGateway(func(g *gwapiv1.Gateway) {
					g.Name = "gw-1"
					g.Spec.Listeners = append(g.Spec.Listeners, gwapiv1.Listener{
						Name:     "listener-2",
						Port:     443,
						Protocol: "HTTPS",
					})
				}),
			},
			httpRoutes: []*gwapiv1.HTTPRoute{
				BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
					r.Name = "http-route-1"
					r.Spec.ParentRefs = []gwapiv1.ParentReference{
						{Name: "gw-1", SectionName: ptr.To(gwapiv1.SectionName("my-listener"))},
						{Name: "gw-1", SectionName: ptr.To(gwapiv1.SectionName("listener-2"))},
					}
				}),
			},
			upstreams: map[TargetRef]RegisteredUpstreamEntry{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "http-route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-http-svc", Host: "svc1", Port: 8081,
				},
			},
			gatewayName:   "gw-1",
			expectedCount: 1,
			expectedNames: []string{"ext-http-svc"},
		},
		{
			name: "same-name routes of different kinds are collected separately",
			gateways: []*gwapiv1.Gateway{
				BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-1" }),
			},
			httpRoutes: []*gwapiv1.HTTPRoute{
				BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
					r.Name = "route-1"
					r.Spec.ParentRefs[0].Name = "gw-1"
				}),
			},
			grpcRoutes: []*gwapiv1.GRPCRoute{
				BuildGRPCRoute(func(r *gwapiv1.GRPCRoute) {
					r.Name = "route-1"
					r.Spec.ParentRefs[0].Name = "gw-1"
				}),
			},
			upstreams: map[TargetRef]RegisteredUpstreamEntry{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-http-svc", Host: "svc1", Port: 8081,
				},
				{Group: "gateway.networking.k8s.io", Kind: "GRPCRoute", Name: "route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-grpc-svc", Host: "svc2", Port: 8082,
				},
			},
			gatewayName:   "gw-1",
			expectedCount: 2,
			expectedNames: []string{"ext-http-svc", "ext-grpc-svc"},
		},
		{
			name: "upstream with mismatched kind is not collected",
			gateways: []*gwapiv1.Gateway{
				BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-1" }),
			},
			grpcRoutes: []*gwapiv1.GRPCRoute{
				BuildGRPCRoute(func(r *gwapiv1.GRPCRoute) {
					r.Name = "route-1"
					r.Spec.ParentRefs[0].Name = "gw-1"
				}),
			},
			upstreams: map[TargetRef]RegisteredUpstreamEntry{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route-1", Namespace: "my-namespace"}: {
					ClusterName: "ext-wrong-kind", Host: "svc1", Port: 8081,
				},
			},
			gatewayName:   "gw-1",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore GlobalMutatorRegistry
			originalRegistry := GlobalMutatorRegistry
			defer func() { GlobalMutatorRegistry = originalRegistry }()
			GlobalMutatorRegistry = &MutatorRegistry{}

			// Register upstreams
			store := NewRegisteredDataStore()
			for targetRef, entry := range tt.upstreams {
				entry.TargetRef = targetRef
				store.SetUpstream(
					RegisteredUpstreamKey{
						Policy:  testResourceID("TestPolicy", "my-namespace", "test-policy"),
						URL:     fmt.Sprintf("grpc://%s:%d", entry.Host, entry.Port),
						Service: "test.Service",
						Method:  "TestMethod",
					},
					entry,
					testFileDescriptorSet(),
				)
			}
			GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

			topology := buildTopology(tt.gateways, tt.httpRoutes, tt.grpcRoutes)
			gateway := findGateway(topology, tt.gatewayName)

			result := CollectRouteUpstreams(topology, gateway)

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d upstreams, got %d", tt.expectedCount, len(result))
			}

			for _, expectedName := range tt.expectedNames {
				found := false
				for _, entry := range result {
					if entry.ClusterName == expectedName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected upstream %s not found in results", expectedName)
				}
			}
		})
	}
}

func TestMutateWasmConfig_TranslatesPipelineActions(t *testing.T) {
	store := NewRegisteredDataStore()
	mockTargetRef := createMockGatewayTargetRef()
	targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: mockTargetRef.GetName(), Namespace: mockTargetRef.GetNamespace()}
	policyID := testResourceID("ThreatPolicy", "default", "my-threat")

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policyID, Name: "assess-threat", URL: "grpc://svc:8081", Service: "threat.Service", Method: "Check"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms", Service: "threat.Service", Method: "Check", MessageTemplate: "threat.v1.Request{path: request.path}"},
		testFileDescriptorSet(),
	)

	// Request phase: deny (root), grpc with var, deny referencing var (onReply)
	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: `request.url_path == "/blocked"`, WithStatus: 403},
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "assess-threat", Var: "threatResponse", Predicate: `"x-assess-threat" in request.headers`},
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "threatResponse.threat_level > 5", WithStatus: 429},
	})
	// Response phase: add_headers (root), fail referencing var (onReply)
	store.AppendPipelineActions(policyID, PipelinePhaseResponse, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, HeadersToAdd: `{"x-checked": "true"}`},
		{ActionType: extpb.ActionType_ACTION_TYPE_FAIL, Predicate: "threatResponse.blocked", LogMessage: "blocked by threat policy"},
	})

	mutator := NewRegisteredDataMutator[*wasm.Config](store)
	wasmConfig := &wasm.Config{
		Services: make(map[string]wasm.Service),
		ActionSets: []wasm.ActionSet{
			{Name: "test-action-set"},
		},
	}

	err := mutator.Mutate(wasmConfig, []machinery.PolicyTargetReference{mockTargetRef})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	typed := wasmConfig.ActionSets[0].Actions
	// Root-level: deny, grpc (with onReply), headers
	if len(typed) != 3 {
		t.Fatalf("Expected 3 root-level Actions, got %d", len(typed))
	}

	expectedLocator := "ThreatPolicy/default/my-threat"

	deny0 := typed[0].(*wasm.DenyAction)
	if deny0.ActionType() != wasm.ActionKindDeny {
		t.Errorf("typed[0]: expected type 'deny', got %q", deny0.ActionType())
	}
	if deny0.Predicate != `request.url_path == "/blocked"` {
		t.Errorf("typed[0]: expected predicate, got %q", deny0.Predicate)
	}
	if deny0.DenyWith != "DenyResponse{status: 403u}" {
		t.Errorf("typed[0]: expected denyWith 'DenyResponse{status: 403u}', got %q", deny0.DenyWith)
	}
	if !deny0.Terminal {
		t.Error("typed[0]: expected terminal")
	}

	grpc := typed[1].(*wasm.GrpcAction)
	if grpc.ActionType() != wasm.ActionKindGrpc {
		t.Errorf("typed[1]: expected type 'grpc', got %q", grpc.ActionType())
	}
	if grpc.Predicate != `"x-assess-threat" in request.headers` {
		t.Errorf("typed[1]: expected predicate, got %q", grpc.Predicate)
	}
	if grpc.Var != "threatResponse" {
		t.Errorf("typed[1]: expected var 'threatResponse', got %q", grpc.Var)
	}
	if grpc.Service == "" {
		t.Error("typed[1]: expected service to be set")
	}
	if grpc.MessageBuilder != "threat.v1.Request{path: request.path}" {
		t.Errorf("typed[1]: expected messageBuilder, got %q", grpc.MessageBuilder)
	}
	if grpc.Terminal {
		t.Error("typed[1]: expected NOT terminal")
	}
	if len(grpc.SourcePolicyLocators) != 1 || grpc.SourcePolicyLocators[0] != expectedLocator {
		t.Errorf("typed[1]: expected source %q, got %v", expectedLocator, grpc.SourcePolicyLocators)
	}

	if len(grpc.OnReply) != 2 {
		t.Fatalf("Expected 2 onReply actions, got %d", len(grpc.OnReply))
	}
	onReplyDeny := grpc.OnReply[0].(*wasm.DenyAction)
	if onReplyDeny.ActionType() != wasm.ActionKindDeny {
		t.Errorf("onReply[0]: expected type 'deny', got %q", onReplyDeny.ActionType())
	}
	if onReplyDeny.Predicate != "threatResponse.threat_level > 5" {
		t.Errorf("onReply[0]: expected predicate, got %q", onReplyDeny.Predicate)
	}
	if onReplyDeny.DenyWith != "DenyResponse{status: 429u}" {
		t.Errorf("onReply[0]: expected denyWith 'DenyResponse{status: 429u}', got %q", onReplyDeny.DenyWith)
	}
	if !onReplyDeny.Terminal {
		t.Error("onReply[0]: expected terminal")
	}
	onReplyFail := grpc.OnReply[1].(*wasm.FailAction)
	if onReplyFail.ActionType() != wasm.ActionKindFail {
		t.Errorf("onReply[1]: expected type 'fail', got %q", onReplyFail.ActionType())
	}
	if onReplyFail.Predicate != "threatResponse.blocked" {
		t.Errorf("onReply[1]: expected predicate, got %q", onReplyFail.Predicate)
	}
	if onReplyFail.LogMessage != "blocked by threat policy" {
		t.Errorf("onReply[1]: expected logMessage, got %q", onReplyFail.LogMessage)
	}

	headers2 := typed[2].(*wasm.HeadersAction)
	if headers2.ActionType() != wasm.ActionKindHeaders {
		t.Errorf("typed[2]: expected type 'headers', got %q", headers2.ActionType())
	}
	if headers2.Headers != `{"x-checked": "true"}` {
		t.Errorf("typed[2]: expected headers, got %q", headers2.Headers)
	}
	if headers2.Target != "response" {
		t.Errorf("typed[2]: expected target 'response', got %q", headers2.Target)
	}
}

func TestMutateWasmConfig_NoPipelineActionsNoChange(t *testing.T) {
	store := NewRegisteredDataStore()
	mockTargetRef := createMockGatewayTargetRef()
	targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: mockTargetRef.GetName(), Namespace: mockTargetRef.GetNamespace()}

	// Register an upstream but NO pipeline actions
	store.SetUpstream(
		RegisteredUpstreamKey{Policy: testResourceID("DemoPolicy", "default", "demo"), Name: "my-action", URL: "grpc://svc:8081", Service: "test.Service", Method: "Method1"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms", Service: "test.Service", Method: "Method1"},
		testFileDescriptorSet(),
	)

	mutator := NewRegisteredDataMutator[*wasm.Config](store)
	existingAction := &wasm.DenyAction{
		ActionBase: wasm.ActionBase{Predicate: "true", Terminal: true},
		DenyWith:   "DenyResponse{status: 403u}",
	}
	wasmConfig := &wasm.Config{
		Services:   make(map[string]wasm.Service),
		ActionSets: []wasm.ActionSet{{Name: "set1", Actions: []wasm.Action{existingAction}}},
	}

	err := mutator.Mutate(wasmConfig, []machinery.PolicyTargetReference{mockTargetRef})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Service should be injected, but existing action set unchanged (no pipeline actions)
	if len(wasmConfig.ActionSets[0].Actions) != 1 {
		t.Errorf("Expected 1 action (unchanged), got %d", len(wasmConfig.ActionSets[0].Actions))
	}
	if wasmConfig.ActionSets[0].Actions[0].ActionType() != wasm.ActionKindDeny {
		t.Errorf("Expected existing action preserved, got %q", wasmConfig.ActionSets[0].Actions[0].ActionType())
	}
}

func TestMutateWasmConfig_PipelineActionsAppendToMultipleActionSets(t *testing.T) {
	store := NewRegisteredDataStore()
	mockTargetRef := createMockGatewayTargetRef()
	targetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: mockTargetRef.GetName(), Namespace: mockTargetRef.GetNamespace()}
	policyID := testResourceID("ThreatPolicy", "default", "my-threat")

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policyID, Name: "check", URL: "grpc://svc:8081", Service: "threat.Service", Method: "Check"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: targetRef, FailureMode: "deny", Timeout: "100ms", Service: "threat.Service", Method: "Check"},
		testFileDescriptorSet(),
	)

	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "request.method == 'GET'", WithStatus: 403},
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "check"},
	})

	mutator := NewRegisteredDataMutator[*wasm.Config](store)
	wasmConfig := &wasm.Config{
		Services: make(map[string]wasm.Service),
		ActionSets: []wasm.ActionSet{
			{Name: "set1"},
			{Name: "set2"},
		},
	}

	err := mutator.Mutate(wasmConfig, []machinery.PolicyTargetReference{mockTargetRef})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	for i, as := range wasmConfig.ActionSets {
		if len(as.Actions) != 2 {
			t.Errorf("ActionSet[%d]: expected 2 actions, got %d", i, len(as.Actions))
			continue
		}
		if as.Actions[0].ActionType() != wasm.ActionKindDeny {
			t.Errorf("ActionSet[%d]: expected actions[0] deny, got %s", i, as.Actions[0].ActionType())
		}
		if as.Actions[1].ActionType() != wasm.ActionKindGrpc {
			t.Errorf("ActionSet[%d]: expected actions[1] grpc, got %s", i, as.Actions[1].ActionType())
		}
	}
}

// testTopology builds a topology with gateway→listener→route links for testing.
func testTopology(t *testing.T, gateways []*gwapiv1.Gateway, httpRoutes []*gwapiv1.HTTPRoute, grpcRoutes []*gwapiv1.GRPCRoute) *machinery.Topology {
	t.Helper()
	mGateways := lo.Map(gateways, func(g *gwapiv1.Gateway, _ int) *machinery.Gateway {
		return &machinery.Gateway{Gateway: g}
	})
	listeners := lo.FlatMap(mGateways, machinery.ListenersFromGatewayFunc)
	mHTTPRoutes := lo.Map(httpRoutes, func(r *gwapiv1.HTTPRoute, _ int) *machinery.HTTPRoute {
		return &machinery.HTTPRoute{HTTPRoute: r}
	})
	mGRPCRoutes := lo.Map(grpcRoutes, func(r *gwapiv1.GRPCRoute, _ int) *machinery.GRPCRoute {
		return &machinery.GRPCRoute{GRPCRoute: r}
	})

	topology, err := machinery.NewTopology(
		machinery.WithTargetables(mGateways...),
		machinery.WithTargetables(listeners...),
		machinery.WithTargetables(mHTTPRoutes...),
		machinery.WithTargetables(mGRPCRoutes...),
		machinery.WithLinks(
			machinery.LinkGatewayToListenerFunc(),
			machinery.LinkListenerToHTTPRouteFunc(mGateways, listeners),
			machinery.LinkListenerToGRPCRouteFunc(mGateways, listeners),
		),
	)
	if err != nil {
		t.Fatalf("Failed to build topology: %v", err)
	}
	return topology
}

func findGatewayInTopology(t *testing.T, topology *machinery.Topology, name string) *machinery.Gateway {
	t.Helper()
	for _, obj := range topology.Targetables().Items() {
		if gw, ok := obj.(*machinery.Gateway); ok && gw.GetName() == name {
			return gw
		}
	}
	t.Fatalf("Gateway %s not found in topology", name)
	return nil
}

func TestApplyWasmConfigMutators_CreatesActionSetsFromTopology(t *testing.T) {
	store := NewRegisteredDataStore()
	gatewayTargetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "test-gateway", Namespace: "test-namespace"}
	policyID := testResourceID("ThreatPolicy", "default", "my-threat")

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policyID, Name: "assess-threat", URL: "grpc://svc:8081", Service: "threat.Service", Method: "Check"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: gatewayTargetRef, FailureMode: "deny", Timeout: "100ms", Service: "threat.Service", Method: "Check"},
		testFileDescriptorSet(),
	)
	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: `request.url_path == "/blocked"`, WithStatus: 403},
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "assess-threat", Var: "threatResponse"},
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "threatResponse.threat_level > 5", WithStatus: 429},
	})

	savedRegistry := GlobalMutatorRegistry
	defer func() { GlobalMutatorRegistry = savedRegistry }()
	GlobalMutatorRegistry = &MutatorRegistry{}
	GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

	gw := BuildGateway(func(g *gwapiv1.Gateway) {
		g.Name = "test-gateway"
		g.Namespace = "test-namespace"
	})
	route := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "test-route"
		r.Namespace = "test-namespace"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "test-gateway"}}
		r.Spec.Hostnames = []gwapiv1.Hostname{"api.example.com"}
		r.Spec.Rules = []gwapiv1.HTTPRouteRule{{
			Matches: []gwapiv1.HTTPRouteMatch{{
				Path: &gwapiv1.HTTPPathMatch{
					Type:  ptr.To(gwapiv1.PathMatchExact),
					Value: ptr.To("/toy"),
				},
				Method: ptr.To(gwapiv1.HTTPMethodGet),
			}},
		}}
	})

	topology := testTopology(t, []*gwapiv1.Gateway{gw}, []*gwapiv1.HTTPRoute{route}, nil)
	gateway := findGatewayInTopology(t, topology, "test-gateway")

	wasmConfig := wasm.Config{}
	err := ApplyWasmConfigMutators(&wasmConfig, gateway, topology)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(wasmConfig.ActionSets) == 0 {
		t.Fatal("Expected actionsets to be created from topology, got 0")
	}

	as := wasmConfig.ActionSets[0]
	if len(as.RouteRuleConditions.Hostnames) != 1 || as.RouteRuleConditions.Hostnames[0] != "api.example.com" {
		t.Errorf("Expected hostname 'api.example.com', got %v", as.RouteRuleConditions.Hostnames)
	}

	// Root-level: deny + grpc (with var-dependent deny in onReply)
	if len(as.Actions) != 2 {
		t.Fatalf("Expected 2 root-level actions, got %d", len(as.Actions))
	}
	if as.Actions[0].ActionType() != wasm.ActionKindDeny {
		t.Errorf("Expected actions[0] deny, got %s", as.Actions[0].ActionType())
	}
	if as.Actions[1].ActionType() != wasm.ActionKindGrpc {
		t.Errorf("Expected actions[1] grpc, got %s", as.Actions[1].ActionType())
	}
	grpcAction := as.Actions[1].(*wasm.GrpcAction)
	if len(grpcAction.OnReply) != 1 {
		t.Fatalf("Expected 1 onReply action, got %d", len(grpcAction.OnReply))
	}
	if grpcAction.OnReply[0].ActionType() != wasm.ActionKindDeny {
		t.Errorf("Expected onReply[0] deny, got %s", grpcAction.OnReply[0].ActionType())
	}
	onReplyDeny := grpcAction.OnReply[0].(*wasm.DenyAction)
	if onReplyDeny.DenyWith != "DenyResponse{status: 429u}" {
		t.Errorf("Expected onReply[0] denyWith 'DenyResponse{status: 429u}', got %q", onReplyDeny.DenyWith)
	}
}

func TestApplyWasmConfigMutators_NoRoutesNoActionSets(t *testing.T) {
	store := NewRegisteredDataStore()
	gatewayTargetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "test-gateway", Namespace: "test-namespace"}
	policyID := testResourceID("ThreatPolicy", "default", "my-threat")

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policyID, Name: "assess-threat", URL: "grpc://svc:8081", Service: "threat.Service", Method: "Check"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: gatewayTargetRef, FailureMode: "deny", Timeout: "100ms", Service: "threat.Service", Method: "Check"},
		testFileDescriptorSet(),
	)
	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY},
	})

	savedRegistry := GlobalMutatorRegistry
	defer func() { GlobalMutatorRegistry = savedRegistry }()
	GlobalMutatorRegistry = &MutatorRegistry{}
	GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

	// Gateway with no routes attached
	gw := BuildGateway(func(g *gwapiv1.Gateway) {
		g.Name = "test-gateway"
		g.Namespace = "test-namespace"
	})
	topology := testTopology(t, []*gwapiv1.Gateway{gw}, nil, nil)
	gateway := findGatewayInTopology(t, topology, "test-gateway")

	wasmConfig := wasm.Config{}
	err := ApplyWasmConfigMutators(&wasmConfig, gateway, topology)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// No routes → no actionsets created
	if len(wasmConfig.ActionSets) != 0 {
		t.Errorf("Expected 0 actionsets (no routes), got %d", len(wasmConfig.ActionSets))
	}
}

func TestApplyWasmConfigMutators_ExistingActionSetsPreserved(t *testing.T) {
	store := NewRegisteredDataStore()
	gatewayTargetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "test-gateway", Namespace: "test-namespace"}
	policyID := testResourceID("ThreatPolicy", "default", "my-threat")

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policyID, Name: "assess-threat", URL: "grpc://svc:8081", Service: "threat.Service", Method: "Check"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: gatewayTargetRef, FailureMode: "deny", Timeout: "100ms", Service: "threat.Service", Method: "Check"},
		testFileDescriptorSet(),
	)
	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "request.method == 'GET'", WithStatus: 403},
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "assess-threat"},
	})

	savedRegistry := GlobalMutatorRegistry
	defer func() { GlobalMutatorRegistry = savedRegistry }()
	GlobalMutatorRegistry = &MutatorRegistry{}
	GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

	gw := BuildGateway(func(g *gwapiv1.Gateway) {
		g.Name = "test-gateway"
		g.Namespace = "test-namespace"
	})
	route := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "test-route"
		r.Namespace = "test-namespace"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "test-gateway"}}
	})
	topology := testTopology(t, []*gwapiv1.Gateway{gw}, []*gwapiv1.HTTPRoute{route}, nil)
	gateway := findGatewayInTopology(t, topology, "test-gateway")

	wasmConfig := wasm.Config{
		ActionSets: []wasm.ActionSet{
			{Name: "auth-actionset"},
		},
	}

	err := ApplyWasmConfigMutators(&wasmConfig, gateway, topology)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(wasmConfig.ActionSets) != 1 {
		t.Fatalf("Expected 1 actionset (existing), got %d", len(wasmConfig.ActionSets))
	}
	if wasmConfig.ActionSets[0].Name != "auth-actionset" {
		t.Errorf("Expected existing actionset preserved, got name %q", wasmConfig.ActionSets[0].Name)
	}

	actions := wasmConfig.ActionSets[0].Actions
	if len(actions) != 2 {
		t.Fatalf("Expected 2 actions (deny + grpc), got %d", len(actions))
	}
	if actions[0].ActionType() != wasm.ActionKindDeny {
		t.Errorf("Expected actions[0] deny, got %s", actions[0].ActionType())
	}
	if actions[1].ActionType() != wasm.ActionKindGrpc {
		t.Errorf("Expected actions[1] grpc, got %s", actions[1].ActionType())
	}
}

func TestReplacePipelineActions(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")
	otherPolicy := testResourceID("ThreatPolicy", "default", "other-policy")

	// Seed with initial actions for both policies
	store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "old-check"},
	})
	store.AppendPipelineActions(policy, PipelinePhaseResponse, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, HeadersToAdd: `{"x-old": "true"}`},
	})
	store.AppendPipelineActions(otherPolicy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, WithStatus: 403},
	})

	// Replace both phases atomically
	err := store.ReplacePipelineActions(policy, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: 403, Predicate: `request.url_path == "/blocked"`},
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Phase: "request", Method: "new-check", Var: "threatResponse"},
		{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, Phase: "response", HeadersToAdd: `{"x-new": "true"}`},
		{ActionType: extpb.ActionType_ACTION_TYPE_FAIL, Phase: "response", LogMessage: "blocked"},
	})
	if err != nil {
		t.Fatalf("ReplacePipelineActions returned error: %v", err)
	}

	// Verify request actions replaced
	reqActions := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if len(reqActions) != 2 {
		t.Fatalf("Expected 2 request actions, got %d", len(reqActions))
	}
	if reqActions[0].ActionType != extpb.ActionType_ACTION_TYPE_DENY {
		t.Errorf("First request action type = %v, want DENY", reqActions[0].ActionType)
	}
	if reqActions[1].Method != "new-check" {
		t.Errorf("Second request action method = %q, want %q", reqActions[1].Method, "new-check")
	}
	if reqActions[0].Index != 0 || reqActions[1].Index != 1 {
		t.Errorf("Indices not sequential: %d, %d", reqActions[0].Index, reqActions[1].Index)
	}

	// Verify response actions replaced
	respActions := store.GetPipelineActions(policy, PipelinePhaseResponse)
	if len(respActions) != 2 {
		t.Fatalf("Expected 2 response actions, got %d", len(respActions))
	}
	if respActions[0].HeadersToAdd != `{"x-new": "true"}` {
		t.Errorf("First response action headers = %q, unexpected", respActions[0].HeadersToAdd)
	}
	if respActions[1].LogMessage != "blocked" {
		t.Errorf("Second response action log message = %q, want %q", respActions[1].LogMessage, "blocked")
	}

	// Other policy unaffected
	otherActions := store.GetPipelineActions(otherPolicy, PipelinePhaseRequest)
	if len(otherActions) != 1 {
		t.Fatalf("Expected other policy to still have 1 action, got %d", len(otherActions))
	}
	if otherActions[0].ActionType != extpb.ActionType_ACTION_TYPE_DENY {
		t.Errorf("Other policy action type = %v, want DENY", otherActions[0].ActionType)
	}
}

func TestReplacePipelineActions_InvalidPhase(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	err := store.ReplacePipelineActions(policy, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "invalid", WithStatus: 403},
	})
	if err == nil {
		t.Fatal("Expected error for invalid phase, got nil")
	}
}

func TestReplacePipelineActions_EmptyReplacement(t *testing.T) {
	store := NewRegisteredDataStore()
	policy := testResourceID("ThreatPolicy", "default", "my-policy")

	store.AppendPipelineActions(policy, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "check"},
	})

	// Replace with nil clears everything
	if err := store.ReplacePipelineActions(policy, nil); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	reqActions := store.GetPipelineActions(policy, PipelinePhaseRequest)
	if reqActions != nil {
		t.Errorf("Expected nil request actions after empty replace, got %v", reqActions)
	}
	respActions := store.GetPipelineActions(policy, PipelinePhaseResponse)
	if respActions != nil {
		t.Errorf("Expected nil response actions after empty replace, got %v", respActions)
	}
}

func TestApplyWasmConfigMutators_RouteTargetedPipelineActions(t *testing.T) {
	store := NewRegisteredDataStore()
	routeTargetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "test-route", Namespace: "test-namespace"}
	policyID := testResourceID("ThreatPolicy", "test-namespace", "route-threat")

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policyID, Name: "assess-threat", URL: "grpc://svc:8081", Service: "threat.Service", Method: "Check"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: routeTargetRef, FailureMode: "deny", Timeout: "100ms", Service: "threat.Service", Method: "Check"},
		testFileDescriptorSet(),
	)
	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: `request.url_path == "/blocked"`, WithStatus: 403},
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "assess-threat"},
	})

	savedRegistry := GlobalMutatorRegistry
	defer func() { GlobalMutatorRegistry = savedRegistry }()
	GlobalMutatorRegistry = &MutatorRegistry{}
	GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

	gw := BuildGateway(func(g *gwapiv1.Gateway) {
		g.Name = "test-gateway"
		g.Namespace = "test-namespace"
	})
	matchingRoute := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "test-route"
		r.Namespace = "test-namespace"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "test-gateway"}}
		r.Spec.Hostnames = []gwapiv1.Hostname{"api.example.com"}
		r.Spec.Rules = []gwapiv1.HTTPRouteRule{{
			Matches: []gwapiv1.HTTPRouteMatch{{
				Path: &gwapiv1.HTTPPathMatch{
					Type:  ptr.To(gwapiv1.PathMatchExact),
					Value: ptr.To("/toy"),
				},
				Method: ptr.To(gwapiv1.HTTPMethodGet),
			}},
		}}
	})
	otherRoute := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "other-route"
		r.Namespace = "test-namespace"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "test-gateway"}}
		r.Spec.Hostnames = []gwapiv1.Hostname{"other.example.com"}
		r.Spec.Rules = []gwapiv1.HTTPRouteRule{{
			Matches: []gwapiv1.HTTPRouteMatch{{
				Path: &gwapiv1.HTTPPathMatch{
					Type:  ptr.To(gwapiv1.PathMatchExact),
					Value: ptr.To("/other"),
				},
				Method: ptr.To(gwapiv1.HTTPMethodGet),
			}},
		}}
	})

	topology := testTopology(t, []*gwapiv1.Gateway{gw}, []*gwapiv1.HTTPRoute{matchingRoute, otherRoute}, nil)
	gateway := findGatewayInTopology(t, topology, "test-gateway")

	wasmConfig := wasm.Config{}
	err := ApplyWasmConfigMutators(&wasmConfig, gateway, topology)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Only the matching route's action set survives; empty skeletons are filtered out.
	if len(wasmConfig.ActionSets) != 1 {
		t.Fatalf("Expected 1 actionset (matching route only), got %d", len(wasmConfig.ActionSets))
	}

	as := wasmConfig.ActionSets[0]
	if len(as.RouteRuleConditions.Hostnames) != 1 || as.RouteRuleConditions.Hostnames[0] != "api.example.com" {
		t.Errorf("Expected hostname 'api.example.com', got %v", as.RouteRuleConditions.Hostnames)
	}
	if len(as.Actions) != 2 {
		t.Fatalf("Expected 2 actions on matching route, got %d", len(as.Actions))
	}
	if as.Actions[0].ActionType() != wasm.ActionKindDeny {
		t.Errorf("Expected actions[0] deny, got %s", as.Actions[0].ActionType())
	}
	if as.Actions[1].ActionType() != wasm.ActionKindGrpc {
		t.Errorf("Expected actions[1] grpc, got %s", as.Actions[1].ActionType())
	}
	if as.Actions[1].(*wasm.GrpcAction).Service == "" {
		t.Error("Expected grpc action to have service set")
	}
}

func TestApplyWasmConfigMutators_RouteTargetedExtensionWithBuiltinActionSets(t *testing.T) {
	// This test reproduces the bug where extension pipeline actions targeting an
	// HTTPRoute are lost when built-in policies (AuthPolicy/RateLimitPolicy) have
	// already created ActionSets for the same route. Built-in ActionSets don't set
	// SourceRoute, so the extension's route-matching logic in mutateWasmConfig skips
	// them all — resulting in extension Actions never being appended.
	store := NewRegisteredDataStore()
	routeTargetRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "test-route", Namespace: "test-namespace"}
	policyID := testResourceID("ThreatPolicy", "test-namespace", "route-threat")

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policyID, Name: "assess-threat", URL: "grpc://svc:8081", Service: "threat.Service", Method: "Check"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: routeTargetRef, FailureMode: "deny", Timeout: "100ms", Service: "threat.Service", Method: "Check"},
		testFileDescriptorSet(),
	)
	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "assess-threat", Var: "threatResponse"},
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "threatResponse.threat_level > 5", WithStatus: 403},
	})

	savedRegistry := GlobalMutatorRegistry
	defer func() { GlobalMutatorRegistry = savedRegistry }()
	GlobalMutatorRegistry = &MutatorRegistry{}
	GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

	gw := BuildGateway(func(g *gwapiv1.Gateway) {
		g.Name = "test-gateway"
		g.Namespace = "test-namespace"
	})
	route := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "test-route"
		r.Namespace = "test-namespace"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "test-gateway"}}
		r.Spec.Hostnames = []gwapiv1.Hostname{"api.example.com"}
		r.Spec.Rules = []gwapiv1.HTTPRouteRule{{
			Matches: []gwapiv1.HTTPRouteMatch{{
				Path: &gwapiv1.HTTPPathMatch{
					Type:  ptr.To(gwapiv1.PathMatchExact),
					Value: ptr.To("/toy"),
				},
				Method: ptr.To(gwapiv1.HTTPMethodGet),
			}},
		}}
	})

	topology := testTopology(t, []*gwapiv1.Gateway{gw}, []*gwapiv1.HTTPRoute{route}, nil)
	gateway := findGatewayInTopology(t, topology, "test-gateway")

	// Simulate built-in policies (AuthPolicy/RateLimitPolicy) having already
	// created ActionSets. Crucially, these do NOT have SourceRoute set — exactly
	// as BuildActionSetsForPath produces them.
	wasmConfig := wasm.Config{
		ActionSets: []wasm.ActionSet{
			{
				Name: "builtin-actionset",
				RouteRuleConditions: wasm.RouteRuleConditions{
					Hostnames: []string{"api.example.com"},
				},
			},
		},
	}

	err := ApplyWasmConfigMutators(&wasmConfig, gateway, topology)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Built-in action set must survive
	if len(wasmConfig.ActionSets) != 1 {
		t.Fatalf("Expected 1 actionset, got %d", len(wasmConfig.ActionSets))
	}

	as := wasmConfig.ActionSets[0]

	// Extension Actions must be merged into the action set.
	// The deny depends on the var "threatResponse" from the gRPC action, so it
	// gets nested in the gRPC action's OnReply rather than being a separate root action.
	if len(as.Actions) != 1 {
		t.Fatalf("Expected 1 action (grpc with deny in onReply) merged into built-in actionset, got %d", len(as.Actions))
	}
	if as.Actions[0].ActionType() != wasm.ActionKindGrpc {
		t.Errorf("Expected actions[0] grpc, got %s", as.Actions[0].ActionType())
	}
	grpcAction := as.Actions[0].(*wasm.GrpcAction)
	if len(grpcAction.OnReply) != 1 {
		t.Fatalf("Expected 1 onReply action (deny), got %d", len(grpcAction.OnReply))
	}
	if grpcAction.OnReply[0].ActionType() != wasm.ActionKindDeny {
		t.Errorf("Expected onReply[0] deny, got %s", grpcAction.OnReply[0].ActionType())
	}
}

func TestMutateWasmConfig_DenyOnlyPipelineProducesRootAction(t *testing.T) {
	store := NewRegisteredDataStore()
	policyID := testResourceID("DenyPolicy", "default", "deny-only")
	routeRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "test-route", Namespace: "test-namespace"}

	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: `request.url_path == "/admin"`, WithStatus: 403},
	})
	store.SetPipelineTargetRefs(policyID, []TargetRef{routeRef})

	wasmConfig := wasm.Config{
		ActionSets: []wasm.ActionSet{{
			Name: "deny-test",
			RouteRuleConditions: wasm.RouteRuleConditions{
				Hostnames: []string{"example.com"},
			},
		}},
	}

	mockTargetRef := createMockHTTPRouteTargetRef()
	mutator := NewRegisteredDataMutator[*wasm.Config](store)
	err := mutator.Mutate(&wasmConfig, []machinery.PolicyTargetReference{mockTargetRef})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(wasmConfig.ActionSets[0].Actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(wasmConfig.ActionSets[0].Actions))
	}
	ta := wasmConfig.ActionSets[0].Actions[0].(*wasm.DenyAction)
	if ta.ActionType() != wasm.ActionKindDeny {
		t.Errorf("Expected type 'deny', got %q", ta.ActionType())
	}
	if ta.Predicate != `request.url_path == "/admin"` {
		t.Errorf("Expected predicate, got %q", ta.Predicate)
	}
	if ta.DenyWith != "DenyResponse{status: 403u}" {
		t.Errorf("Expected denyWith 'DenyResponse{status: 403u}', got %q", ta.DenyWith)
	}
	if !ta.Terminal {
		t.Error("Expected deny action to be terminal")
	}
}

func TestMutateWasmConfig_CrossGatewayIsolation(t *testing.T) {
	store := NewRegisteredDataStore()
	policyID := testResourceID("ThreatPolicy", "test-ns", "my-threat")
	routeRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route-a", Namespace: "test-ns"}

	store.SetUpstream(
		RegisteredUpstreamKey{Policy: policyID, Name: "assess", URL: "grpc://svc:8081", Service: "threat.Service", Method: "Check"},
		RegisteredUpstreamEntry{ClusterName: "ext-svc-8081", Host: "svc", Port: 8081, TargetRef: routeRef, FailureMode: "deny", Timeout: "100ms", Service: "threat.Service", Method: "Check"},
		testFileDescriptorSet(),
	)
	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Method: "assess"},
	})
	store.SetPipelineTargetRefs(policyID, []TargetRef{routeRef})

	savedRegistry := GlobalMutatorRegistry
	defer func() { GlobalMutatorRegistry = savedRegistry }()
	GlobalMutatorRegistry = &MutatorRegistry{}
	GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

	gwA := BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-a"; g.Namespace = "test-ns" })
	routeA := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "route-a"
		r.Namespace = "test-ns"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "gw-a"}}
		r.Spec.Hostnames = []gwapiv1.Hostname{"a.example.com"}
		r.Spec.Rules = []gwapiv1.HTTPRouteRule{{Matches: []gwapiv1.HTTPRouteMatch{{
			Path: &gwapiv1.HTTPPathMatch{Type: ptr.To(gwapiv1.PathMatchPathPrefix), Value: ptr.To("/")},
		}}}}
	})

	gwB := BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "gw-b"; g.Namespace = "test-ns" })
	routeB := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "route-b"
		r.Namespace = "test-ns"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "gw-b"}}
		r.Spec.Hostnames = []gwapiv1.Hostname{"b.example.com"}
		r.Spec.Rules = []gwapiv1.HTTPRouteRule{{Matches: []gwapiv1.HTTPRouteMatch{{
			Path: &gwapiv1.HTTPPathMatch{Type: ptr.To(gwapiv1.PathMatchPathPrefix), Value: ptr.To("/")},
		}}}}
	})

	topology := testTopology(t, []*gwapiv1.Gateway{gwA, gwB}, []*gwapiv1.HTTPRoute{routeA, routeB}, nil)
	gatewayA := findGatewayInTopology(t, topology, "gw-a")
	gatewayB := findGatewayInTopology(t, topology, "gw-b")

	configA := wasm.Config{}
	if err := ApplyWasmConfigMutators(&configA, gatewayA, topology); err != nil {
		t.Fatalf("Unexpected error for gw-a: %v", err)
	}
	if len(configA.ActionSets) != 1 {
		t.Fatalf("gw-a: expected 1 actionset, got %d", len(configA.ActionSets))
	}
	if len(configA.ActionSets[0].Actions) == 0 {
		t.Fatal("gw-a: expected actions on route-a's action set")
	}

	configB := wasm.Config{}
	if err := ApplyWasmConfigMutators(&configB, gatewayB, topology); err != nil {
		t.Fatalf("Unexpected error for gw-b: %v", err)
	}
	if len(configB.ActionSets) != 0 {
		t.Fatalf("gw-b: expected 0 actionsets (no policies target this gateway), got %d", len(configB.ActionSets))
	}
}

func TestMutateWasmConfig_PipelineOnlyRouteIsolation(t *testing.T) {
	store := NewRegisteredDataStore()
	policyID := testResourceID("DenyPolicy", "test-ns", "deny-route-a")
	routeRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route-a", Namespace: "test-ns"}

	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "true", WithStatus: 403},
	})
	store.SetPipelineTargetRefs(policyID, []TargetRef{routeRef})

	mutator := NewRegisteredDataMutator[*wasm.Config](store)
	routeTargetRef := policyTargetRef("HTTPRoute", "route-a", "test-ns")
	otherRouteRef := policyTargetRef("HTTPRoute", "route-b", "test-ns")
	targetRefs := []machinery.PolicyTargetReference{routeTargetRef, otherRouteRef}

	wasmConfig := &wasm.Config{
		ActionSets: []wasm.ActionSet{
			{Name: "set-a", SourceRoute: "HTTPRoute/test-ns/route-a"},
			{Name: "set-b", SourceRoute: "HTTPRoute/test-ns/route-b"},
		},
	}

	if err := mutator.Mutate(wasmConfig, targetRefs); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(wasmConfig.ActionSets[0].Actions) != 1 {
		t.Fatalf("route-a: expected 1 action, got %d", len(wasmConfig.ActionSets[0].Actions))
	}
	if wasmConfig.ActionSets[0].Actions[0].ActionType() != wasm.ActionKindDeny {
		t.Errorf("route-a: expected deny action, got %s", wasmConfig.ActionSets[0].Actions[0].ActionType())
	}

	if len(wasmConfig.ActionSets[1].Actions) != 0 {
		t.Fatalf("route-b: expected 0 actions (policy doesn't target this route), got %d", len(wasmConfig.ActionSets[1].Actions))
	}
}

func TestApplyWasmConfigMutators_SkeletonCreatedForExtensionOnlyRoute(t *testing.T) {
	store := NewRegisteredDataStore()
	policyID := testResourceID("DenyPolicy", "test-ns", "deny-route-b")
	routeRef := TargetRef{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "route-b", Namespace: "test-ns"}

	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "true", WithStatus: 403},
	})
	store.SetPipelineTargetRefs(policyID, []TargetRef{routeRef})

	savedRegistry := GlobalMutatorRegistry
	defer func() { GlobalMutatorRegistry = savedRegistry }()
	GlobalMutatorRegistry = &MutatorRegistry{}
	GlobalMutatorRegistry.RegisterWasmConfigMutator(NewRegisteredDataMutator[*wasm.Config](store))

	gw := BuildGateway(func(g *gwapiv1.Gateway) { g.Name = "test-gw"; g.Namespace = "test-ns" })
	routeA := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "route-a"
		r.Namespace = "test-ns"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "test-gw"}}
		r.Spec.Hostnames = []gwapiv1.Hostname{"a.example.com"}
		r.Spec.Rules = []gwapiv1.HTTPRouteRule{{Matches: []gwapiv1.HTTPRouteMatch{{
			Path: &gwapiv1.HTTPPathMatch{Type: ptr.To(gwapiv1.PathMatchPathPrefix), Value: ptr.To("/")},
		}}}}
	})
	routeB := BuildHTTPRoute(func(r *gwapiv1.HTTPRoute) {
		r.Name = "route-b"
		r.Namespace = "test-ns"
		r.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: "test-gw"}}
		r.Spec.Hostnames = []gwapiv1.Hostname{"b.example.com"}
		r.Spec.Rules = []gwapiv1.HTTPRouteRule{{Matches: []gwapiv1.HTTPRouteMatch{{
			Path: &gwapiv1.HTTPPathMatch{Type: ptr.To(gwapiv1.PathMatchPathPrefix), Value: ptr.To("/")},
		}}}}
	})

	topology := testTopology(t, []*gwapiv1.Gateway{gw}, []*gwapiv1.HTTPRoute{routeA, routeB}, nil)
	gateway := findGatewayInTopology(t, topology, "test-gw")

	// Simulate route-a having a built-in policy action set, route-b has none
	wasmConfig := wasm.Config{
		ActionSets: []wasm.ActionSet{
			{
				Name:                "builtin-a",
				SourceRoute:         "HTTPRoute/test-ns/route-a",
				RouteRuleConditions: wasm.RouteRuleConditions{Hostnames: []string{"a.example.com"}},
				Actions: []wasm.Action{
					&wasm.DenyAction{
						ActionBase: wasm.ActionBase{Predicate: "true", Terminal: true},
						DenyWith:   "DenyResponse{status: 503u}",
					},
				},
			},
		},
	}

	if err := ApplyWasmConfigMutators(&wasmConfig, gateway, topology); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(wasmConfig.ActionSets) != 2 {
		t.Fatalf("Expected 2 actionsets (builtin for route-a + skeleton for route-b), got %d", len(wasmConfig.ActionSets))
	}

	// route-a's action set should retain the existing built-in action only
	if wasmConfig.ActionSets[0].SourceRoute != "HTTPRoute/test-ns/route-a" {
		t.Errorf("Expected route-a action set first, got %s", wasmConfig.ActionSets[0].SourceRoute)
	}
	if len(wasmConfig.ActionSets[0].Actions) != 1 {
		t.Errorf("route-a: expected 1 action (built-in deny, extension targets route-b only), got %d", len(wasmConfig.ActionSets[0].Actions))
	}

	// route-b should have a skeleton with the deny action
	if wasmConfig.ActionSets[1].SourceRoute != "HTTPRoute/test-ns/route-b" {
		t.Errorf("Expected route-b action set second, got %s", wasmConfig.ActionSets[1].SourceRoute)
	}
	if len(wasmConfig.ActionSets[1].Actions) != 1 {
		t.Fatalf("route-b: expected 1 action (deny), got %d", len(wasmConfig.ActionSets[1].Actions))
	}
	if wasmConfig.ActionSets[1].Actions[0].ActionType() != wasm.ActionKindDeny {
		t.Errorf("route-b: expected deny action, got %s", wasmConfig.ActionSets[1].Actions[0].ActionType())
	}
}

func TestPipelineTargetRefCleanup(t *testing.T) {
	store := NewRegisteredDataStore()
	policyID := testResourceID("ThreatPolicy", "default", "my-policy")
	refs := []TargetRef{{Kind: "HTTPRoute", Name: "route-a", Namespace: "default"}}

	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "true", WithStatus: 403},
	})
	store.SetPipelineTargetRefs(policyID, refs)

	if got := store.GetPipelineTargetRefs(policyID); len(got) != 1 {
		t.Fatalf("Expected 1 target ref, got %d", len(got))
	}

	store.ClearPipelineActions(policyID)
	if got := store.GetPipelineTargetRefs(policyID); got != nil {
		t.Fatalf("Expected nil target refs after ClearPipelineActions, got %v", got)
	}

	// Verify ClearPolicyData also cleans up
	store.AppendPipelineActions(policyID, PipelinePhaseRequest, []PipelineActionEntry{
		{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Predicate: "true", WithStatus: 403},
	})
	store.SetPipelineTargetRefs(policyID, refs)
	_, _, _, clearedPipeline := store.ClearPolicyData(policyID)
	if clearedPipeline != 1 {
		t.Fatalf("Expected 1 cleared pipeline action, got %d", clearedPipeline)
	}
	if got := store.GetPipelineTargetRefs(policyID); got != nil {
		t.Fatalf("Expected nil target refs after ClearPolicyData, got %v", got)
	}
}
