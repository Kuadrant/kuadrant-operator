//go:build unit

package wasm

import (
	"errors"
	"testing"

	"gotest.tools/assert"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/structpb"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/yaml"
)

var (
	testBasicConfig = &Config{
		Services: map[string]Service{
			"auth-service": {
				Type:        "auth",
				Endpoint:    "kuadrant-auth-service",
				FailureMode: "deny",
			},
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
					Predicates: []string{
						"request.url_path.startsWith('/')",
					},
				},
				Actions: []Action{
					{
						ServiceName: "ratelimit-service",
						Scope:       "default/other",
						Conditions: []Condition{
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
			{
				Name: "21cb3adc608c09a360d62a03fd1afd7cc6f8720999a51d7916927fff26a34ef8",
				RouteRuleConditions: RouteRuleConditions{
					Hostnames: []string{"*"},
					Predicates: []string{
						"request.method == 'GET'",
						"request.url_path.startsWith('/')",
					},
				},
				Actions: []Action{
					{
						ServiceName: "auth-service",
						Scope:       "e2db39952dd3bc72e152330a2eb15abbd9675c7ac6b54a1a292f07f25f09f138",
					},
					{
						ServiceName: "ratelimit-service",
						Scope:       "default/toystore",
						Data: []DataType{
							{
								Value: &Static{
									Static: StaticSpec{
										Key:   "limit.specific__69ea4d2d",
										Value: "1",
									},
								},
							},
						},
					},
					{
						ServiceName: "ratelimit-service",
						Scope:       "default/toystore",
						Conditions: []Condition{
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
	}
	testBasicConfigJSON = `{"services":{"auth-service":{"endpoint":"kuadrant-auth-service","type":"auth","failureMode":"deny"},"ratelimit-service":{"endpoint":"kuadrant-ratelimit-service","type":"ratelimit","failureMode":"allow"}},"actionSets":[{"name":"5755da0b3c275ba6b8f553890eb32b04768a703b60ab9a5d7f4e0948e23ef0ab","routeRuleConditions":{"hostnames":["other.example.com"],"predicates":["request.url_path.startsWith('/')"]},"actions":[{"service":"ratelimit-service","scope":"default/other","conditions":[{"operator":"neq","selector":"source.address","value":"127.0.0.1"}],"data":[{"static":{"key":"limit.global__f63bec56","value":"1"}}]}]},{"name":"21cb3adc608c09a360d62a03fd1afd7cc6f8720999a51d7916927fff26a34ef8","routeRuleConditions":{"hostnames":["*"],"predicates":["request.method == 'GET'","request.url_path.startsWith('/')"]},"actions":[{"service":"auth-service","scope":"e2db39952dd3bc72e152330a2eb15abbd9675c7ac6b54a1a292f07f25f09f138"},{"service":"ratelimit-service","scope":"default/toystore","data":[{"static":{"key":"limit.specific__69ea4d2d","value":"1"}}]},{"service":"ratelimit-service","scope":"default/toystore","conditions":[{"operator":"neq","selector":"source.address","value":"127.0.0.1"}],"data":[{"static":{"key":"limit.global__f63bec56","value":"1"}}]}]}]}`
	testBasicConfigYAML = `
services:
  auth-service:
    type: auth
    endpoint: kuadrant-auth-service
    failureMode: deny
  ratelimit-service:
    type: ratelimit
    endpoint: kuadrant-ratelimit-service
    failureMode: allow
actionSets:
  - name: 5755da0b3c275ba6b8f553890eb32b04768a703b60ab9a5d7f4e0948e23ef0ab
    routeRuleConditions:
      hostnames:
        - other.example.com
      predicates:
        - request.url_path.startsWith('/')
    actions:
      - service: ratelimit-service
        scope: default/other
        conditions:
          - operator: neq
            selector: source.address
            value: 127.0.0.1
        data:
          - static:
              key: limit.global__f63bec56
              value: "1"
  - name: 21cb3adc608c09a360d62a03fd1afd7cc6f8720999a51d7916927fff26a34ef8
    routeRuleConditions:
      hostnames:
        - "*"
      predicates:
        - request.method == 'GET'
        - request.url_path.startsWith('/')
    actions:
      - service: auth-service
        scope: e2db39952dd3bc72e152330a2eb15abbd9675c7ac6b54a1a292f07f25f09f138
      - service: ratelimit-service
        scope: default/toystore
        data:
          - static:
              key: limit.specific__69ea4d2d
              value: "1"
      - service: ratelimit-service
        scope: default/toystore
        conditions:
          - operator: neq
            selector: source.address
            value: 127.0.0.1
        data:
          - static:
              key: limit.global__f63bec56
              value: "1"
`
)

func TestConfigFromJSON(t *testing.T) {
	testCases := []struct {
		name           string
		json           *apiextensionsv1.JSON
		expectedConfig *Config
		expectedError  error
	}{
		{
			name:          "nil config",
			json:          nil,
			expectedError: errors.New("cannot desestructure config from nil"),
		},
		{
			name:           "valid config",
			json:           &apiextensionsv1.JSON{Raw: []byte(testBasicConfigJSON)},
			expectedConfig: testBasicConfig,
		},
		{
			name:          "invalid config",
			json:          &apiextensionsv1.JSON{Raw: []byte(`{invalid}`)},
			expectedError: errors.New("invalid character 'i' looking for beginning of object key string"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			config, err := ConfigFromJSON(tc.json)
			if (err == nil && tc.expectedError != nil) || (err != nil && tc.expectedError == nil) || (err != nil && tc.expectedError != nil && err.Error() != tc.expectedError.Error()) {
				t.Fatalf("unexpected error to be: %+v, got: %+v", tc.expectedError, err)
			}
			if !cmp.Equal(tc.expectedConfig, config) {
				diff := cmp.Diff(tc.expectedConfig, config)
				subT.Fatalf("unexpected config (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConfigFromStruct(t *testing.T) {
	testCases := []struct {
		name           string
		yaml           *string
		expectedConfig *Config
		expectedError  error
	}{
		{
			name:          "nil config",
			yaml:          nil,
			expectedError: errors.New("cannot desestructure config from nil"),
		},
		{
			name:           "valid config",
			yaml:           &testBasicConfigYAML,
			expectedConfig: testBasicConfig,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			var structure *structpb.Struct
			if y := tc.yaml; y != nil {
				m := map[string]any{}
				if err := yaml.Unmarshal([]byte(*tc.yaml), &m); err != nil {
					subT.Fatal(err)
				}
				structure, _ = structpb.NewStruct(m)
			}
			config, err := ConfigFromStruct(structure)
			if (err == nil && tc.expectedError != nil) || (err != nil && tc.expectedError == nil) || (err != nil && tc.expectedError != nil && err.Error() != tc.expectedError.Error()) {
				t.Fatalf("unexpected error to be: %+v, got: %+v", tc.expectedError, err)
			}
			if !cmp.Equal(tc.expectedConfig, config) {
				diff := cmp.Diff(tc.expectedConfig, config)
				subT.Fatalf("unexpected config (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPredicatesFromHTTPRouteMatch(t *testing.T) {
	queryParams := make([]gatewayapiv1.HTTPQueryParamMatch, 0)
	queryParamMatch := gatewayapiv1.QueryParamMatchExact
	queryParams = append(queryParams, gatewayapiv1.HTTPQueryParamMatch{
		Type:  &queryParamMatch,
		Name:  "foo",
		Value: "bar",
	})
	queryParams = append(queryParams, gatewayapiv1.HTTPQueryParamMatch{
		Type:  &queryParamMatch,
		Name:  "foo",
		Value: "baz",
	}) // this param will be ignored, as `foo` was defined above to match `bar`
	queryParams = append(queryParams, gatewayapiv1.HTTPQueryParamMatch{
		Type:  &queryParamMatch,
		Name:  "kua",
		Value: "drant",
	})

	headerMatch := gatewayapiv1.HeaderMatchExact
	header := gatewayapiv1.HTTPHeaderMatch{
		Type:  &headerMatch,
		Name:  "x-auth",
		Value: "kuadrant",
	}

	method := gatewayapiv1.HTTPMethodTrace

	pathMatch := gatewayapiv1.PathMatchPathPrefix
	path := "/admin"
	predicates := PredicatesFromHTTPRouteMatch(gatewayapiv1.HTTPRouteMatch{
		Path: &gatewayapiv1.HTTPPathMatch{
			Type:  &pathMatch,
			Value: &path,
		},
		Headers:     []gatewayapiv1.HTTPHeaderMatch{header},
		QueryParams: queryParams,
		Method:      &method,
	})

	assert.Equal(t, predicates[0], "request.method == 'TRACE'")
	assert.Equal(t, predicates[1], "request.url_path.startsWith('/admin')")
	assert.Equal(t, predicates[2], "request.headers['x-auth'] == 'kuadrant'")
	assert.Equal(t, predicates[3], "queryMap(request.query)['foo'] == 'bar'")
	assert.Equal(t, predicates[4], "queryMap(request.query)['kua'] == 'drant'")
	assert.Equal(t, len(predicates), 5)
}
