//go:build unit

package controllers

import (
	"context"
	"testing"

	"gotest.tools/assert"

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

func TestMergeAndVerifyExtensionActions(t *testing.T) {
	tests := []struct {
		name        string
		actions     []wasm.Action
		expectedLen int
		description string
		validate    func(*testing.T, []wasm.Action)
	}{
		{
			name: "extension action before auth - preserved order",
			actions: []wasm.Action{
				{
					ServiceName:       "ext-a1b2c3",
					Scope:             "threat-assess",
					Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
					ResponsePredicate: "service.response.threat_level < 5",
					DenialResponse: &wasm.DenialResponse{
						StatusCode: 403,
						Headers:    map[string]string{"X-Deny-Reason": "threat-level-exceeded"},
						Body:       "blocked",
					},
					SourcePolicyLocators: []string{"ThreatPolicy/default/my-policy"},
				},
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "auth-scope",
				},
			},
			expectedLen: 2,
			description: "extension action should remain before auth action",
			validate: func(t *testing.T, result []wasm.Action) {
				assert.Equal(t, "ext-a1b2c3", result[0].ServiceName)
				assert.Equal(t, wasm.AuthServiceName, result[1].ServiceName)
			},
		},
		{
			name: "extension action + auth + ratelimit - ordering preserved",
			actions: []wasm.Action{
				{
					ServiceName:          "ext-a1b2c3",
					Scope:                "threat-assess",
					Dispatch:             `threat.v1.AssessRequest{uri: request.url_path}`,
					ResponsePredicate:    "service.response.threat_level < 5",
					SourcePolicyLocators: []string{"ThreatPolicy/default/my-policy"},
				},
				{
					ServiceName: wasm.AuthServiceName,
					Scope:       "auth-scope",
				},
				{
					ServiceName: wasm.RateLimitServiceName,
					Scope:       "rl-scope",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{Value: &wasm.Static{Static: wasm.StaticSpec{Key: "limit.key", Value: "1"}}},
							},
						},
					},
				},
			},
			expectedLen: 3,
			description: "all three action types should remain in order",
			validate: func(t *testing.T, result []wasm.Action) {
				assert.Equal(t, "ext-a1b2c3", result[0].ServiceName)
				assert.Equal(t, wasm.AuthServiceName, result[1].ServiceName)
				assert.Equal(t, wasm.RateLimitServiceName, result[2].ServiceName)
			},
		},
		{
			name: "two extension actions with different scopes - not merged",
			actions: []wasm.Action{
				{
					ServiceName:          "ext-a1b2c3",
					Scope:                "threat-assess",
					Dispatch:             `threat.v1.AssessRequest{uri: request.url_path}`,
					ResponsePredicate:    "service.response.threat_level < 5",
					SourcePolicyLocators: []string{"ThreatPolicy/default/policy-1"},
				},
				{
					ServiceName:          "ext-d4e5f6",
					Scope:                "fraud-check",
					Dispatch:             `fraud.v1.CheckRequest{user_id: request.headers["x-user-id"]}`,
					ResponsePredicate:    "service.response.fraud_score < 80",
					SourcePolicyLocators: []string{"FraudPolicy/default/policy-2"},
				},
			},
			expectedLen: 2,
			description: "extension actions with different scopes and services stay separate",
			validate: func(t *testing.T, result []wasm.Action) {
				assert.Equal(t, "ext-a1b2c3", result[0].ServiceName)
				assert.Equal(t, "ext-d4e5f6", result[1].ServiceName)
				assert.Equal(t, "threat-assess", result[0].Scope)
				assert.Equal(t, "fraud-check", result[1].Scope)
			},
		},
		{
			name: "multiple extension actions from different policies with own source locators",
			actions: []wasm.Action{
				{
					ServiceName:          "ext-a1b2c3",
					Scope:                "threat-assess-1",
					Dispatch:             `threat.v1.AssessRequest{uri: request.url_path}`,
					ResponsePredicate:    "service.response.threat_level < 5",
					SourcePolicyLocators: []string{"ThreatPolicy/ns1/policy-a"},
				},
				{
					ServiceName:          "ext-a1b2c3",
					Scope:                "threat-assess-2",
					Dispatch:             `threat.v1.AssessRequest{uri: request.url_path}`,
					ResponsePredicate:    "service.response.threat_level < 3",
					SourcePolicyLocators: []string{"ThreatPolicy/ns2/policy-b"},
				},
			},
			expectedLen: 2,
			description: "two extension actions from different policies stay separate with their own source locators",
			validate: func(t *testing.T, result []wasm.Action) {
				assert.Equal(t, 1, len(result[0].SourcePolicyLocators))
				assert.Equal(t, "ThreatPolicy/ns1/policy-a", result[0].SourcePolicyLocators[0])
				assert.Equal(t, 1, len(result[1].SourcePolicyLocators))
				assert.Equal(t, "ThreatPolicy/ns2/policy-b", result[1].SourcePolicyLocators[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeAndVerify(context.TODO(), tt.actions)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assert.Equal(t, tt.expectedLen, len(result), tt.description)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}
