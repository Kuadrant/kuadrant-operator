//go:build unit

package wasm

import (
	"encoding/json"
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
						Type:        "ratelimit-check",
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
						Type:        "ratelimit-check",
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
		{
			name: "different descriptor service",
			config1: &Config{
				DescriptorService: "kuadrant-operator-grpc",
			},
			config2: &Config{
				DescriptorService: "different-descriptor-service",
			},
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

func TestTypedAction_JSON(t *testing.T) {
	ta := TypedAction{
		Type:                 "grpc",
		Predicate:            `"x-assess-threat" in request.headers`,
		Terminal:             false,
		Var:                  "threatResponse",
		Service:              "ext-abc123",
		MessageBuilder:       "threat.v1.Request{path: request.path}",
		SourcePolicyLocators: []string{"ThreatPolicy/default/my-threat"},
		OnReply: []TypedAction{
			{
				Type:      "deny",
				Predicate: "!(threatResponse.threat_level < 5)",
				Terminal:  true,
				DenyWith:  "DenyResponse{status: 403u}",
			},
			{
				Type:      "headers",
				Predicate: "true",
				Terminal:  false,
				Target:    "response",
				Headers:   `{"x-threat-assessed": "true"}`,
			},
		},
	}

	data, err := json.Marshal(ta)
	if err != nil {
		t.Fatalf("failed to marshal TypedAction: %v", err)
	}

	var roundTripped TypedAction
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("failed to unmarshal TypedAction: %v", err)
	}

	if !ta.EqualTo(roundTripped) {
		t.Fatalf("round-tripped TypedAction not equal:\n  got:  %+v\n  want: %+v", roundTripped, ta)
	}

	// Verify JSON has expected top-level fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}
	if raw["type"] != "grpc" {
		t.Errorf("Expected type 'grpc', got %v", raw["type"])
	}
	if raw["var"] != "threatResponse" {
		t.Errorf("Expected var 'threatResponse', got %v", raw["var"])
	}
	onReply, ok := raw["onReply"].([]interface{})
	if !ok || len(onReply) != 2 {
		t.Fatalf("Expected onReply with 2 elements, got %v", raw["onReply"])
	}
}

func TestTypedAction_EqualTo(t *testing.T) {
	base := TypedAction{
		Type:                 "grpc",
		Predicate:            "true",
		Terminal:             false,
		Var:                  "resp",
		Service:              "ext-svc",
		SourcePolicyLocators: []string{"Policy/ns/name"},
		OnReply: []TypedAction{
			{Type: "deny", Predicate: "!(resp.ok)", Terminal: true, DenyWith: "DenyResponse{status: 403u}"},
		},
	}

	same := TypedAction{
		Type:                 "grpc",
		Predicate:            "true",
		Terminal:             false,
		Var:                  "resp",
		Service:              "ext-svc",
		SourcePolicyLocators: []string{"Policy/ns/name"},
		OnReply: []TypedAction{
			{Type: "deny", Predicate: "!(resp.ok)", Terminal: true, DenyWith: "DenyResponse{status: 403u}"},
		},
	}

	if !base.EqualTo(same) {
		t.Fatal("identical TypedActions should be equal")
	}

	diffVar := same
	diffVar.Var = "other"
	if base.EqualTo(diffVar) {
		t.Fatal("TypedActions with different Var should not be equal")
	}

	diffOnReply := same
	diffOnReply.OnReply = nil
	if base.EqualTo(diffOnReply) {
		t.Fatal("TypedActions with different OnReply length should not be equal")
	}
}

func TestTypedAction_FailType_JSON(t *testing.T) {
	ta := TypedAction{
		Type:       "fail",
		Predicate:  `threatResponse.error_code != 0`,
		Terminal:   true,
		LogMessage: "Threat service returned unexpected error",
	}

	data, err := json.Marshal(ta)
	if err != nil {
		t.Fatalf("failed to marshal TypedAction: %v", err)
	}

	var roundTripped TypedAction
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("failed to unmarshal TypedAction: %v", err)
	}

	if !ta.EqualTo(roundTripped) {
		t.Fatalf("round-tripped TypedAction not equal:\n  got:  %+v\n  want: %+v", roundTripped, ta)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}
	if raw["type"] != "fail" {
		t.Errorf("Expected type 'fail', got %v", raw["type"])
	}
	if raw["logMessage"] != "Threat service returned unexpected error" {
		t.Errorf("Expected logMessage, got %v", raw["logMessage"])
	}
}

