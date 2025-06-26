package controllers

import (
	"testing"

	"github.com/kuadrant/kuadrant-operator/internal/wasm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeAndVerify(t *testing.T) {
	tests := []struct {
		name          string
		actions       []wasm.Action
		expectedError string
		expectedLen   int
		description   string
	}{
		{
			name:        "empty actions",
			actions:     []wasm.Action{},
			expectedLen: 0,
			description: "should return empty slice when no actions provided",
		},
		{
			name: "single action",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
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
			description: "should return single rate limit action unchanged",
		},
		{
			name: "actions with different scopes - no merge",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
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
					ServiceName: "ratelimit-service",
					Scope:       "namespace",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "api_key",
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
			name: "actions with different service names - no merge",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "ratelimit.limit_key",
											Value: "api_key",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: "auth-service",
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
			},
			expectedLen: 2,
			description: "should not merge actions with different service names (rate limit vs auth)",
		},
		{
			name: "successful merge - same scope and service",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
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
					ServiceName: "ratelimit-service",
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
			description: "should merge rate limit actions with same scope and service name",
		},
		{
			name: "successful merge - duplicate keys with same values",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
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
					ServiceName: "ratelimit-service",
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
			description: "should allow duplicate keys with same values in rate limit actions",
		},
		{
			name: "error - duplicate keys with different values",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
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
					ServiceName: "ratelimit-service",
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
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
			description:   "should detect duplicate keys with different values",
		},
		{
			name: "mixed data types - successful merge",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
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
					ServiceName: "ratelimit-service",
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "responseBodyJSON(\"usage.total_tokens\")",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 1,
			description: "should successfully merge rate limit actions with different data types",
		},
		{
			name: "multiple actions with complex merge",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
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
					ServiceName: "ratelimit-service",
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "responseBodyJSON(\"usage.total_tokens\")",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: "ratelimit-service",
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "limit",
											Value: "100",
										},
									},
								},
							},
						},
					},
				},
				{
					ServiceName: "ratelimit-service",
					Scope:       "namespace",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Static{
										Static: wasm.StaticSpec{
											Key:   "namespace",
											Value: "test",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 2,
			description: "should properly merge and separate rate limit actions based on scope and service",
		},
		{
			name: "actions with duplicate hits.addend and different values ",
			actions: []wasm.Action{
				{
					ServiceName: "ratelimit-service",
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
					ServiceName: "ratelimit-service",
					Scope:       "global",
					ConditionalData: []wasm.ConditionalData{
						{
							Data: []wasm.DataType{
								{
									Value: &wasm.Expression{
										ExpressionItem: wasm.ExpressionItem{
											Key:   "ratelimit.hits_addend",
											Value: "responseBodyJSON(\"usage.total_tokens\")",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen: 1,
			description: "should merge and contain both hits.addend values in the same action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeAndVerify(tt.actions)

			if tt.expectedError != "" {
				require.Error(t, err, tt.description)
				assert.Contains(t, err.Error(), tt.expectedError, "error message should contain expected text")
				return
			}

			require.NoError(t, err, tt.description)
			assert.Equal(t, tt.expectedLen, len(result), tt.description)
		})
	}
}

func TestMergeAndVerifyEdgeCases(t *testing.T) {
	t.Run("empty conditional data", func(t *testing.T) {
		actions := []wasm.Action{
			{
				ServiceName:     "ratelimit-service",
				Scope:           "global",
				ConditionalData: []wasm.ConditionalData{},
			},
			{
				ServiceName:     "ratelimit-service",
				Scope:           "global",
				ConditionalData: []wasm.ConditionalData{},
			},
		}

		result, err := mergeAndVerify(actions)
		require.NoError(t, err)
		assert.Equal(t, 1, len(result))
	})

	t.Run("empty data in conditional data", func(t *testing.T) {
		actions := []wasm.Action{
			{
				ServiceName: "ratelimit-service",
				Scope:       "global",
				ConditionalData: []wasm.ConditionalData{
					{
						Data: []wasm.DataType{},
					},
				},
			},
		}

		result, err := mergeAndVerify(actions)
		require.NoError(t, err)
		assert.Equal(t, 1, len(result))
	})

	t.Run("empty keys are handled", func(t *testing.T) {
		actions := []wasm.Action{
			{
				ServiceName: "ratelimit-service",
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
				ServiceName: "ratelimit-service",
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

		_, err := mergeAndVerify(actions)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key '' with different values")
	})

	t.Run("duplicate keys within same conditional data", func(t *testing.T) {
		actions := []wasm.Action{
			{
				ServiceName: "ratelimit-service",
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
							{
								Value: &wasm.Static{
									Static: wasm.StaticSpec{
										Key:   "ratelimit.hits_addend",
										Value: "10",
									},
								},
							},
						},
					},
				},
			},
		}

		_, err := mergeAndVerify(actions)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key 'ratelimit.hits_addend' with different values")
	})
}
