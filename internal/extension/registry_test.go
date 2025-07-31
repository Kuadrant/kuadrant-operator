//go:build unit

package extension

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

func testPolicyID(kind, namespace, name string) PolicyID {
	return PolicyID{Kind: kind, Namespace: namespace, Name: name}
}

func TestRegisteredDataStore_Set_Get_Delete(t *testing.T) {
	store := NewRegisteredDataStore()

	entry := DataProviderEntry{
		Requester:  testPolicyID("Extension", "ns1", "ext1"),
		Binding:    "user",
		Expression: "user.id",
		CAst:       nil,
	}

	store.Set(testPolicyID("AuthPolicy", "ns1", "policy1"), testPolicyID("Extension", "ns1", "ext1"), "user", entry)

	retrieved, exists := store.Get(testPolicyID("AuthPolicy", "ns1", "policy1"), testPolicyID("Extension", "ns1", "ext1"), "user")
	if !exists {
		t.Fatal("Expected entry to exist")
	}

	if retrieved.Requester != entry.Requester {
		t.Errorf("Expected requester %+v, got %+v", entry.Requester, retrieved.Requester)
	}

	if retrieved.Binding != entry.Binding {
		t.Errorf("Expected binding %s, got %s", entry.Binding, retrieved.Binding)
	}

	entries := store.GetAllForTarget(testPolicyID("AuthPolicy", "ns1", "policy1"))
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}

	deleted := store.Delete(testPolicyID("AuthPolicy", "ns1", "policy1"), testPolicyID("Extension", "ns1", "ext1"), "user")
	if !deleted {
		t.Error("Expected entry to be deleted")
	}

	_, exists = store.Get(testPolicyID("AuthPolicy", "ns1", "policy1"), testPolicyID("Extension", "ns1", "ext1"), "user")
	if exists {
		t.Error("Expected entry to not exist after deletion")
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

	policyID := testPolicyID("AuthPolicy", "test-ns", "test-policy")
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

	entry1 := DataProviderEntry{
		Requester:  testPolicyID("Extension", "ns1", "ext1"),
		Binding:    "user_id",
		Expression: "user.id",
		CAst:       nil,
	}
	entry2 := DataProviderEntry{
		Requester:  testPolicyID("Extension", "ns1", "ext2"),
		Binding:    "user_email",
		Expression: "user.email",
		CAst:       nil,
	}

	store.Set(testPolicyID("AuthPolicy", "test-ns", "test-policy"), testPolicyID("Extension", "ns1", "ext1"), "user_id", entry1)
	store.Set(testPolicyID("AuthPolicy", "test-ns", "test-policy"), testPolicyID("Extension", "ns1", "ext2"), "user_email", entry2)
	store.Set(testPolicyID("AuthPolicy", "other-ns", "other-policy"), testPolicyID("Extension", "ns1", "ext1"), "user_id", entry1)

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

	store.SetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "expression1", subscription1)
	store.SetSubscription(testPolicyID("AuthPolicy", "other-ns", "other-policy"), "expression2", subscription2)

	allSubs := store.GetAllSubscriptions()
	if len(allSubs) != 2 {
		t.Errorf("Expected 2 subscriptions, got %d", len(allSubs))
	}

	testEntries := store.GetAllForTarget(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if len(testEntries) == 0 {
		t.Error("Expected test policy to have data")
	}
	otherEntries := store.GetAllForTarget(testPolicyID("AuthPolicy", "other-ns", "other-policy"))
	if len(otherEntries) == 0 {
		t.Error("Expected other policy to have data")
	}

	clearedMutators, clearedSubscriptions := store.ClearPolicyData(testPolicyID("AuthPolicy", "test-ns", "test-policy"))

	if clearedMutators != 2 {
		t.Errorf("Expected 2 cleared mutators, got %d", clearedMutators)
	}
	if clearedSubscriptions != 1 {
		t.Errorf("Expected 1 cleared subscription, got %d", clearedSubscriptions)
	}

	entries := store.GetAllForTarget(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", len(entries))
	}

	_, exists := store.GetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "expression1")
	if exists {
		t.Error("Expected subscription1 to be cleared")
	}

	otherEntriesAfterClear := store.GetAllForTarget(testPolicyID("AuthPolicy", "other-ns", "other-policy"))
	if len(otherEntriesAfterClear) != 1 {
		t.Errorf("Expected 1 entry for other policy, got %d", len(otherEntriesAfterClear))
	}

	_, exists = store.GetSubscription(testPolicyID("AuthPolicy", "other-ns", "other-policy"), "expression2")
	if !exists {
		t.Error("Expected subscription2 to still exist")
	}

	finalSubs := store.GetAllSubscriptions()
	if len(finalSubs) != 1 {
		t.Errorf("Expected 1 subscription after clear, got %d", len(finalSubs))
	}

	testEntriesAfter := store.GetAllForTarget(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if len(testEntriesAfter) != 0 {
		t.Error("Expected test policy to have no data after clear")
	}
	testSubsAfter := store.GetPolicySubscriptions(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if len(testSubsAfter) != 0 {
		t.Error("Expected test policy to have no subscriptions after clear")
	}

	otherEntriesAfter := store.GetAllForTarget(testPolicyID("AuthPolicy", "other-ns", "other-policy"))
	if len(otherEntriesAfter) == 0 {
		t.Error("Expected other policy to still have data after clear")
	}
}

func TestRegisteredDataStore_PolicyDataLifecycle(t *testing.T) {
	store := NewRegisteredDataStore()

	entries := store.GetAllForTarget(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	subscriptions := store.GetPolicySubscriptions(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if len(entries) != 0 || len(subscriptions) != 0 {
		t.Error("Expected no policy data initially")
	}

	entry := DataProviderEntry{
		Requester:  testPolicyID("Extension", "ns1", "ext1"),
		Binding:    "user_id",
		Expression: "user.id",
		CAst:       nil,
	}
	store.Set(testPolicyID("AuthPolicy", "test-ns", "test-policy"), testPolicyID("Extension", "ns1", "ext1"), "user_id", entry)

	entries = store.GetAllForTarget(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if len(entries) == 0 {
		t.Error("Expected policy data after adding entry")
	}

	store.ClearPolicyData(testPolicyID("AuthPolicy", "test-ns", "test-policy"))

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
	store.SetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "user.data", subscription)

	subscriptions = store.GetPolicySubscriptions(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
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

	store.SetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "some.expression", subscription)

	newVal := ref.Val(nil)
	updated := store.UpdateSubscriptionValue(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "some.expression", newVal)
	if !updated {
		t.Error("Expected subscription value to be updated")
	}

	updated = store.UpdateSubscriptionValue(testPolicyID("non-existent", "ns", "name"), "expression", newVal)
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

	store.SetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "expression1", subscription1)
	store.SetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "expression2", subscription2)
	store.SetSubscription(testPolicyID("AuthPolicy", "other-ns", "other-policy"), "expression3", subscription3)

	subscriptions := store.GetPolicySubscriptions(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if len(subscriptions) != 2 {
		t.Errorf("Expected 2 subscriptions for test policy, got %d", len(subscriptions))
	}

	subscriptions = store.GetPolicySubscriptions(testPolicyID("AuthPolicy", "other-ns", "other-policy"))
	if len(subscriptions) != 1 {
		t.Errorf("Expected 1 subscription for other policy, got %d", len(subscriptions))
	}

	subscriptions = store.GetPolicySubscriptions(testPolicyID("AuthPolicy", "non-existent", "policy"))
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

	store.SetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "expression1", subscription1)
	store.SetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "expression2", subscription2)
	store.SetSubscription(testPolicyID("AuthPolicy", "other-ns", "other-policy"), "expression3", subscription3)

	_, cleared := store.ClearPolicyData(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if cleared != 2 {
		t.Errorf("Expected 2 cleared subscriptions, got %d", cleared)
	}

	subscriptions := store.GetPolicySubscriptions(testPolicyID("AuthPolicy", "test-ns", "test-policy"))
	if len(subscriptions) != 0 {
		t.Errorf("Expected 0 subscriptions after clear, got %d", len(subscriptions))
	}

	subscriptions = store.GetPolicySubscriptions(testPolicyID("AuthPolicy", "other-ns", "other-policy"))
	if len(subscriptions) != 1 {
		t.Errorf("Expected 1 subscription for other policy, got %d", len(subscriptions))
	}

	_, cleared = store.ClearPolicyData(testPolicyID("AuthPolicy", "non-existent", "policy"))
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

	store.SetSubscription(testPolicyID("AuthPolicy", "test-ns", "test-policy"), "expression1", authPolicySubscription)
	store.SetSubscription(testPolicyID("RateLimitPolicy", "test-ns", "test-policy"), "expression1", rateLimitPolicySubscription)

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
		expectedKey := SubscriptionKey{Policy: testPolicyID("AuthPolicy", "test-ns", "test-policy"), Expression: "expression1"}
		if key != expectedKey {
			t.Errorf("Expected key %+v, got %+v", expectedKey, key)
		}
	}
}