func TestTypedAction_EqualTo_FailFields(t *testing.T) {
	a := TypedAction{Type: "fail", Predicate: "true", Terminal: true, LogMessage: "error"}
	b := TypedAction{Type: "fail", Predicate: "true", Terminal: true, LogMessage: "error"}
	if !a.EqualTo(b) {
		t.Fatal("identical fail TypedActions should be equal")
	}

	diffMsg := b
	diffMsg.LogMessage = "other"
	if a.EqualTo(diffMsg) {
		t.Fatal("TypedActions with different LogMessage should not be equal")
	}
}

func TestActionSet_MixedActions_JSON(t *testing.T) {
	as := ActionSet{
		Name: "test-set",
		RouteRuleConditions: RouteRuleConditions{
			Hostnames: []string{"api.example.com"},
		},
		Actions: []Action{
			{ServiceName: "auth-service", Scope: "default/route"},
		},
		TypedActions: []TypedAction{
			{Type: "deny", Predicate: "!(request.ok)", Terminal: true, DenyWith: "DenyResponse{status: 403u}"},
		},
	}

	data, err := json.Marshal(as)
	if err != nil {
		t.Fatalf("failed to marshal ActionSet: %v", err)
	}

	// Verify the JSON has a single "actions" array with 2 items
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}
	actions, ok := raw["actions"].([]interface{})
	if !ok {
		t.Fatalf("Expected actions array, got %T", raw["actions"])
	}
	if len(actions) != 2 {
		t.Fatalf("Expected 2 items in actions array, got %d", len(actions))
	}

	// First should be legacy (has "service" field)
	first := actions[0].(map[string]interface{})
	if _, hasService := first["service"]; !hasService {
		t.Error("Expected first action to have 'service' field (legacy)")
	}

	// Second should be typed (has "type" field)
	second := actions[1].(map[string]interface{})
	if second["type"] != "deny" {
		t.Errorf("Expected second action type 'deny', got %v", second["type"])
	}

	// Round-trip
	var roundTripped ActionSet
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("failed to unmarshal ActionSet: %v", err)
	}
	if !as.EqualTo(roundTripped) {
		t.Fatalf("round-tripped ActionSet not equal")
	}
	if len(roundTripped.Actions) != 1 {
		t.Fatalf("Expected 1 legacy action after unmarshal, got %d", len(roundTripped.Actions))
	}
	if len(roundTripped.TypedActions) != 1 {
		t.Fatalf("Expected 1 typed action after unmarshal, got %d", len(roundTripped.TypedActions))
	}
}

func TestActionEqualTo_LegacyUnchanged(t *testing.T) {
	// Existing actions without new fields should still compare equal.
	a1 := Action{
		ServiceName: "auth-service",
		Scope:       "default/my-route",
		Predicates:  []string{"request.url_path.startsWith('/')"},
	}
	a2 := Action{
		ServiceName: "auth-service",
		Scope:       "default/my-route",
		Predicates:  []string{"request.url_path.startsWith('/')"},
	}
	if !a1.EqualTo(a2) {
		t.Fatal("legacy actions without new fields should be equal")
	}
}

