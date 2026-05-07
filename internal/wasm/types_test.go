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