func TestRegisteredDataMutator(t *testing.T) {
	t.Run("mutate with empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()
		mutator := NewRegisteredDataMutator(store)

		authConfig := &authorinov1beta3.AuthConfig{}
		policy := &kuadrantv1.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: "kuadrant.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-namespace",
			},
		}

		err := mutator.Mutate(authConfig, policy)
		if err != nil {
			t.Errorf("Expected no error with empty store: %v", err)
		}

		if authConfig.Spec.Response != nil {
			t.Error("Expected AuthConfig to remain unmodified when store is empty")
		}
	})

	t.Run("mutate with registered data", func(t *testing.T) {
		store := NewRegisteredDataStore()
		mutator := NewRegisteredDataMutator(store)

		entry1 := DataProviderEntry{
			Requester:  testPolicyID("Extension", "ns1", "ext1"),
			Binding:    "user_id",
			Expression: "user.id",
			CAst:       nil,
		}
		entry2 := DataProviderEntry{
			Requester:  testPolicyID("Extension", "ns1", "ext2"),
			Binding:    "user_email",
			Expression: "user.email",
			CAst:       nil,
		}

		store.Set(testPolicyID("AuthPolicy", "test-namespace", "test-policy"), testPolicyID("Extension", "ns1", "ext1"), "user_id", entry1)
		store.Set(testPolicyID("AuthPolicy", "test-namespace", "test-policy"), testPolicyID("Extension", "ns1", "ext2"), "user_email", entry2)

		authConfig := &authorinov1beta3.AuthConfig{}
		policy := &kuadrantv1.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: "kuadrant.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-namespace",
			},
		}

		err := mutator.Mutate(authConfig, policy)
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

		userIdProp, exists := kuadrantMetadata.Json.Properties["user_id"]
		if !exists {
			t.Error("Expected 'user_id' property to exist")
		} else if string(userIdProp.Expression) != "user.id" {
			t.Errorf("Expected expression 'user.id', got '%s'", userIdProp.Expression)
		}

		userEmailProp, exists := kuadrantMetadata.Json.Properties["user_email"]
		if !exists {
			t.Error("Expected 'user_email' property to exist")
		} else if string(userEmailProp.Expression) != "user.email" {
			t.Errorf("Expected expression 'user.email', got '%s'", userEmailProp.Expression)
		}
	})

	t.Run("mutate with existing response config", func(t *testing.T) {
		store := NewRegisteredDataStore()
		mutator := NewRegisteredDataMutator(store)

		entry := DataProviderEntry{
			Requester:  testPolicyID("Extension", "ns1", "ext1"),
			Binding:    "custom_data",
			Expression: "custom.expression",
			CAst:       nil,
		}
		store.Set(testPolicyID("AuthPolicy", "test-namespace", "test-policy"), testPolicyID("Extension", "ns1", "ext1"), "custom_data", entry)

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

		policy := &kuadrantv1.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: "kuadrant.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-namespace",
			},
		}

		err := mutator.Mutate(authConfig, policy)
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
			mutateFn: func(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
				mutator1Called = true
				return nil
			},
		}

		mutator2 := &mockAuthConfigMutator{
			mutateFn: func(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
				mutator2Called = true
				return nil
			},
		}

		registry.RegisterAuthConfigMutator(mutator1)
		registry.RegisterAuthConfigMutator(mutator2)

		authConfig := &authorinov1beta3.AuthConfig{}
		policy := &kuadrantv1.AuthPolicy{}

		err := registry.ApplyAuthConfigMutators(authConfig, policy)
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
			mutateFn: func(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
				return fmt.Errorf("mutator error")
			},
		}

		registry.RegisterAuthConfigMutator(errorMutator)

		authConfig := &authorinov1beta3.AuthConfig{}
		policy := &kuadrantv1.AuthPolicy{}

		err := registry.ApplyAuthConfigMutators(authConfig, policy)
		if err == nil {
			t.Error("Expected error from failing mutator")
		}
		if err.Error() != "mutator error" {
			t.Errorf("Expected 'mutator error', got '%s'", err.Error())
		}
	})

	t.Run("global mutator registry", func(t *testing.T) {
		authConfig := &authorinov1beta3.AuthConfig{}
		policy := &kuadrantv1.AuthPolicy{}

		err := ApplyAuthConfigMutators(authConfig, policy)
		if err != nil {
			t.Errorf("Expected no error from global registry: %v", err)
		}
	})
}

