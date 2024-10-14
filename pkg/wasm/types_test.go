//go:build unit

package wasm

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

func TestConfig(t *testing.T) {
	testCases := []struct {
		name           string
		expectedConfig *Config
		yaml           string
	}{
		{
			name:           "basic example",
			expectedConfig: testBasicConfigExample(),
			yaml: `
extensions:
  limitador:
    type: ratelimit
    endpoint: kuadrant-rate-limiting-service
    failureMode: allow
policies:
- name: rlp-ns-A/rlp-name-A
  hostnames:
  - '*.toystore.com'
  - example.com
  rules:
  - conditions:
    - allOf:
      - selector: request.host
        operator: eq
        value: cars.toystore.com
    actions:
    - extension: limitador
      scope: rlp-ns-A/rlp-name-A
      data:
      - static:
          key: rlp-ns-A/rlp-name-A
          value: "1"
      - selector:
          selector: auth.metadata.username
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			var conf Config
			if err := yaml.Unmarshal([]byte(tc.yaml), &conf); err != nil {
				subT.Fatal(err)
			}

			if !cmp.Equal(tc.expectedConfig, &conf) {
				diff := cmp.Diff(tc.expectedConfig, &conf)
				subT.Fatalf("unexpected config (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConfigMarshallUnmarshalling(t *testing.T) {
	conf := testBasicConfigExample()
	serializedConfig, err := json.Marshal(conf)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(serializedConfig))

	var unMarshalledConf Config
	if err := json.Unmarshal(serializedConfig, &unMarshalledConf); err != nil {
		t.Fatal(err)
	}

	if !cmp.Equal(conf, &unMarshalledConf) {
		diff := cmp.Diff(conf, &unMarshalledConf)
		t.Fatalf("unexpected wasm rules (-want +got):\n%s", diff)
	}
}

func TestValidActionConfig(t *testing.T) {
	testCases := []struct {
		name           string
		yaml           string
		expectedAction *Action
	}{
		{
			name: "valid empty data",
			expectedAction: &Action{
				Scope:         "some-scope",
				ExtensionName: "limitador",
				Data:          nil,
			},
			yaml: `
scope: some-scope
extension: limitador
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

func TestInValidActionConfig(t *testing.T) {
	testCases := []struct {
		name string
		yaml string
	}{
		{
			name: "unknown data type",
			yaml: `
scope: some-scope
extension: limitador
data:
- other:
    key: keyA
`,
		},
		{
			name: "both data types at the same time",
			yaml: `
scope: some-scope
extension: limitador
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

func testBasicConfigExample() *Config {
	return &Config{
		Extensions: map[string]Extension{
			RateLimitExtensionName: {
				Endpoint:    common.KuadrantRateLimitClusterName,
				FailureMode: FailureModeAllow,
				Type:        RateLimitExtensionType,
			},
		},
		Policies: []Policy{
			{
				Name: "rlp-ns-A/rlp-name-A",
				Hostnames: []string{
					"*.toystore.com",
					"example.com",
				},
				Rules: []Rule{
					{
						Conditions: []Condition{
							{
								AllOf: []PatternExpression{
									{
										Selector: "request.host",
										Operator: PatternOperator(kuadrantv1beta3.EqualOperator),
										Value:    "cars.toystore.com",
									},
								},
							},
						},
						Actions: []Action{
							{
								Scope:         "rlp-ns-A/rlp-name-A",
								ExtensionName: RateLimitExtensionName,
								Data: []DataType{
									{
										Value: &Static{
											Static: StaticSpec{
												Key:   "rlp-ns-A/rlp-name-A",
												Value: "1",
											},
										},
									},
									{
										Value: &Selector{
											Selector: SelectorSpec{
												Selector: "auth.metadata.username",
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
	}
}
