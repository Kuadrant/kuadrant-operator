//go:build unit

package wasm

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/ptr"
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
							NewDenyAction("source.address != '127.0.0.1'", "DenyResponse{status: 429u}"),
						},
					},
				},
			},
			config2: &Config{ // same config as config1 with fields sorted alphabetically
				ActionSets: []ActionSet{
					{
						Actions: []Action{
							NewDenyAction("source.address != '127.0.0.1'", "DenyResponse{status: 429u}"),
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

func TestAction_JSON(t *testing.T) {
	ta := NewGrpcAction(
		`"x-assess-threat" in request.headers`,
		"threatResponse", "ext-abc123",
		"threat.v1.Request{path: request.path}", "",
	).WithSources([]string{"ThreatPolicy/default/my-threat"}).
		WithOnReply(
			NewDenyAction("!(threatResponse.threat_level < 5)", "DenyResponse{status: 403u}"),
			NewHeadersAction("true", "response", `{"x-threat-assessed": "true"}`),
		)

	data, err := json.Marshal(ta)
	if err != nil {
		t.Fatalf("failed to marshal Action: %v", err)
	}

	roundTripped, err := UnmarshalAction(data)
	if err != nil {
		t.Fatalf("failed to unmarshal Action: %v", err)
	}

	if !ta.EqualTo(roundTripped) {
		t.Fatalf("round-tripped Action not equal:\n  got:  %+v\n  want: %+v", roundTripped, ta)
	}

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

func TestAction_EqualTo(t *testing.T) {
	base := NewGrpcAction("true", "resp", "ext-svc", "", "").
		WithSources([]string{"Policy/ns/name"}).
		WithOnReply(NewDenyAction("!(resp.ok)", "DenyResponse{status: 403u}"))

	same := NewGrpcAction("true", "resp", "ext-svc", "", "").
		WithSources([]string{"Policy/ns/name"}).
		WithOnReply(NewDenyAction("!(resp.ok)", "DenyResponse{status: 403u}"))

	if !base.EqualTo(same) {
		t.Fatal("identical Actions should be equal")
	}

	diffVar := NewGrpcAction("true", "other", "ext-svc", "", "").
		WithSources([]string{"Policy/ns/name"}).
		WithOnReply(NewDenyAction("!(resp.ok)", "DenyResponse{status: 403u}"))
	if base.EqualTo(diffVar) {
		t.Fatal("Actions with different Var should not be equal")
	}

	diffOnReply := NewGrpcAction("true", "resp", "ext-svc", "", "").
		WithSources([]string{"Policy/ns/name"})
	if base.EqualTo(diffOnReply) {
		t.Fatal("Actions with different OnReply length should not be equal")
	}
}

func TestAction_FailType_JSON(t *testing.T) {
	ta := NewFailAction(`threatResponse.error_code != 0`, "Threat service returned unexpected error")

	data, err := json.Marshal(ta)
	if err != nil {
		t.Fatalf("failed to marshal Action: %v", err)
	}

	roundTripped, err := UnmarshalAction(data)
	if err != nil {
		t.Fatalf("failed to unmarshal Action: %v", err)
	}

	if !ta.EqualTo(roundTripped) {
		t.Fatalf("round-tripped Action not equal:\n  got:  %+v\n  want: %+v", roundTripped, ta)
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

func TestAction_EqualTo_FailFields(t *testing.T) {
	a := NewFailAction("true", "error")
	b := NewFailAction("true", "error")
	if !a.EqualTo(b) {
		t.Fatal("identical fail Actions should be equal")
	}

	diffMsg := NewFailAction("true", "other")
	if a.EqualTo(diffMsg) {
		t.Fatal("Actions with different LogMessage should not be equal")
	}
}
