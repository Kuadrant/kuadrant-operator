//go:build unit

package wasm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

func TestConfigToJSON(t *testing.T) {
	config := testBasicConfig
	j, err := config.ToJSON()
	if err != nil {
		t.Fatal(err)
	}

	convertedConfig, _ := ConfigFromJSON(j)

	if !cmp.Equal(convertedConfig, testBasicConfig) {
		diff := cmp.Diff(convertedConfig, testBasicConfig)
		t.Fatalf("unexpected converted wasm config (-want +got):\n%s", diff)
	}
}

func TestConfigToStruct(t *testing.T) {
	config := testBasicConfig
	s, err := config.ToStruct()
	if err != nil {
		t.Fatal(err)
	}

	convertedConfig, _ := ConfigFromStruct(s)

	if !cmp.Equal(testBasicConfig, convertedConfig) {
		diff := cmp.Diff(testBasicConfig, convertedConfig)
		t.Fatalf("unexpected converted wasm config (-want +got):\n%s", diff)
	}
}

func TestConfigEqual(t *testing.T) {
	testCases := []struct {
		name     string
		config1  *Config
		config2  *Config
		expected bool
	}{
		{
			name: "equal configs",
			config1: &Config{
				RequestData: map[string]string{
					"metrics.labels.user":  "auth.identity.user",
					"metrics.labels.group": "auth.identity.group",
				},
				Services: map[string]Service{
					"ratelimit-service": {
						Type:        "ratelimit",
						Endpoint:    "kuadrant-ratelimit-service",
						FailureMode: "allow",
						Timeout:     ptr.To("100ms"),
					},
				},
				ActionSets: []ActionSet{
					{
						Name: "5755da0b3c275ba6b8f553890eb32b04768a703b60ab9a5d7f4e0948e23ef0ab",
						RouteRuleConditions: RouteRuleConditions{
							Hostnames: []string{"other.example.com"},
							Predicates: []string{
								"request.url_path.startsWith('/')",
							},
						},
						Actions: []Action{
							{
								ServiceName: "ratelimit-service",
								Scope:       "default/other",
								ConditionalData: []ConditionalData{
									{
										Predicates: []string{
											"source.address != '127.0.0.1'",
										},
										Data: []DataType{
											{
												Value: &Static{
													Static: StaticSpec{
														Key:   "limit.global__f63bec56",
														Value: "1",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			config2: &Config{ // same config as config1 with fields sorted alphabetically
				ActionSets: []ActionSet{
					{
						Actions: []Action{
							{
								ConditionalData: []ConditionalData{
									{
										Predicates: []string{
											"source.address != '127.0.0.1'",
										},
										Data: []DataType{
											{
												Value: &Static{
													Static: StaticSpec{
														Key:   "limit.global__f63bec56",
														Value: "1",
													},
												},
											},
										},
									},
								},
								ServiceName: "ratelimit-service",
								Scope:       "default/other",
							},
						},
						Name: "5755da0b3c275ba6b8f553890eb32b04768a703b60ab9a5d7f4e0948e23ef0ab",
						RouteRuleConditions: RouteRuleConditions{
							Hostnames: []string{"other.example.com"},
							Predicates: []string{
								"request.url_path.startsWith('/')",
							},
						},
					},
				},
				RequestData: map[string]string{
					"metrics.labels.group": "auth.identity.group",
					"metrics.labels.user":  "auth.identity.user",
				},
				Services: map[string]Service{
					"ratelimit-service": {
						Type:        "ratelimit",
						Endpoint:    "kuadrant-ratelimit-service",
						FailureMode: "allow",
						Timeout:     ptr.To("100ms"),
					},
				},
			},
			expected: true,
		},
		{
			name:     "different configs",
			config1:  testBasicConfig,
			config2:  &Config{},
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			if tc.config1.EqualTo(tc.config2) != tc.expected {
				diff := cmp.Diff(tc.config1, tc.config2)
				subT.Fatalf("unexpected config equality result (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMarshallUnmarshalConfig(t *testing.T) {
	config := testBasicConfig

	marshalledConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshalledConfig Config
	if err := json.Unmarshal(marshalledConfig, &unmarshalledConfig); err != nil {
		t.Fatal(err)
	}

	if !cmp.Equal(config, &unmarshalledConfig) {
		diff := cmp.Diff(config, &unmarshalledConfig)
		t.Fatalf("unexpected wasm config (-want +got):\n%s", diff)
	}
}

func TestValidAction(t *testing.T) {
	testCases := []struct {
		name           string
		yaml           string
		expectedAction *Action
	}{
		{
			name: "valid empty data",
			expectedAction: &Action{
				ServiceName: "ratelimit-service",
				Scope:       "some-scope",
			},
			yaml: `
service: ratelimit-service
scope: some-scope
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			var action Action
			if err := yaml.Unmarshal([]byte(tc.yaml), &action); err != nil {
				subT.Fatal(err)
			}
			if !cmp.Equal(tc.expectedAction, &action) {
				diff := cmp.Diff(tc.expectedAction, &action)
				subT.Fatalf("unexpected wasm action (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInvalidAction(t *testing.T) {
	testCases := []struct {
		name string
		yaml string
	}{
		{
			name: "unknown data type",
			yaml: `
service: ratelimit-service
scope: some-scope
conditionalData:
  - data:
    - other:
        key: keyA
`,
		},
		{
			name: "both data types at the same time",
			yaml: `
service: ratelimit-service
scope: some-scope
conditionalData:
  - data:
    - static:
        key: keyA
      selector:
        selector: selectorA
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			var action Action
			if err := yaml.Unmarshal([]byte(tc.yaml), &action); err == nil {
				subT.Fatal("unmashall should fail")
			}
		})
	}
}

func TestActionWithDynamicFields(t *testing.T) {
	testCases := []struct {
		name   string
		action Action
	}{
		{
			name: "action with dispatch only",
			action: Action{
				ServiceName: "ext-threat-svc-8080",
				Scope:       "threat-assess",
				Dispatch:    `threat.v1.AssessRequest{uri: request.url_path, method: request.method}`,
			},
		},
		{
			name: "action with response predicate only",
			action: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				ResponsePredicate: "service.response.threat_level < 5",
			},
		},
		{
			name: "action with denial response only",
			action: Action{
				ServiceName: "ext-threat-svc-8080",
				Scope:       "threat-assess",
				DenialResponse: &DenialResponse{
					StatusCode: 403,
					Headers:    map[string]string{"X-Deny-Reason": "threat-level-exceeded"},
					Body:       "Request blocked: threat level exceeded threshold",
				},
			},
		},
		{
			name: "action with all new fields",
			action: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Dispatch:          `threat.v1.AssessRequest{uri: request.url_path, source_ip: source.address}`,
				ResponsePredicate: "service.response.threat_level < 5",
				DenialResponse: &DenialResponse{
					StatusCode: 403,
					Headers:    map[string]string{"X-Deny-Reason": "threat-level-exceeded"},
					Body:       "Request blocked: threat level exceeded threshold",
				},
				SourcePolicyLocators: []string{"ThreatPolicy/default/my-threat-policy"},
			},
		},
		{
			name: "action with new and existing fields",
			action: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Predicates:        []string{"request.method == 'POST'"},
				Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
				ResponsePredicate: "service.response.threat_level < 5",
				DenialResponse: &DenialResponse{
					StatusCode: 403,
					Headers:    map[string]string{"X-Deny-Reason": "threat-level-exceeded"},
					Body:       "Request blocked",
				},
				ConditionalData: []ConditionalData{
					{
						Data: []DataType{
							{Value: &Static{Static: StaticSpec{Key: "key", Value: "val"}}},
						},
					},
				},
				SourcePolicyLocators: []string{"ThreatPolicy/default/my-threat-policy"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			marshalled, err := json.Marshal(tc.action)
			if err != nil {
				subT.Fatalf("failed to marshal action: %v", err)
			}

			var unmarshalled Action
			if err := json.Unmarshal(marshalled, &unmarshalled); err != nil {
				subT.Fatalf("failed to unmarshal action: %v", err)
			}

			if !cmp.Equal(&tc.action, &unmarshalled) {
				diff := cmp.Diff(&tc.action, &unmarshalled)
				subT.Fatalf("action round-trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestActionDynamicFieldsOmitEmpty(t *testing.T) {
	action := Action{
		ServiceName: "ratelimit-service",
		Scope:       "default/toystore",
	}

	marshalled, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	jsonStr := string(marshalled)

	for _, field := range []string{"dispatch", "responsePredicate", "denialResponse"} {
		if strings.Contains(jsonStr, field) {
			t.Errorf("expected %q to be omitted from JSON, got: %s", field, jsonStr)
		}
	}
}

func TestActionEqualToWithDynamicFields(t *testing.T) {
	base := Action{
		ServiceName:       "ext-threat-svc-8080",
		Scope:             "threat-assess",
		Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
		ResponsePredicate: "service.response.threat_level < 5",
		DenialResponse: &DenialResponse{
			StatusCode: 403,
			Headers:    map[string]string{"X-Deny-Reason": "blocked"},
			Body:       "denied",
		},
	}

	testCases := []struct {
		name     string
		other    Action
		expected bool
	}{
		{
			name: "identical actions are equal",
			other: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
				ResponsePredicate: "service.response.threat_level < 5",
				DenialResponse: &DenialResponse{
					StatusCode: 403,
					Headers:    map[string]string{"X-Deny-Reason": "blocked"},
					Body:       "denied",
				},
			},
			expected: true,
		},
		{
			name: "different dispatch",
			other: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Dispatch:          `threat.v1.AssessRequest{uri: request.method}`,
				ResponsePredicate: "service.response.threat_level < 5",
				DenialResponse: &DenialResponse{
					StatusCode: 403,
					Headers:    map[string]string{"X-Deny-Reason": "blocked"},
					Body:       "denied",
				},
			},
			expected: false,
		},
		{
			name: "different response predicate",
			other: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
				ResponsePredicate: "service.response.threat_level < 10",
				DenialResponse: &DenialResponse{
					StatusCode: 403,
					Headers:    map[string]string{"X-Deny-Reason": "blocked"},
					Body:       "denied",
				},
			},
			expected: false,
		},
		{
			name: "different denial status code",
			other: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
				ResponsePredicate: "service.response.threat_level < 5",
				DenialResponse: &DenialResponse{
					StatusCode: 429,
					Headers:    map[string]string{"X-Deny-Reason": "blocked"},
					Body:       "denied",
				},
			},
			expected: false,
		},
		{
			name: "different denial headers",
			other: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
				ResponsePredicate: "service.response.threat_level < 5",
				DenialResponse: &DenialResponse{
					StatusCode: 403,
					Headers:    map[string]string{"X-Deny-Reason": "other-reason"},
					Body:       "denied",
				},
			},
			expected: false,
		},
		{
			name: "different denial body",
			other: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
				ResponsePredicate: "service.response.threat_level < 5",
				DenialResponse: &DenialResponse{
					StatusCode: 403,
					Headers:    map[string]string{"X-Deny-Reason": "blocked"},
					Body:       "other body",
				},
			},
			expected: false,
		},
		{
			name: "nil vs non-nil denial response",
			other: Action{
				ServiceName:       "ext-threat-svc-8080",
				Scope:             "threat-assess",
				Dispatch:          `threat.v1.AssessRequest{uri: request.url_path}`,
				ResponsePredicate: "service.response.threat_level < 5",
				DenialResponse:    nil,
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := base.EqualTo(tc.other)
			if result != tc.expected {
				subT.Fatalf("expected EqualTo=%v, got %v", tc.expected, result)
			}
		})
	}
}

func TestDenialResponseEqualTo(t *testing.T) {
	testCases := []struct {
		name     string
		d1       *DenialResponse
		d2       *DenialResponse
		expected bool
	}{
		{
			name:     "both nil",
			d1:       nil,
			d2:       nil,
			expected: true,
		},
		{
			name:     "first nil second non-nil",
			d1:       nil,
			d2:       &DenialResponse{StatusCode: 403},
			expected: false,
		},
		{
			name:     "first non-nil second nil",
			d1:       &DenialResponse{StatusCode: 403},
			d2:       nil,
			expected: false,
		},
		{
			name:     "both empty",
			d1:       &DenialResponse{},
			d2:       &DenialResponse{},
			expected: true,
		},
		{
			name:     "same values",
			d1:       &DenialResponse{StatusCode: 403, Headers: map[string]string{"X-Reason": "blocked"}, Body: "denied"},
			d2:       &DenialResponse{StatusCode: 403, Headers: map[string]string{"X-Reason": "blocked"}, Body: "denied"},
			expected: true,
		},
		{
			name:     "different status code",
			d1:       &DenialResponse{StatusCode: 403},
			d2:       &DenialResponse{StatusCode: 429},
			expected: false,
		},
		{
			name:     "different headers count",
			d1:       &DenialResponse{StatusCode: 403, Headers: map[string]string{"a": "1"}},
			d2:       &DenialResponse{StatusCode: 403, Headers: map[string]string{"a": "1", "b": "2"}},
			expected: false,
		},
		{
			name:     "different header values",
			d1:       &DenialResponse{StatusCode: 403, Headers: map[string]string{"a": "1"}},
			d2:       &DenialResponse{StatusCode: 403, Headers: map[string]string{"a": "2"}},
			expected: false,
		},
		{
			name:     "different body",
			d1:       &DenialResponse{StatusCode: 403, Body: "denied"},
			d2:       &DenialResponse{StatusCode: 403, Body: "blocked"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := tc.d1.EqualTo(tc.d2)
			if result != tc.expected {
				subT.Fatalf("expected EqualTo=%v, got %v", tc.expected, result)
			}
		})
	}
}

func TestConfigWithDynamicServiceRoundTrip(t *testing.T) {
	config := &Config{
		Services: map[string]Service{
			"auth-service": {
				Type:        AuthServiceType,
				Endpoint:    "kuadrant-auth-service",
				FailureMode: FailureModeDeny,
				Timeout:     ptr.To("200ms"),
			},
			"ext-a1b2c3": {
				Type:        DynamicServiceType,
				Endpoint:    "ext-threat-service-security-svc-cluster-local-8080",
				FailureMode: FailureModeDeny,
				Timeout:     ptr.To("100ms"),
			},
		},
		ActionSets: []ActionSet{
			{
				Name: "test-actionset",
				RouteRuleConditions: RouteRuleConditions{
					Hostnames: []string{"api.example.com"},
				},
				Actions: []Action{
					{
						ServiceName:       "ext-a1b2c3",
						Scope:             "threat-assess",
						Dispatch:          `threat.v1.AssessRequest{uri: request.url_path, method: request.method, source_ip: source.address}`,
						ResponsePredicate: "service.response.threat_level < 5",
						DenialResponse: &DenialResponse{
							StatusCode: 403,
							Headers:    map[string]string{"X-Deny-Reason": "threat-level-exceeded"},
							Body:       "Request blocked: threat level exceeded threshold",
						},
						SourcePolicyLocators: []string{"ThreatPolicy/default/api-protection"},
					},
					{
						ServiceName: "auth-service",
						Scope:       "auth-scope",
					},
				},
			},
		},
	}

	t.Run("ToJSON and ConfigFromJSON round-trip", func(subT *testing.T) {
		j, err := config.ToJSON()
		if err != nil {
			subT.Fatalf("ToJSON failed: %v", err)
		}

		converted, err := ConfigFromJSON(j)
		if err != nil {
			subT.Fatalf("ConfigFromJSON failed: %v", err)
		}

		if !cmp.Equal(config, converted) {
			diff := cmp.Diff(config, converted)
			subT.Fatalf("JSON round-trip mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("ToStruct and ConfigFromStruct round-trip", func(subT *testing.T) {
		s, err := config.ToStruct()
		if err != nil {
			subT.Fatalf("ToStruct failed: %v", err)
		}

		converted, err := ConfigFromStruct(s)
		if err != nil {
			subT.Fatalf("ConfigFromStruct failed: %v", err)
		}

		if !cmp.Equal(config, converted) {
			diff := cmp.Diff(config, converted)
			subT.Fatalf("Struct round-trip mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestDynamicServiceTypeConstant(t *testing.T) {
	if DynamicServiceType != "dynamic" {
		t.Fatalf("expected DynamicServiceType to be %q, got %q", "dynamic", DynamicServiceType)
	}

	svc := Service{
		Type:        DynamicServiceType,
		Endpoint:    "ext-test",
		FailureMode: FailureModeDeny,
		Timeout:     ptr.To("100ms"),
	}

	marshalled, err := json.Marshal(svc)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if !strings.Contains(string(marshalled), `"type":"dynamic"`) {
		t.Fatalf("expected JSON to contain '\"type\":\"dynamic\"', got: %s", string(marshalled))
	}
}

func TestAuthAccesses(t *testing.T) {
	action := Action{
		ServiceName: "ratelimit-service",
		Scope:       "default/other",
		ConditionalData: []ConditionalData{
			{
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.global__f63bec56",
								Value: "1",
							},
						},
					},
				},
			},
		},
	}

	if action.HasAuthAccess() {
		t.Fatal("must not have auth access")
	}

	action = Action{
		ServiceName: "ratelimit-service",
		Scope:       "default/other",
		ConditionalData: []ConditionalData{
			{
				Predicates: []string{
					"auth.something != '127.0.0.1'",
				},
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.global__f63bec56",
								Value: "1",
							},
						},
					},
				},
			},
		},
	}

	if !action.HasAuthAccess() {
		t.Fatal("must have auth access")
	}

	action = Action{
		ServiceName: "ratelimit-service",
		Scope:       "default/other",
		ConditionalData: []ConditionalData{
			{
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
				Data: []DataType{
					{
						Value: &Expression{
							ExpressionItem: ExpressionItem{
								Key:   "limit.global__f63bec56",
								Value: "auth.identity.anonymous",
							},
						},
					},
				},
			},
		},
	}

	if !action.HasAuthAccess() {
		t.Fatal("must have auth access")
	}

	action = Action{
		ServiceName: "ratelimit-service",
		Scope:       "default/other",
		ConditionalData: []ConditionalData{
			{
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "auth.global__f63bec56",
								Value: "auth",
							},
						},
					},
				},
			},
		},
	}

	if action.HasAuthAccess() {
		t.Fatal("must not have auth access")
	}
}
