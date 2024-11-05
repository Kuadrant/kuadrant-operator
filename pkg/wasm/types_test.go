//go:build unit

package wasm

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
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
				Services: map[string]Service{
					"ratelimit-service": {
						Type:        "ratelimit",
						Endpoint:    "kuadrant-ratelimit-service",
						FailureMode: "allow",
					},
				},
				ActionSets: []ActionSet{
					{
						Name: "5755da0b3c275ba6b8f553890eb32b04768a703b60ab9a5d7f4e0948e23ef0ab",
						RouteRuleConditions: RouteRuleConditions{
							Hostnames: []string{"other.example.com"},
							Matches: []Predicate{
								{
									Selector: "request.url_path",
									Operator: "startswith",
									Value:    "/",
								},
							},
						},
						Actions: []Action{
							{
								ServiceName: "ratelimit-service",
								Scope:       "default/other",
								Conditions: []Predicate{
									{
										Selector: "source.address",
										Operator: "neq",
										Value:    "127.0.0.1",
									},
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
			config2: &Config{ // same config as config1 with fields orted alphabetically
				ActionSets: []ActionSet{
					{
						Actions: []Action{
							{
								Conditions: []Predicate{
									{
										Operator: "neq",
										Selector: "source.address",
										Value:    "127.0.0.1",
									},
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
								ServiceName: "ratelimit-service",
								Scope:       "default/other",
							},
						},
						Name: "5755da0b3c275ba6b8f553890eb32b04768a703b60ab9a5d7f4e0948e23ef0ab",
						RouteRuleConditions: RouteRuleConditions{
							Hostnames: []string{"other.example.com"},
							Matches: []Predicate{
								{
									Operator: "startswith",
									Selector: "request.url_path",
									Value:    "/",
								},
							},
						},
					},
				},
				Services: map[string]Service{
					"ratelimit-service": {
						Type:        "ratelimit",
						Endpoint:    "kuadrant-ratelimit-service",
						FailureMode: "allow",
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
				subT.Fatalf("unexpected config equality result")
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
				Data:        nil,
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
data:
- other:
    key: keyA
`,
		},
		{
			name: "both data types at the same time",
			yaml: `
service: ratelimit-service
scope: some-scope
data:
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