func TestConditionalData_EqualTo(t *testing.T) {
	testCases := []struct {
		name     string
		cd1      ConditionalData
		cd2      ConditionalData
		expected bool
	}{
		{
			name: "equal conditional data - same predicates and data",
			cd1: ConditionalData{
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
			cd2: ConditionalData{
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
			expected: true,
		},
		{
			name:     "empty conditional data - both empty",
			cd1:      ConditionalData{},
			cd2:      ConditionalData{},
			expected: true,
		},
		{
			name: "different predicates - different values",
			cd1: ConditionalData{
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
			cd2: ConditionalData{
				Predicates: []string{
					"source.address != '192.168.1.1'",
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
			expected: false,
		},
		{
			name: "different data - different values",
			cd1: ConditionalData{
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
			cd2: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.global__f63bec56",
								Value: "2",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "different number of predicates",
			cd1: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
			},
			cd2: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.method == 'GET'",
				},
			},
			expected: false,
		},
		{
			name: "different number of data items",
			cd1: ConditionalData{
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
			cd2: ConditionalData{
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.global__f63bec56",
								Value: "1",
							},
						},
					},
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.user__abc123",
								Value: "5",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "same predicates but different order - should not be equal",
			cd1: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.method == 'GET'",
				},
			},
			cd2: ConditionalData{
				Predicates: []string{
					"request.method == 'GET'",
					"source.address != '127.0.0.1'",
				},
			},
			expected: false,
		},
		{
			name: "multiple predicates and data - all equal",
			cd1: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.method == 'GET'",
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
					{
						Value: &Expression{
							ExpressionItem: ExpressionItem{
								Key:   "limit.user",
								Value: "auth.identity.user",
							},
						},
					},
				},
			},
			cd2: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.method == 'GET'",
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
					{
						Value: &Expression{
							ExpressionItem: ExpressionItem{
								Key:   "limit.user",
								Value: "auth.identity.user",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "only predicates, no data - equal",
			cd1: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.method == 'GET'",
				},
			},
			cd2: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.method == 'GET'",
				},
			},
			expected: true,
		},
		{
			name: "only data, no predicates - equal",
			cd1: ConditionalData{
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
			cd2: ConditionalData{
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
			expected: true,
		},
		{
			name: "one has predicates, other doesn't",
			cd1: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
			},
			cd2:      ConditionalData{},
			expected: false,
		},
		{
			name: "one has data, other doesn't",
			cd1: ConditionalData{
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
			cd2:      ConditionalData{},
			expected: false,
		},
		{
			name: "empty predicates slice vs nil predicates - not equal with reflect.DeepEqual",
			cd1: ConditionalData{
				Predicates: []string{},
			},
			cd2:      ConditionalData{},
			expected: false,
		},
		{
			name: "empty data slice vs nil data",
			cd1: ConditionalData{
				Data: []DataType{},
			},
			cd2:      ConditionalData{},
			expected: true,
		},
		{
			name: "complex predicates with CEL expressions",
			cd1: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.headers['user-agent'].startsWith('Mozilla')",
					"auth.identity.user != ''",
				},
			},
			cd2: ConditionalData{
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.headers['user-agent'].startsWith('Mozilla')",
					"auth.identity.user != ''",
				},
			},
			expected: true,
		},
		{
			name: "different data types in data slice",
			cd1: ConditionalData{
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.global",
								Value: "1",
							},
						},
					},
					{
						Value: &Expression{
							ExpressionItem: ExpressionItem{
								Key:   "limit.user",
								Value: "auth.identity.user",
							},
						},
					},
				},
			},
			cd2: ConditionalData{
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.global",
								Value: "1",
							},
						},
					},
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.user",
								Value: "1",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "same data different order - ordering matters",
			cd1: ConditionalData{
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.global",
								Value: "1",
							},
						},
					},
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.user",
								Value: "5",
							},
						},
					},
				},
			},
			cd2: ConditionalData{
				Data: []DataType{
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.user",
								Value: "5",
							},
						},
					},
					{
						Value: &Static{
							Static: StaticSpec{
								Key:   "limit.global",
								Value: "1",
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "single predicate with special characters",
			cd1: ConditionalData{
				Predicates: []string{
					"request.headers['X-Custom-Header'] == 'value-with-dashes'",
				},
			},
			cd2: ConditionalData{
				Predicates: []string{
					"request.headers['X-Custom-Header'] == 'value-with-dashes'",
				},
			},
			expected: true,
		},
		{
			name: "multiple data with expressions",
			cd1: ConditionalData{
				Predicates: []string{
					"auth.identity.user != ''",
				},
				Data: []DataType{
					{
						Value: &Expression{
							ExpressionItem: ExpressionItem{
								Key:   "limit.user",
								Value: "auth.identity.user",
							},
						},
					},
					{
						Value: &Expression{
							ExpressionItem: ExpressionItem{
								Key:   "limit.group",
								Value: "auth.identity.group",
							},
						},
					},
				},
			},
			cd2: ConditionalData{
				Predicates: []string{
					"auth.identity.user != ''",
				},
				Data: []DataType{
					{
						Value: &Expression{
							ExpressionItem: ExpressionItem{
								Key:   "limit.user",
								Value: "auth.identity.user",
							},
						},
					},
					{
						Value: &Expression{
							ExpressionItem: ExpressionItem{
								Key:   "limit.group",
								Value: "auth.identity.group",
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := tc.cd1.EqualTo(tc.cd2)
			if result != tc.expected {
				diff := cmp.Diff(tc.cd1, tc.cd2)
				subT.Fatalf("unexpected ConditionalData equality result. Expected %v, got %v. Diff (-want +got):\n%s", tc.expected, result, diff)
			}
		})
	}
}

func TestDataType_EqualTo(t *testing.T) {
	testCases := []struct {
		name     string
		dt1      DataType
		dt2      DataType
		expected bool
	}{
		{
			name: "equal static data types - same key and value",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "1",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "1",
					},
				},
			},
			expected: true,
		},
		{
			name: "different static data types - different value",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "1",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "2",
					},
				},
			},
			expected: false,
		},
		{
			name: "different static data types - different key",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "1",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__different",
						Value: "1",
					},
				},
			},
			expected: false,
		},
		{
			name: "different static data types - different key and value",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "1",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__different",
						Value: "2",
					},
				},
			},
			expected: false,
		},
		{
			name: "equal expression data types - same key and expression",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "auth.identity.user",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "auth.identity.user",
					},
				},
			},
			expected: true,
		},
		{
			name: "different expression data types - different expression value",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "auth.identity.user",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "auth.identity.group",
					},
				},
			},
			expected: false,
		},
		{
			name: "different expression data types - different key",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "auth.identity.user",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__different",
						Value: "auth.identity.user",
					},
				},
			},
			expected: false,
		},
		{
			name: "different expression data types - different key and expression",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "auth.identity.user",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__different",
						Value: "auth.identity.group",
					},
				},
			},
			expected: false,
		},
		{
			name: "static vs expression - should not be equal",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "1",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "auth.identity.user",
					},
				},
			},
			expected: false,
		},
		{
			name: "expression vs static - should not be equal",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "auth.identity.user",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "1",
					},
				},
			},
			expected: false,
		},
		{
			name: "static with empty key - equal",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "",
						Value: "1",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "",
						Value: "1",
					},
				},
			},
			expected: true,
		},
		{
			name: "static with empty value - equal",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global__f63bec56",
						Value: "",
					},
				},
			},
			expected: true,
		},
		{
			name: "static with empty key and value - equal",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "",
						Value: "",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "",
						Value: "",
					},
				},
			},
			expected: true,
		},
		{
			name: "expression with empty key - equal",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "",
						Value: "auth.identity.user",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "",
						Value: "auth.identity.user",
					},
				},
			},
			expected: true,
		},
		{
			name: "expression with empty expression value - equal",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.global__f63bec56",
						Value: "",
					},
				},
			},
			expected: true,
		},
		{
			name: "expression with empty key and value - equal",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "",
						Value: "",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "",
						Value: "",
					},
				},
			},
			expected: true,
		},
		{
			name: "static with special characters in key",
			dt1: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global/special:key_with-chars",
						Value: "1",
					},
				},
			},
			dt2: DataType{
				Value: &Static{
					Static: StaticSpec{
						Key:   "limit.global/special:key_with-chars",
						Value: "1",
					},
				},
			},
			expected: true,
		},
		{
			name: "expression with complex CEL expression",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.user",
						Value: "auth.identity.user != '' ? auth.identity.user : 'anonymous'",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.user",
						Value: "auth.identity.user != '' ? auth.identity.user : 'anonymous'",
					},
				},
			},
			expected: true,
		},
		{
			name: "different complex CEL expressions",
			dt1: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.user",
						Value: "auth.identity.user != '' ? auth.identity.user : 'anonymous'",
					},
				},
			},
			dt2: DataType{
				Value: &Expression{
					ExpressionItem: ExpressionItem{
						Key:   "limit.user",
						Value: "auth.identity.user != '' ? auth.identity.user : 'guest'",
					},
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := tc.dt1.EqualTo(tc.dt2)
			if result != tc.expected {
				diff := cmp.Diff(tc.dt1, tc.dt2)
				subT.Fatalf("unexpected DataType equality result. Expected %v, got %v. Diff (-want +got):\n%s", tc.expected, result, diff)
			}
		})
	}
}

