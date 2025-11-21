//go:build unit

package wasm

import (
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/api/v1beta1"

	"gotest.tools/assert"
	"k8s.io/utils/ptr"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/structpb"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/yaml"
)

var (
	testBasicConfig = &Config{
		RequestData: map[string]string{
			"metrics.labels.user":  "auth.identity.user",
			"metrics.labels.group": "auth.identity.group",
		},
		Services: map[string]Service{
			"auth-service": {
				Type:        "auth",
				Endpoint:    "kuadrant-auth-service",
				FailureMode: "deny",
				Timeout:     ptr.To("200ms"),
			},
			"ratelimit-service": {
				Type:        "ratelimit",
				Endpoint:    "kuadrant-ratelimit-service",
				FailureMode: "allow",
				Timeout:     ptr.To("100ms"),
			},
			"ratelimit-check-service": {
				Type:        "ratelimit-check",
				Endpoint:    "kuadrant-ratelimit-service",
				FailureMode: "allow",
				Timeout:     ptr.To("100ms"),
			},
			"ratelimit-report-service": {
				Type:        "ratelimit-report",
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
									`source.address != "127.0.0.1"`,
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
						ConditionalData: []ConditionalData{
							{
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
						},
					},
					{
						ServiceName: "ratelimit-service",
						Scope:       "default/toystore",
						ConditionalData: []ConditionalData{
							{
								Predicates: []string{
									`source.address != "127.0.0.1"`,
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
	}
	testBasicConfigJSON = `{"requestData":{"metrics.labels.group":"auth.identity.group","metrics.labels.user":"auth.identity.user"},"services":{"auth-service":{"type":"auth","endpoint":"kuadrant-auth-service","failureMode":"deny","timeout":"200ms"},"ratelimit-service":{"type":"ratelimit","endpoint":"kuadrant-ratelimit-service","failureMode":"allow","timeout":"100ms"},"ratelimit-check-service":{"type":"ratelimit-check","endpoint":"kuadrant-ratelimit-service","failureMode":"allow","timeout":"100ms"},"ratelimit-report-service":{"type":"ratelimit-report","endpoint":"kuadrant-ratelimit-service","failureMode":"allow","timeout":"100ms"}},"actionSets":[{"name":"5755da0b3c275ba6b8f553890eb32b04768a703b60ab9a5d7f4e0948e23ef0ab","routeRuleConditions":{"hostnames":["other.example.com"],"predicates":["request.url_path.startsWith('/')"]},"actions":[{"service":"ratelimit-service","scope":"default/other","conditionalData":[{"predicates":["source.address != \"127.0.0.1\""],"data":[{"static":{"key":"limit.global__f63bec56","value":"1"}}]}]}]},{"name":"21cb3adc608c09a360d62a03fd1afd7cc6f8720999a51d7916927fff26a34ef8","routeRuleConditions":{"hostnames":["*"],"predicates":["request.method == 'GET'","request.url_path.startsWith('/')"]},"actions":[{"service":"auth-service","scope":"e2db39952dd3bc72e152330a2eb15abbd9675c7ac6b54a1a292f07f25f09f138"},{"service":"ratelimit-service","scope":"default/toystore","conditionalData":[{"data":[{"static":{"key":"limit.specific__69ea4d2d","value":"1"}}]}]},{"service":"ratelimit-service","scope":"default/toystore","conditionalData":[{"predicates":["source.address != \"127.0.0.1\""],"data":[{"static":{"key":"limit.global__f63bec56","value":"1"}}]}]}]}]}`
	testBasicConfigYAML = `
requestData:
  metrics.labels.user: auth.identity.user
  metrics.labels.group: auth.identity.group
services:
  auth-service:
    type: auth
    endpoint: kuadrant-auth-service
    failureMode: deny
    timeout: 200ms
  ratelimit-service:
    type: ratelimit
    endpoint: kuadrant-ratelimit-service
    failureMode: allow
    timeout: 100ms
  ratelimit-check-service:
    type: ratelimit-check
    endpoint: kuadrant-ratelimit-service
    failureMode: allow
    timeout: 100ms
  ratelimit-report-service:
    type: ratelimit-report
    endpoint: kuadrant-ratelimit-service
    failureMode: allow
    timeout: 100ms
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
        conditionalData:
          - predicates:
              - source.address != "127.0.0.1"
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
        conditionalData:
          - data:
              - static:
                  key: limit.specific__69ea4d2d
                  value: "1"
      - service: ratelimit-service
        scope: default/toystore
        conditionalData:
          - predicates:
              - source.address != "127.0.0.1"
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
		Name:  "X-Auth",
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
	assert.Equal(t, predicates[2], "request.headers.exists(h, h.lowerAscii() == 'x-auth' && request.headers[h] == 'kuadrant')")
	assert.Equal(t, predicates[3], "'foo' in queryMap(request.query) ? queryMap(request.query)['foo'] == 'bar' : false")
	assert.Equal(t, predicates[4], "'kua' in queryMap(request.query) ? queryMap(request.query)['kua'] == 'drant' : false")
	assert.Equal(t, len(predicates), 5)
}

func TestBuildObservabilityConfig(t *testing.T) {
	testCases := []struct {
		name                     string
		observability            *v1beta1.Observability
		expectedDefaultLevel     string
		expectedHttpHeaderId     string
		expectedTracing          *Tracing
		expectedObservabilityNil bool
	}{
		{
			name:                     "nil observability",
			observability:            nil,
			expectedObservabilityNil: true,
		},
		{
			name:                     "empty log levels",
			observability:            &v1beta1.Observability{DataPlane: &v1beta1.DataPlane{DefaultLevels: []v1beta1.LogLevel{}}},
			expectedDefaultLevel:     "ERROR",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "debug level enabled - highest priority",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Debug: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "DEBUG",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "info level enabled",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Info: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "INFO",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "warn level enabled",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Warn: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "WARN",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "error level only - default",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Error: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "ERROR",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "multiple levels - debug wins",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Error: ptr.To("true")},
						{Warn: ptr.To("true")},
						{Info: ptr.To("true")},
						{Debug: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "DEBUG",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "multiple levels - info wins (no debug)",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Error: ptr.To("true")},
						{Warn: ptr.To("true")},
						{Info: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "INFO",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "debug and info - debug wins",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Info: ptr.To("true")},
						{Debug: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "DEBUG",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "warn and error - warn wins",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Error: ptr.To("true")},
						{Warn: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "WARN",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "nil pointer values are ignored",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Debug: nil, Info: nil, Warn: ptr.To("true"), Error: nil},
					},
				},
			},
			expectedDefaultLevel:     "WARN",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "custom http header identifier",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Debug: ptr.To("true")},
					},
					HTTPHeaderIdentifier: ptr.To("x-custom-trace-id"),
				},
			},
			expectedDefaultLevel:     "DEBUG",
			expectedHttpHeaderId:     "x-custom-trace-id",
			expectedObservabilityNil: false,
		},
		{
			name: "info set after debug - debug still wins",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Debug: ptr.To("true")},
						{Info: ptr.To("false")},
					},
				},
			},
			expectedDefaultLevel:     "DEBUG",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "warn set after info - info wins due to priority",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Info: ptr.To("true")},
						{Warn: ptr.To("something")},
					},
				},
			},
			expectedDefaultLevel:     "INFO",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "only error level set to false - still uses ERROR default",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Error: ptr.To("false")},
					},
				},
			},
			expectedDefaultLevel:     "ERROR",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "warn then info then debug - debug wins",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Warn: ptr.To("true")},
						{Info: ptr.To("true")},
						{Debug: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "DEBUG",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "multiple entries same level - last one sets",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Info: ptr.To("condition1")},
						{Info: ptr.To("condition2")},
					},
				},
			},
			expectedDefaultLevel:     "INFO",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "info then warn - info wins even though warn comes after",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Info: ptr.To("true")},
						{Warn: ptr.To("true")},
					},
				},
			},
			expectedDefaultLevel:     "INFO",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "all levels with various values - debug wins",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Error: ptr.To("false")},
						{Warn: ptr.To("cel_expression_1")},
						{Info: ptr.To("cel_expression_2")},
						{Debug: ptr.To("cel_expression_3")},
					},
				},
			},
			expectedDefaultLevel:     "DEBUG",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedObservabilityNil: false,
		},
		{
			name: "with tracing endpoint configured",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Info: ptr.To("true")},
					},
				},
				Tracing: &v1beta1.Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			expectedDefaultLevel: "INFO",
			expectedHttpHeaderId: DefaultHTTPHeaderIdentifier,
			expectedTracing: &Tracing{
				Endpoint: "http://jaeger:14268/api/traces",
			},
			expectedObservabilityNil: false,
		},
		{
			name: "debug level with tracing endpoint",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Debug: ptr.To("true")},
					},
					HTTPHeaderIdentifier: ptr.To("x-trace-id"),
				},
				Tracing: &v1beta1.Tracing{
					Endpoint: "http://tempo:4318/v1/traces",
				},
			},
			expectedDefaultLevel: "DEBUG",
			expectedHttpHeaderId: "x-trace-id",
			expectedTracing: &Tracing{
				Endpoint: "http://tempo:4318/v1/traces",
			},
			expectedObservabilityNil: false,
		},
		{
			name: "only dataplane without tracing",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Warn: ptr.To("true")},
					},
				},
				Tracing: nil,
			},
			expectedDefaultLevel:     "WARN",
			expectedHttpHeaderId:     DefaultHTTPHeaderIdentifier,
			expectedTracing:          nil,
			expectedObservabilityNil: false,
		},
		{
			name: "nil dataplane with tracing - should return nil",
			observability: &v1beta1.Observability{
				DataPlane: nil,
				Tracing: &v1beta1.Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			expectedObservabilityNil: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := BuildObservabilityConfig(tc.observability)

			if tc.expectedObservabilityNil {
				assert.Assert(subT, result == nil, "expected observability to be nil")
				return
			}

			assert.Assert(subT, result != nil, "expected observability to be non-nil")
			assert.Assert(subT, result.DefaultLevel != nil, "expected DefaultLevel to be non-nil")
			assert.Equal(subT, *result.DefaultLevel, tc.expectedDefaultLevel)

			assert.Assert(subT, result.HTTPHeaderIdentifier != nil, "expected HttpHeaderIdentifier to be non-nil")
			assert.Equal(subT, *result.HTTPHeaderIdentifier, tc.expectedHttpHeaderId)

			// Validate tracing
			if tc.expectedTracing == nil {
				assert.Assert(subT, result.Tracing == nil, "expected Tracing to be nil")
			} else {
				assert.Assert(subT, result.Tracing != nil, "expected Tracing to be non-nil")
				assert.Equal(subT, result.Tracing.Endpoint, tc.expectedTracing.Endpoint)
			}
		})
	}
}

func TestObservabilityEqualTo(t *testing.T) {
	testCases := []struct {
		name     string
		obs1     *Observability
		obs2     *Observability
		expected bool
	}{
		{
			name:     "both nil",
			obs1:     nil,
			obs2:     nil,
			expected: true,
		},
		{
			name:     "first nil, second non-nil",
			obs1:     nil,
			obs2:     &Observability{},
			expected: false,
		},
		{
			name:     "first non-nil, second nil",
			obs1:     &Observability{},
			obs2:     nil,
			expected: false,
		},
		{
			name:     "both empty",
			obs1:     &Observability{},
			obs2:     &Observability{},
			expected: true,
		},
		{
			name: "same defaultLevel",
			obs1: &Observability{
				DefaultLevel: ptr.To("DEBUG"),
			},
			obs2: &Observability{
				DefaultLevel: ptr.To("DEBUG"),
			},
			expected: true,
		},
		{
			name: "different defaultLevel",
			obs1: &Observability{
				DefaultLevel: ptr.To("DEBUG"),
			},
			obs2: &Observability{
				DefaultLevel: ptr.To("INFO"),
			},
			expected: false,
		},
		{
			name: "defaultLevel nil vs non-nil",
			obs1: &Observability{
				DefaultLevel: ptr.To("DEBUG"),
			},
			obs2: &Observability{
				DefaultLevel: nil,
			},
			expected: false,
		},
		{
			name: "defaultLevel non-nil vs nil",
			obs1: &Observability{
				DefaultLevel: nil,
			},
			obs2: &Observability{
				DefaultLevel: ptr.To("DEBUG"),
			},
			expected: false,
		},
		{
			name: "same httpHeaderIdentifier",
			obs1: &Observability{
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
			},
			obs2: &Observability{
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
			},
			expected: true,
		},
		{
			name: "different httpHeaderIdentifier",
			obs1: &Observability{
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
			},
			obs2: &Observability{
				HTTPHeaderIdentifier: ptr.To("x-trace-id"),
			},
			expected: false,
		},
		{
			name: "httpHeaderIdentifier nil vs non-nil",
			obs1: &Observability{
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
			},
			obs2: &Observability{
				HTTPHeaderIdentifier: nil,
			},
			expected: false,
		},
		{
			name: "httpHeaderIdentifier non-nil vs nil",
			obs1: &Observability{
				HTTPHeaderIdentifier: nil,
			},
			obs2: &Observability{
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
			},
			expected: false,
		},
		{
			name: "complete observability - equal",
			obs1: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing: &Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			obs2: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing: &Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			expected: true,
		},
		{
			name: "complete observability - different tracing",
			obs1: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing: &Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			obs2: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing: &Tracing{
					Endpoint: "http://tempo:14268/api/traces",
				},
			},
			expected: false,
		},
		{
			name: "tracing nil vs non-nil",
			obs1: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing: &Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			obs2: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing:              nil,
			},
			expected: false,
		},
		{
			name: "tracing non-nil vs nil",
			obs1: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing:              nil,
			},
			obs2: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing: &Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			expected: false,
		},
		{
			name: "both with nil tracing",
			obs1: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing:              nil,
			},
			obs2: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
				Tracing:              nil,
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := tc.obs1.EqualTo(tc.obs2)
			assert.Equal(subT, result, tc.expected)
		})
	}
}

func TestTracingEqualTo(t *testing.T) {
	testCases := []struct {
		name     string
		tracing1 *Tracing
		tracing2 *Tracing
		expected bool
	}{
		{
			name:     "both nil",
			tracing1: nil,
			tracing2: nil,
			expected: true,
		},
		{
			name:     "first nil, second non-nil",
			tracing1: nil,
			tracing2: &Tracing{Endpoint: "http://jaeger:14268"},
			expected: false,
		},
		{
			name:     "first non-nil, second nil",
			tracing1: &Tracing{Endpoint: "http://jaeger:14268"},
			tracing2: nil,
			expected: false,
		},
		{
			name:     "both empty",
			tracing1: &Tracing{},
			tracing2: &Tracing{},
			expected: true,
		},
		{
			name:     "same endpoint",
			tracing1: &Tracing{Endpoint: "http://jaeger:14268/api/traces"},
			tracing2: &Tracing{Endpoint: "http://jaeger:14268/api/traces"},
			expected: true,
		},
		{
			name:     "different endpoint",
			tracing1: &Tracing{Endpoint: "http://jaeger:14268/api/traces"},
			tracing2: &Tracing{Endpoint: "http://tempo:14268/api/traces"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := tc.tracing1.EqualTo(tc.tracing2)
			assert.Equal(subT, result, tc.expected)
		})
	}
}

func TestConfigEqualToWithObservability(t *testing.T) {
	baseConfig := &Config{
		Services: map[string]Service{
			"test-service": {
				Type:        "auth",
				Endpoint:    "test-endpoint",
				FailureMode: "deny",
				Timeout:     ptr.To("200ms"),
			},
		},
		ActionSets: []ActionSet{},
	}

	testCases := []struct {
		name     string
		config1  *Config
		config2  *Config
		expected bool
	}{
		{
			name: "both configs without observability",
			config1: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
			},
			config2: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
			},
			expected: true,
		},
		{
			name: "one config with observability, one without",
			config1: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
				},
			},
			config2: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
			},
			expected: false,
		},
		{
			name: "both configs with same observability",
			config1: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
				},
			},
			config2: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
				},
			},
			expected: true,
		},
		{
			name: "configs with different observability",
			config1: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
				},
			},
			config2: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("INFO"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
				},
			},
			expected: false,
		},
		{
			name: "both configs with observability including tracing",
			config1: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
					Tracing: &Tracing{
						Endpoint: "http://jaeger:14268/api/traces",
					},
				},
			},
			config2: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
					Tracing: &Tracing{
						Endpoint: "http://jaeger:14268/api/traces",
					},
				},
			},
			expected: true,
		},
		{
			name: "configs with different tracing endpoints",
			config1: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
					Tracing: &Tracing{
						Endpoint: "http://jaeger:14268/api/traces",
					},
				},
			},
			config2: &Config{
				Services:   baseConfig.Services,
				ActionSets: baseConfig.ActionSets,
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
					Tracing: &Tracing{
						Endpoint: "http://tempo:14268/api/traces",
					},
				},
			},
			expected: false,
		},
		{
			name: "configs with different RequestData",
			config1: &Config{
				RequestData: map[string]string{"key1": "value1"},
				Services:    baseConfig.Services,
				ActionSets:  baseConfig.ActionSets,
			},
			config2: &Config{
				RequestData: map[string]string{"key1": "value2"},
				Services:    baseConfig.Services,
				ActionSets:  baseConfig.ActionSets,
			},
			expected: false,
		},
		{
			name: "configs with RequestData - one missing key",
			config1: &Config{
				RequestData: map[string]string{"key1": "value1", "key2": "value2"},
				Services:    baseConfig.Services,
				ActionSets:  baseConfig.ActionSets,
			},
			config2: &Config{
				RequestData: map[string]string{"key1": "value1"},
				Services:    baseConfig.Services,
				ActionSets:  baseConfig.ActionSets,
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := tc.config1.EqualTo(tc.config2)
			assert.Equal(subT, result, tc.expected)
		})
	}
}

func TestLogLevelString(t *testing.T) {
	testCases := []struct {
		name     string
		logLevel LogLevel
		expected string
	}{
		{
			name:     "error level",
			logLevel: LogLevelError,
			expected: "ERROR",
		},
		{

			name:     "warn level",
			logLevel: LogLevelWarn,
			expected: "WARN",
		},
		{
			name:     "info level",
			logLevel: LogLevelInfo,
			expected: "INFO",
		},
		{
			name:     "debug level",
			logLevel: LogLevelDebug,
			expected: "DEBUG",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := tc.logLevel.String()
			assert.Equal(subT, result, tc.expected)
		})
	}
}

func TestBuildObservabilityConfigWithTracing(t *testing.T) {
	testCases := []struct {
		name              string
		observability     *v1beta1.Observability
		expectedTracing   *Tracing
		shouldHaveTracing bool
	}{
		{
			name: "with tracing endpoint",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Debug: ptr.To("true")},
					},
				},
				Tracing: &v1beta1.Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			shouldHaveTracing: true,
			expectedTracing: &Tracing{
				Endpoint: "http://jaeger:14268/api/traces",
			},
		},
		{
			name: "without tracing",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Debug: ptr.To("true")},
					},
				},
			},
			shouldHaveTracing: false,
		},
		{
			name: "with empty tracing endpoint",
			observability: &v1beta1.Observability{
				DataPlane: &v1beta1.DataPlane{
					DefaultLevels: []v1beta1.LogLevel{
						{Info: ptr.To("true")},
					},
				},
				Tracing: &v1beta1.Tracing{
					Endpoint: "",
				},
			},
			shouldHaveTracing: true,
			expectedTracing: &Tracing{
				Endpoint: "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			result := BuildObservabilityConfig(tc.observability)
			assert.Assert(subT, result != nil, "expected observability to be non-nil")

			if tc.shouldHaveTracing {
				assert.Assert(subT, result.Tracing != nil, "expected Tracing to be non-nil")
				assert.Equal(subT, result.Tracing.Endpoint, tc.expectedTracing.Endpoint)
			} else {
				assert.Assert(subT, result.Tracing == nil, "expected Tracing to be nil")
			}
		})
	}
}

func TestConfigToStructWithObservability(t *testing.T) {
	testCases := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name: "config with observability - basic",
			config: &Config{
				Services: map[string]Service{
					"test-service": {
						Type:        "auth",
						Endpoint:    "test-endpoint",
						FailureMode: "deny",
						Timeout:     ptr.To("200ms"),
					},
				},
				ActionSets: []ActionSet{},
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
				},
			},
			expectError: false,
		},
		{
			name: "config with observability and tracing",
			config: &Config{
				Services: map[string]Service{
					"test-service": {
						Type:        "auth",
						Endpoint:    "test-endpoint",
						FailureMode: "deny",
						Timeout:     ptr.To("200ms"),
					},
				},
				ActionSets: []ActionSet{},
				Observability: &Observability{
					DefaultLevel:         ptr.To("INFO"),
					HTTPHeaderIdentifier: ptr.To("x-trace-id"),
					Tracing: &Tracing{
						Endpoint: "http://jaeger:14268/api/traces",
					},
				},
			},
			expectError: false,
		},
		{
			name: "config without observability",
			config: &Config{
				Services: map[string]Service{
					"test-service": {
						Type:        "auth",
						Endpoint:    "test-endpoint",
						FailureMode: "deny",
						Timeout:     ptr.To("200ms"),
					},
				},
				ActionSets: []ActionSet{},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			structPb, err := tc.config.ToStruct()

			if tc.expectError {
				assert.Assert(subT, err != nil, "expected an error")
			} else {
				assert.Assert(subT, err == nil, "expected no error, got: %v", err)
				assert.Assert(subT, structPb != nil, "expected struct to be non-nil")

				// Verify the config can be deserialized back
				deserializedConfig, err := ConfigFromStruct(structPb)
				assert.Assert(subT, err == nil, "expected no error deserializing, got: %v", err)
				assert.Assert(subT, tc.config.EqualTo(deserializedConfig), "configs should be equal after round-trip")
			}
		})
	}
}

func TestConfigToJSONWithObservability(t *testing.T) {
	testCases := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name: "config with observability",
			config: &Config{
				Services: map[string]Service{
					"test-service": {
						Type:        "auth",
						Endpoint:    "test-endpoint",
						FailureMode: "deny",
						Timeout:     ptr.To("200ms"),
					},
				},
				ActionSets: []ActionSet{},
				Observability: &Observability{
					DefaultLevel:         ptr.To("DEBUG"),
					HTTPHeaderIdentifier: ptr.To("x-request-id"),
					Tracing: &Tracing{
						Endpoint: "http://jaeger:14268/api/traces",
					},
				},
			},
			expectError: false,
		},
		{
			name: "config without observability",
			config: &Config{
				Services: map[string]Service{
					"test-service": {
						Type:        "auth",
						Endpoint:    "test-endpoint",
						FailureMode: "deny",
						Timeout:     ptr.To("200ms"),
					},
				},
				ActionSets: []ActionSet{},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			jsonData, err := tc.config.ToJSON()

			if tc.expectError {
				assert.Assert(subT, err != nil, "expected an error")
			} else {
				assert.Assert(subT, err == nil, "expected no error, got: %v", err)
				assert.Assert(subT, jsonData != nil, "expected JSON to be non-nil")
				assert.Assert(subT, len(jsonData.Raw) > 0, "expected JSON raw data to be non-empty")

				// Verify the config can be deserialized back
				deserializedConfig, err := ConfigFromJSON(jsonData)
				assert.Assert(subT, err == nil, "expected no error deserializing, got: %v", err)
				assert.Assert(subT, tc.config.EqualTo(deserializedConfig), "configs should be equal after round-trip")

				// Verify observability is present in JSON if it was in the original config
				if tc.config.Observability != nil {
					jsonStr := string(jsonData.Raw)
					assert.Assert(subT, len(jsonStr) > 0, "JSON string should not be empty")
					// The observability field should be present in the JSON
					assert.Assert(subT, deserializedConfig.Observability != nil, "deserialized config should have observability")
				}
			}
		})
	}
}

func TestBuildConfigForActionSetWithObservability(t *testing.T) {
	logger := logr.Discard()
	actionSets := []ActionSet{
		{
			Name: "test-action-set",
			RouteRuleConditions: RouteRuleConditions{
				Hostnames: []string{"example.com"},
			},
			Actions: []Action{
				{
					ServiceName: "auth-service",
					Scope:       "test-scope",
				},
			},
		},
	}

	testCases := []struct {
		name                  string
		observability         *Observability
		expectedObservability *Observability
	}{
		{
			name:                  "with nil observability",
			observability:         nil,
			expectedObservability: nil,
		},
		{
			name: "with observability config",
			observability: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
			},
			expectedObservability: &Observability{
				DefaultLevel:         ptr.To("DEBUG"),
				HTTPHeaderIdentifier: ptr.To("x-request-id"),
			},
		},
		{
			name: "with observability and tracing",
			observability: &Observability{
				DefaultLevel:         ptr.To("INFO"),
				HTTPHeaderIdentifier: ptr.To("x-trace-id"),
				Tracing: &Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
			expectedObservability: &Observability{
				DefaultLevel:         ptr.To("INFO"),
				HTTPHeaderIdentifier: ptr.To("x-trace-id"),
				Tracing: &Tracing{
					Endpoint: "http://jaeger:14268/api/traces",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			config := BuildConfigForActionSet(actionSets, &logger, tc.observability)

			// Verify the config has the expected structure
			assert.Assert(subT, config.Services != nil, "expected services to be non-nil")
			assert.Assert(subT, config.ActionSets != nil, "expected action sets to be non-nil")
			assert.Equal(subT, len(config.ActionSets), len(actionSets))

			// Verify observability
			if tc.expectedObservability == nil {
				assert.Assert(subT, config.Observability == nil, "expected observability to be nil")
			} else {
				assert.Assert(subT, config.Observability != nil, "expected observability to be non-nil")
				assert.Assert(subT, config.Observability.EqualTo(tc.expectedObservability),
					"observability configs should be equal")
			}

			// Verify required services are present
			assert.Assert(subT, config.Services[AuthServiceName] != Service{}, "auth service should be present")
			assert.Assert(subT, config.Services[RateLimitCheckServiceName] != Service{}, "ratelimit check service should be present")
			assert.Assert(subT, config.Services[RateLimitReportServiceName] != Service{}, "ratelimit report service should be present")
		})
	}
}
