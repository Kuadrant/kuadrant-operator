//go:build unit

package controllers

import (
	"context"
	"testing"

	_struct "google.golang.org/protobuf/types/known/structpb"
	"gotest.tools/assert"
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istiov1beta1 "istio.io/api/type/v1beta1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

func TestMergeAndVerify(t *testing.T) {
	tests := []struct {
		name          string
		actions       []wasm.Action
		expectedError string
		expectedLen   int
		description   string
		validate      func(*testing.T, []wasm.Action)
	}{
		{
			name: "mixed auth and rate limit actions - auth never merged",
			actions: []wasm.Action{
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "auth.identity",
											Value: "user_token",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "global", // Same scope, but auth actions should never merge
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "auth.permissions",
											Value: "admin",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "user_id",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 3,
			description: "should keep auth actions separate even with same scope",
			validate: func(t *testing.T, result []wasm.Action) {
				authCount := 0
				rateLimitCount := 0
				for _, action := range result {
					switch action.ServiceName {
					case wasm.AuthServiceName:
						authCount++
					case wasm.RateLimitServiceName:
						rateLimitCount++
					}
				}
				assert.Equal(t, 2, authCount, "should have 2 separate auth actions")
				assert.Equal(t, 1, rateLimitCount, "should have 1 rate limit action")
			},
		},
		{
			name: "mixed auth and mergeable rate limit actions",
			actions: []wasm.Action{
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "auth.identity",
											Value: "user_token",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "user_id",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.hits_addend",
											Value: "1",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 2,
			description: "should merge rate limit actions but keep auth action separate",
			validate: func(t *testing.T, result []wasm.Action) {
				authCount := 0
				rateLimitCount := 0
				var mergedRateLimitAction *wasm.Action

				for i, action := range result {
					switch action.ServiceName {
					case wasm.AuthServiceName:
						authCount++
					case wasm.RateLimitServiceName:
						rateLimitCount++
						mergedRateLimitAction = &result[i]
					}
				}

				assert.Equal(t, 1, authCount, "should have 1 auth action")
				assert.Equal(t, 1, rateLimitCount, "should have 1 merged rate limit action")

				// Verify the rate limit action was properly merged
				if mergedRateLimitAction != nil {
					assert.Equal(t, 2, len(mergedRateLimitAction.ConditionalData), "merged rate limit action should have 2 conditional data items")
				}
			},
		},
		{
			name: "multiple auth actions with different scopes - never merged",
			actions: []wasm.Action{
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "auth.identity",
											Value: "global_user",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "namespace",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "auth.identity",
											Value: "namespace_user",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 2,
			description: "should never merge auth actions regardless of scope differences",
		},
		{
			name: "complex mixed scenario with multiple service types",
			actions: []wasm.Action{
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "user_id",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "auth.identity",
											Value: "jwt_token",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "tokenratelimit.limit_key",
											Value: "api_key",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "auth.identity",
											Value: "user_id",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 4,
			description: "auth should not merge with rate limit actions, but rate limit actions should merge",
			validate: func(t *testing.T, result []wasm.Action) {
				authCount := 0
				rateLimitCount := 0

				for _, action := range result {
					switch action.ServiceName {
					case wasm.AuthServiceName:
						authCount++
					case wasm.RateLimitServiceName:
						rateLimitCount++
					}
				}

				assert.Equal(t, 2, authCount, "should have 2 auth action")
				assert.Equal(t, 2, rateLimitCount, "should have 1 merged ratelimit action")
			},
		},
		{
			name: "rate limit actions with different scopes - no merge",
			actions: []wasm.Action{
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "global_key",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "namespace",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "namespace_key",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 2,
			description: "should not merge rate limit actions with different scopes",
		},
		{
			name: "duplicate keys with different values in rate limit actions - error",
			actions: []wasm.Action{
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.hits_addend",
											Value: "1",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.hits_addend",
											Value: "5",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: "duplicate key 'ratelimit.hits_addend' with different values found in action",
			description:   "should detect duplicate keys with different values in mergeable actions",
		},
		{
			name: "duplicate keys with same values in rate limit actions - success",
			actions: []wasm.Action{
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "user_id",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "user_id",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 1,
			description: "should allow duplicate keys with same values in mergeable actions",
		},
		{
			name: "subsequent RateLimitCheckService actions merge correctly",
			actions: []wasm.Action{
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "user_id",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.hits_addend",
											Value: "1",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 1,
			description: "should merge two subsequent RateLimitCheckServiceName actions",
			validate: func(t *testing.T, result []wasm.Action) {
				assert.Equal(t, "ratelimit-check-service", result[0].ServiceName)
				assert.Equal(t, 2, len(result[0].ConditionalData), "merged action should contain data from both original actions")
			},
		},
		{
			name: "RateLimitCheckService and RateLimitService do not merge",
			actions: []wasm.Action{
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "user_id",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: wasm.RateLimitCheckServiceName,
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "check.rate",
											Value: "100",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 2,
			description: "should not merge RateLimitServiceName with the new RateLimitCheckServiceName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeAndVerify(context.TODO(), tt.actions)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError, "description: %s", tt.description)
			} else {
				assert.NilError(t, err, "description: %s", tt.description)
				assert.Equal(t, tt.expectedLen, len(result), "description: %s", tt.description)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestMergeAndVerifyEdgeCases(t *testing.T) {
	t.Run("empty conditional data", func(t *testing.T) {
		actions := []wasm.Action{
			{
				ServiceName:     wasm.RateLimitServiceName,
				Scope:           "global",
				ConditionalData: []wasm.ConditionalData{},
			},
			{
				ServiceName:     wasm.RateLimitServiceName,
				Scope:           "global",
				ConditionalData: []wasm.ConditionalData{},
			},
		}

		result, err := mergeAndVerify(context.TODO(), actions)
		assert.NilError(t, err)
		assert.Equal(t, len(result), 1)
	})

	t.Run("empty data in conditional data", func(t *testing.T) {
		actions := []wasm.Action{
			{
				ServiceName: wasm.RateLimitServiceName,
				Scope:       "global",
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{},
					},
				},
			},
		}

		result, err := mergeAndVerify(context.TODO(), actions)
		assert.NilError(t, err)
		assert.Equal(t, len(result), 1)
	})

	t.Run("empty keys are handled", func(t *testing.T) {
		actions := []wasm.Action{
			{
				ServiceName: wasm.RateLimitServiceName,
				Scope:       "global",
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
										Key:   "",
										Value: "100",
									},
								},
							},
						},
					},
				},
			},
			{
				ServiceName: wasm.RateLimitServiceName,
				Scope:       "global",
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
										Key:   "",
										Value: "200",
									},
								},
							},
						},
					},
				},
			},
		}

		_, err := mergeAndVerify(context.TODO(), actions)
		assert.ErrorContains(t, err, "duplicate key '' with different values")
	})
}