func TestRegisteredDataStoreEdgeCases(t *testing.T) {
	t.Run("concurrent operations", func(t *testing.T) {
		store := NewRegisteredDataStore()

		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				entry := DataProviderEntry{
					Requester:  testPolicyID("Extension", "ns1", fmt.Sprintf("ext%d", index)),
					Binding:    fmt.Sprintf("binding%d", index),
					Expression: fmt.Sprintf("expression%d", index),
					CAst:       nil,
				}
				store.Set(testPolicyID("TestPolicy", "ns", "policy"), testPolicyID("Extension", "ns1", fmt.Sprintf("ext%d", index)), fmt.Sprintf("binding%d", index), entry)
			}(i)
		}

		wg.Wait()

		entries := store.GetAllForTarget(testPolicyID("TestPolicy", "ns", "policy"))
		if len(entries) != 10 {
			t.Errorf("Expected 10 entries, got %d", len(entries))
		}
	})

	t.Run("delete from empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()

		deleted := store.Delete(testPolicyID("non-existent", "ns", "name"), testPolicyID("non-existent", "ns", "name"), "non-existent")
		if deleted {
			t.Error("Expected delete to return false for non-existent entry")
		}
	})

	t.Run("get from empty store", func(t *testing.T) {
		store := NewRegisteredDataStore()

		_, exists := store.Get(testPolicyID("non-existent", "ns", "name"), testPolicyID("non-existent", "ns", "name"), "non-existent")
		if exists {
			t.Error("Expected get to return false for non-existent entry")
		}

		exists = store.Exists(testPolicyID("non-existent", "ns", "name"), testPolicyID("non-existent", "ns", "name"), "non-existent")
		if exists {
			t.Error("Expected exists to return false for non-existent entry")
		}
	})

	t.Run("clear empty target", func(t *testing.T) {
		store := NewRegisteredDataStore()

		cleared, _ := store.ClearPolicyData(testPolicyID("non-existent", "ns", "name"))
		if cleared != 0 {
			t.Errorf("Expected 0 cleared entries, got %d", cleared)
		}
	})

	t.Run("set and delete maintaining map structure", func(t *testing.T) {
		store := NewRegisteredDataStore()

		entry := DataProviderEntry{
			Requester:  testPolicyID("Extension", "ns1", "ext1"),
			Binding:    "binding1",
			Expression: "expression1",
			CAst:       nil,
		}

		store.Set(testPolicyID("TestPolicy", "ns", "policy"), testPolicyID("Extension", "ns1", "ext1"), "binding1", entry)

		if !store.Exists(testPolicyID("TestPolicy", "ns", "policy"), testPolicyID("Extension", "ns1", "ext1"), "binding1") {
			t.Error("Expected entry to exist after setting")
		}

		deleted := store.Delete(testPolicyID("TestPolicy", "ns", "policy"), testPolicyID("Extension", "ns1", "ext1"), "binding1")
		if !deleted {
			t.Error("Expected delete to return true")
		}

		if store.Exists(testPolicyID("TestPolicy", "ns", "policy"), testPolicyID("Extension", "ns1", "ext1"), "binding1") {
			t.Error("Expected entry to not exist after deleting")
		}

		entries := store.GetAllForTarget(testPolicyID("TestPolicy", "ns", "policy"))
		if entries != nil {
			t.Error("Expected nil entries for cleaned up target")
		}
	})
}

// Mock mutator
type mockAuthConfigMutator struct {
	mutateFn func(*authorinov1beta3.AuthConfig, *kuadrantv1.AuthPolicy) error
}

func (m *mockAuthConfigMutator) Mutate(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
	return m.mutateFn(authConfig, policy)
}