func TestAction_EqualTo(t *testing.T) {
	testCases := []struct {
		name     string
		action1  Action
		action2  Action
		expected bool
	}{
		{
			name: "equal actions - identical",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
				ConditionalData: []ConditionalData{
					{
						Predicates: []string{
							"request.method == 'GET'",
						},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "1",
									},
								},
							},
						},
					},
				},
				SourcePolicyLocators: []string{
					"RateLimitPolicy/default/my-policy",
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
				ConditionalData: []ConditionalData{
					{
						Predicates: []string{
							"request.method == 'GET'",
						},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "1",
									},
								},
							},
						},
					},
				},
				SourcePolicyLocators: []string{
					"RateLimitPolicy/default/my-policy",
				},
			},
			expected: true,
		},
		{
			name: "different scope",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/different",
			},
			expected: false,
		},
		{
			name: "different service name",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
			},
			action2: Action{
				ServiceName: "auth-service",
				Scope:       "default/other",
			},
			expected: false,
		},
		{
			name: "same predicates different order - should NOT be equal (order matters)",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.method == 'GET'",
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				Predicates: []string{
					"request.method == 'GET'",
					"source.address != '127.0.0.1'",
				},
			},
			expected: false,
		},
		{
			name: "same source policy locators different order - should NOT be equal (order matters)",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				SourcePolicyLocators: []string{
					"RateLimitPolicy/default/policy1",
					"RateLimitPolicy/default/policy2",
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				SourcePolicyLocators: []string{
					"RateLimitPolicy/default/policy2",
					"RateLimitPolicy/default/policy1",
				},
			},
			expected: false,
		},
		{
			name: "same conditional data different order - SHOULD be equal (order doesn't matter)",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				ConditionalData: []ConditionalData{
					{
						Predicates: []string{"request.method == 'GET'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "1",
									},
								},
							},
						},
					},
					{
						Predicates: []string{"request.method == 'POST'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.user",
										Value: "5",
									},
								},
							},
						},
					},
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				ConditionalData: []ConditionalData{
					{
						Predicates: []string{"request.method == 'POST'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.user",
										Value: "5",
									},
								},
							},
						},
					},
					{
						Predicates: []string{"request.method == 'GET'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "1",
									},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "different conditional data - different values",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				ConditionalData: []ConditionalData{
					{
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "1",
									},
								},
							},
						},
					},
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				ConditionalData: []ConditionalData{
					{
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "2",
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "different number of conditional data items",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				ConditionalData: []ConditionalData{
					{
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "1",
									},
								},
							},
						},
					},
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				ConditionalData: []ConditionalData{
					{
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "1",
									},
								},
							},
						},
					},
					{
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.user",
										Value: "5",
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "empty actions - both empty",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
			},
			expected: true,
		},
		{
			name: "complex action with multiple conditional data - different order but equal",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/complex",
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.headers['user-agent'] != ''",
				},
				ConditionalData: []ConditionalData{
					{
						Predicates: []string{"auth.identity.user != ''"},
						Data: []DataType{
							{
								Value: &Expression{
									ExpressionItem: ExpressionItem{
										Key:   "limit.user",
										Value: "auth.identity.user",
									},
								},
							},
						},
					},
					{
						Predicates: []string{"request.method == 'GET'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "100",
									},
								},
							},
						},
					},
					{
						Predicates: []string{"request.method == 'POST'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "50",
									},
								},
							},
						},
					},
				},
				SourcePolicyLocators: []string{
					"RateLimitPolicy/default/gateway-policy",
					"RateLimitPolicy/default/route-policy",
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/complex",
				Predicates: []string{
					"source.address != '127.0.0.1'",
					"request.headers['user-agent'] != ''",
				},
				ConditionalData: []ConditionalData{
					{
						Predicates: []string{"request.method == 'POST'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "50",
									},
								},
							},
						},
					},
					{
						Predicates: []string{"auth.identity.user != ''"},
						Data: []DataType{
							{
								Value: &Expression{
									ExpressionItem: ExpressionItem{
										Key:   "limit.user",
										Value: "auth.identity.user",
									},
								},
							},
						},
					},
					{
						Predicates: []string{"request.method == 'GET'"},
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "100",
									},
								},
							},
						},
					},
				},
				SourcePolicyLocators: []string{
					"RateLimitPolicy/default/gateway-policy",
					"RateLimitPolicy/default/route-policy",
				},
			},
			expected: true,
		},
		{
			name: "one has predicates, other doesn't",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				Predicates: []string{
					"source.address != '127.0.0.1'",
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
			},
			expected: false,
		},
		{
			name: "one has source policy locators, other doesn't",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				SourcePolicyLocators: []string{
					"RateLimitPolicy/default/my-policy",
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
			},
			expected: false,
		},
		{
			name: "one has conditional data, other doesn't",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				ConditionalData: []ConditionalData{
					{
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.global",
										Value: "1",
									},
								},
							},
						},
					},
				},
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
			},
			expected: false,
		},
		{
			name: "nil vs empty predicates slice",
			action1: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				Predicates:  nil,
			},
			action2: Action{
				ServiceName: "ratelimit-service",
				Scope:       "default/other",
				Predicates:  []string{},
			},
			expected: false,
		},
		{
			name: "nil vs empty source policy locators - slices.Equal treats as equal",
			action1: Action{
				ServiceName:          "ratelimit-service",
				Scope:                "default/other",
				SourcePolicyLocators: nil,
			},
			action2: Action{
				ServiceName:          "ratelimit-service",
				Scope:                "default/other",
				SourcePolicyLocators: []string{},
			},
			expected: true, // slices.Equal treats nil and empty slice as equal
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := tc.action1.EqualTo(tc.action2)
			if result != tc.expected {
				diff := cmp.Diff(tc.action1, tc.action2)
				subT.Fatalf("unexpected Action equality result. Expected %v, got %v. Diff (-want +got):\n%s", tc.expected, result, diff)
			}
		})
	}
}