func Test_equalWasmPlugins(t *testing.T) {
	ptr := func(s string) *string { return &s }

	// Helper to create a basic wasm config
	createWasmConfig := func(actionSets []wasm.ActionSet, services map[string]wasm.Service, requestData map[string]string) *wasm.Config {
		return &wasm.Config{
			ActionSets:  actionSets,
			Services:    services,
			RequestData: requestData,
		}
	}

	// Helper to convert config to struct
	configToStruct := func(cfg *wasm.Config) *_struct.Struct {
		if cfg == nil {
			return nil
		}
		s, _ := cfg.ToStruct()
		return s
	}

	tests := []struct {
		name string
		a    *istioclientgoextensionv1alpha1.WasmPlugin
		b    *istioclientgoextensionv1alpha1.WasmPlugin
		want bool
	}{
		{
			name: "both plugins are identical - simple case",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig:    nil,
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig:    nil,
				},
			},
			want: true,
		},
		{
			name: "different ImagePullSecret",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "secret1",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "secret2",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
				},
			},
			want: false,
		},
		{
			name: "different URL",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v2",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
				},
			},
			want: false,
		},
		{
			name: "different Phase",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_AUTHN,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
				},
			},
			want: false,
		},
		{
			name: "different TargetRefs - different count",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs: []*istiov1beta1.PolicyTargetReference{
						{
							Group: "gateway.networking.k8s.io",
							Kind:  "Gateway",
							Name:  "gateway1",
						},
					},
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs: []*istiov1beta1.PolicyTargetReference{
						{
							Group: "gateway.networking.k8s.io",
							Kind:  "Gateway",
							Name:  "gateway1",
						},
						{
							Group: "gateway.networking.k8s.io",
							Kind:  "Gateway",
							Name:  "gateway2",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different TargetRefs - different names",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs: []*istiov1beta1.PolicyTargetReference{
						{
							Group: "gateway.networking.k8s.io",
							Kind:  "Gateway",
							Name:  "gateway1",
						},
					},
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs: []*istiov1beta1.PolicyTargetReference{
						{
							Group: "gateway.networking.k8s.io",
							Kind:  "Gateway",
							Name:  "gateway2",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "one has PluginConfig, other doesn't",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						nil,
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig:    nil,
				},
			},
			want: false,
		},
		{
			name: "both have nil PluginConfig",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig:    nil,
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig:    nil,
				},
			},
			want: true,
		},
		{
			name: "identical complex PluginConfig with multiple ActionSets",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "secret",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "actionset1"},
							{Name: "actionset2"},
							{Name: "actionset3"},
						},
						map[string]wasm.Service{
							"service1": {
								Endpoint:    "http://service1:8080",
								Type:        wasm.RateLimitServiceType,
								FailureMode: wasm.FailureModeAllow,
							},
							"service2": {
								Endpoint:    "http://service2:8080",
								Type:        wasm.AuthServiceType,
								FailureMode: wasm.FailureModeDeny,
								Timeout:     ptr("500ms"),
							},
						},
						map[string]string{
							"key1": "value1",
							"key2": "value2",
							"key3": "value3",
						},
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "secret",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "actionset1"},
							{Name: "actionset2"},
							{Name: "actionset3"},
						},
						map[string]wasm.Service{
							"service1": {
								Endpoint:    "http://service1:8080",
								Type:        wasm.RateLimitServiceType,
								FailureMode: wasm.FailureModeAllow,
							},
							"service2": {
								Endpoint:    "http://service2:8080",
								Type:        wasm.AuthServiceType,
								FailureMode: wasm.FailureModeDeny,
								Timeout:     ptr("500ms"),
							},
						},
						map[string]string{
							"key1": "value1",
							"key2": "value2",
							"key3": "value3",
						},
					)),
				},
			},
			want: true,
		},
		{
			name: "different ActionSets count",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "actionset1"},
							{Name: "actionset2"},
						},
						nil,
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "actionset1"},
						},
						nil,
						nil,
					)),
				},
			},
			want: false,
		},
		{
			name: "equal ActionSets - out of order (order matters)",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "actionset1"},
							{Name: "actionset2"},
						},
						nil,
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "actionset2"},
							{Name: "actionset1"},
						},
						nil,
						nil,
					)),
				},
			},
			want: false,
		},
		{
			name: "different ActionSets names",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "actionset1"},
							{Name: "actionset2"},
						},
						nil,
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "actionset1"},
							{Name: "actionset_different"},
						},
						nil,
						nil,
					)),
				},
			},
			want: false,
		},
		{
			name: "equal Services count - out of order",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {Endpoint: "http://service1:8080", Type: wasm.RateLimitServiceType, FailureMode: wasm.FailureModeAllow},
							"service2": {Endpoint: "http://service2:8080", Type: wasm.AuthServiceType, FailureMode: wasm.FailureModeDeny},
						},
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service2": {Endpoint: "http://service2:8080", Type: wasm.AuthServiceType, FailureMode: wasm.FailureModeDeny},
							"service1": {Endpoint: "http://service1:8080", Type: wasm.RateLimitServiceType, FailureMode: wasm.FailureModeAllow},
						},
						nil,
					)),
				},
			},
			want: true,
		},
		{
			name: "different Services count",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {Endpoint: "http://service1:8080", Type: wasm.RateLimitServiceType, FailureMode: wasm.FailureModeAllow},
							"service2": {Endpoint: "http://service2:8080", Type: wasm.AuthServiceType, FailureMode: wasm.FailureModeDeny},
						},
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {Endpoint: "http://service1:8080", Type: wasm.RateLimitServiceType, FailureMode: wasm.FailureModeAllow},
						},
						nil,
					)),
				},
			},
			want: false,
		},
		{
			name: "different Service endpoint",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {Endpoint: "http://service1:8080", Type: wasm.RateLimitServiceType, FailureMode: wasm.FailureModeAllow},
						},
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {Endpoint: "http://service1:9090", Type: wasm.RateLimitServiceType, FailureMode: wasm.FailureModeAllow},
						},
						nil,
					)),
				},
			},
			want: false,
		},
		{
			name: "different Service type",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {Endpoint: "http://service1:8080", Type: wasm.RateLimitServiceType, FailureMode: wasm.FailureModeAllow},
						},
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {Endpoint: "http://service1:8080", Type: wasm.AuthServiceType, FailureMode: wasm.FailureModeAllow},
						},
						nil,
					)),
				},
			},
			want: false,
		},
		{
			name: "equal RequestData count - out of order",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						nil,
						map[string]string{
							"key1": "value1",
							"key2": "value2",
						},
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						nil,
						map[string]string{
							"key2": "value2",
							"key1": "value1",
						},
					)),
				},
			},
			want: true,
		},
		{
			name: "different RequestData count",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						nil,
						map[string]string{
							"key1": "value1",
							"key2": "value2",
						},
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						nil,
						map[string]string{
							"key1": "value1",
						},
					)),
				},
			},
			want: false,
		},
		{
			name: "different RequestData values",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						nil,
						map[string]string{
							"key1": "value1",
							"key2": "value2",
						},
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						nil,
						map[string]string{
							"key1": "value1",
							"key2": "different_value",
						},
					)),
				},
			},
			want: false,
		},
		{
			name: "very complex identical PluginConfig with all fields populated",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://registry.example.com/wasm-shim:v2.1.0",
					ImagePullSecret: "registry-secret",
					Phase:           istioextensionsv1alpha1.PluginPhase_AUTHN,
					TargetRefs: []*istiov1beta1.PolicyTargetReference{
						{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "prod-gateway"},
						{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "staging-gateway"},
					},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "auth-actionset"},
							{Name: "ratelimit-actionset"},
							{Name: "logging-actionset"},
							{Name: "metrics-actionset"},
							{Name: "tracing-actionset"},
						},
						map[string]wasm.Service{
							"auth-service": {
								Endpoint:    "http://auth-service.auth-ns.svc.cluster.local:8080",
								Type:        wasm.AuthServiceType,
								FailureMode: wasm.FailureModeDeny,
								Timeout:     ptr("1000ms"),
								GrpcService: ptr("auth.v1.AuthService"),
								GrpcMethod:  ptr("Check"),
							},
							"ratelimit-service": {
								Endpoint:    "http://limitador.limitador-ns.svc.cluster.local:8081",
								Type:        wasm.RateLimitServiceType,
								FailureMode: wasm.FailureModeAllow,
								Timeout:     ptr("500ms"),
								GrpcService: ptr("envoy.service.ratelimit.v3.RateLimitService"),
								GrpcMethod:  ptr("ShouldRateLimit"),
							},
							"metrics-service": {
								Endpoint:    "http://metrics.monitoring.svc.cluster.local:9090",
								Type:        wasm.RateLimitServiceType,
								FailureMode: wasm.FailureModeAllow,
								Timeout:     ptr("200ms"),
							},
						},
						map[string]string{
							"tenant_id":        "tenant-123",
							"environment":      "production",
							"region":           "us-east-1",
							"cluster":          "main-cluster",
							"version":          "v2.1.0",
							"feature_flag_1":   "enabled",
							"feature_flag_2":   "disabled",
							"metadata_key_1":   "metadata_value_1",
							"metadata_key_2":   "metadata_value_2",
							"custom_header":    "X-Custom-Header",
							"log_level":        "info",
							"trace_sampling":   "0.1",
							"circuit_breaker":  "enabled",
							"retry_policy":     "exponential",
							"timeout_override": "5s",
						},
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://registry.example.com/wasm-shim:v2.1.0",
					ImagePullSecret: "registry-secret",
					Phase:           istioextensionsv1alpha1.PluginPhase_AUTHN,
					TargetRefs: []*istiov1beta1.PolicyTargetReference{
						{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "prod-gateway"},
						{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "staging-gateway"},
					},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "auth-actionset"},
							{Name: "ratelimit-actionset"},
							{Name: "logging-actionset"},
							{Name: "metrics-actionset"},
							{Name: "tracing-actionset"},
						},
						map[string]wasm.Service{
							"auth-service": {
								Endpoint:    "http://auth-service.auth-ns.svc.cluster.local:8080",
								Type:        wasm.AuthServiceType,
								FailureMode: wasm.FailureModeDeny,
								Timeout:     ptr("1000ms"),
								GrpcService: ptr("auth.v1.AuthService"),
								GrpcMethod:  ptr("Check"),
							},
							"ratelimit-service": {
								Endpoint:    "http://limitador.limitador-ns.svc.cluster.local:8081",
								Type:        wasm.RateLimitServiceType,
								FailureMode: wasm.FailureModeAllow,
								Timeout:     ptr("500ms"),
								GrpcService: ptr("envoy.service.ratelimit.v3.RateLimitService"),
								GrpcMethod:  ptr("ShouldRateLimit"),
							},
							"metrics-service": {
								Endpoint:    "http://metrics.monitoring.svc.cluster.local:9090",
								Type:        wasm.RateLimitServiceType,
								FailureMode: wasm.FailureModeAllow,
								Timeout:     ptr("200ms"),
							},
						},
						map[string]string{
							"tenant_id":        "tenant-123",
							"environment":      "production",
							"region":           "us-east-1",
							"cluster":          "main-cluster",
							"version":          "v2.1.0",
							"feature_flag_1":   "enabled",
							"feature_flag_2":   "disabled",
							"metadata_key_1":   "metadata_value_1",
							"metadata_key_2":   "metadata_value_2",
							"custom_header":    "X-Custom-Header",
							"log_level":        "info",
							"trace_sampling":   "0.1",
							"circuit_breaker":  "enabled",
							"retry_policy":     "exponential",
							"timeout_override": "5s",
						},
					)),
				},
			},
			want: true,
		},
		{
			name: "very complex PluginConfig with one subtle difference in GrpcService",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://registry.example.com/wasm-shim:v2.1.0",
					ImagePullSecret: "registry-secret",
					Phase:           istioextensionsv1alpha1.PluginPhase_AUTHN,
					TargetRefs: []*istiov1beta1.PolicyTargetReference{
						{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "prod-gateway"},
					},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "auth-actionset"},
							{Name: "ratelimit-actionset"},
						},
						map[string]wasm.Service{
							"auth-service": {
								Endpoint:    "http://auth-service.auth-ns.svc.cluster.local:8080",
								Type:        wasm.AuthServiceType,
								FailureMode: wasm.FailureModeDeny,
								Timeout:     ptr("1000ms"),
								GrpcService: ptr("auth.v1.AuthService"),
								GrpcMethod:  ptr("Check"),
							},
						},
						map[string]string{
							"tenant_id":   "tenant-123",
							"environment": "production",
						},
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://registry.example.com/wasm-shim:v2.1.0",
					ImagePullSecret: "registry-secret",
					Phase:           istioextensionsv1alpha1.PluginPhase_AUTHN,
					TargetRefs: []*istiov1beta1.PolicyTargetReference{
						{Group: "gateway.networking.k8s.io", Kind: "Gateway", Name: "prod-gateway"},
					},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{
							{Name: "auth-actionset"},
							{Name: "ratelimit-actionset"},
						},
						map[string]wasm.Service{
							"auth-service": {
								Endpoint:    "http://auth-service.auth-ns.svc.cluster.local:8080",
								Type:        wasm.AuthServiceType,
								FailureMode: wasm.FailureModeDeny,
								Timeout:     ptr("1000ms"),
								GrpcService: ptr("auth.v2.AuthService"), // Different version
								GrpcMethod:  ptr("Check"),
							},
						},
						map[string]string{
							"tenant_id":   "tenant-123",
							"environment": "production",
						},
					)),
				},
			},
			want: false,
		},
		{
			name: "multiple differences - URL, Phase, and PluginConfig",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "secret1",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test1"}},
						nil,
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v2",
					ImagePullSecret: "secret1",
					Phase:           istioextensionsv1alpha1.PluginPhase_AUTHN,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test2"}},
						nil,
						nil,
					)),
				},
			},
			want: false,
		},
		{
			name: "edge case - Service with nil vs non-nil optional fields",
			a: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {
								Endpoint:    "http://service1:8080",
								Type:        wasm.RateLimitServiceType,
								FailureMode: wasm.FailureModeAllow,
								Timeout:     nil,
								GrpcService: nil,
								GrpcMethod:  nil,
							},
						},
						nil,
					)),
				},
			},
			b: &istioclientgoextensionv1alpha1.WasmPlugin{
				Spec: istioextensionsv1alpha1.WasmPlugin{
					Url:             "oci://example.com/wasm:v1",
					ImagePullSecret: "",
					Phase:           istioextensionsv1alpha1.PluginPhase_STATS,
					TargetRefs:      []*istiov1beta1.PolicyTargetReference{},
					PluginConfig: configToStruct(createWasmConfig(
						[]wasm.ActionSet{{Name: "test"}},
						map[string]wasm.Service{
							"service1": {
								Endpoint:    "http://service1:8080",
								Type:        wasm.RateLimitServiceType,
								FailureMode: wasm.FailureModeAllow,
								Timeout:     ptr("500ms"),
								GrpcService: nil,
								GrpcMethod:  nil,
							},
						},
						nil,
					)),
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := equalWasmPlugins(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("equalWasmPlugins() = %v, want %v", got, tt.want)
			}
		})
	}
}
