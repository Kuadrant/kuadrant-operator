//go:build unit

package common

import (
	"fmt"
	"reflect"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestMergeMapStringString(t *testing.T) {
	testCases := []struct {
		name          string
		existing      map[string]string
		desired       map[string]string
		expected      bool
		expectedState map[string]string
	}{
		{
			name:          "when existing and desired are empty then return false and not modify the existing map",
			existing:      map[string]string{},
			desired:       map[string]string{},
			expected:      false,
			expectedState: map[string]string{},
		},
		{
			name:          "when existing is empty and desired has values then return true and set the values in the existing map",
			existing:      map[string]string{},
			desired:       map[string]string{"a": "1", "b": "2"},
			expected:      true,
			expectedState: map[string]string{"a": "1", "b": "2"},
		},
		{
			name:          "when existing has some values and desired has different/new values then return true and modify the existing map",
			existing:      map[string]string{"a": "1", "b": "2"},
			desired:       map[string]string{"a": "3", "c": "4"},
			expected:      true,
			expectedState: map[string]string{"a": "3", "b": "2", "c": "4"},
		},
		{
			name:          "when existing has all the values from desired then return false and not modify the existing map",
			existing:      map[string]string{"a": "1", "b": "2"},
			desired:       map[string]string{"a": "1", "b": "2"},
			expected:      false,
			expectedState: map[string]string{"a": "1", "b": "2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			existingCopy := make(map[string]string, len(tc.existing))
			for k, v := range tc.existing {
				existingCopy[k] = v
			}
			modified := MergeMapStringString(&existingCopy, tc.desired)

			if modified != tc.expected {
				t.Errorf("MergeMapStringString(%v, %v) returned %v; expected %v", tc.existing, tc.desired, modified, tc.expected)
			}

			if !reflect.DeepEqual(existingCopy, tc.expectedState) {
				t.Errorf("MergeMapStringString(%v, %v) modified the existing map to %v; expected %v", tc.existing, tc.desired, existingCopy, tc.expectedState)
			}
		})
	}
}

func TestUnMarshallLimitNamespace(t *testing.T) {
	testCases := []struct {
		name           string
		namespace      string
		expectedKey    client.ObjectKey
		expectedDomain string
		expectedError  bool
	}{
		{
			name:           "when namespace is valid and contains both namespace and domain then return the correct values",
			namespace:      "exampleNS/exampleGW#domain.com",
			expectedKey:    client.ObjectKey{Name: "exampleGW", Namespace: "exampleNS"},
			expectedDomain: "domain.com",
			expectedError:  false,
		},
		{
			name:           "when namespace is invalid (no '#domain') then return an error",
			namespace:      "exampleNS/exampleGW",
			expectedKey:    client.ObjectKey{},
			expectedDomain: "",
			expectedError:  true,
		},
		{
			name:           "when namespace missing both namespace and gateway parts then return an error",
			namespace:      "#domain.com",
			expectedKey:    client.ObjectKey{},
			expectedDomain: "",
			expectedError:  true,
		},
		{
			name:           "when namespace has no domain name then return correct values",
			namespace:      "exampleNS/exampleGW#",
			expectedKey:    client.ObjectKey{Namespace: "exampleNS", Name: "exampleGW"},
			expectedDomain: "",
			expectedError:  false,
		},
		{
			name:           "when namespace only has gateway name (missing 'namespace/') and domain then return an error",
			namespace:      "exampleGW#domain.com",
			expectedKey:    client.ObjectKey{},
			expectedDomain: "",
			expectedError:  true,
		},
		{
			name:           "when namespace only has namespace name (missing '/gwName') and domain then return an error",
			namespace:      "exampleNS#domain.com",
			expectedKey:    client.ObjectKey{},
			expectedDomain: "",
			expectedError:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			key, domain, err := UnMarshallLimitNamespace(tc.namespace)

			if tc.expectedError {
				if err == nil {
					t.Errorf("Expected an error, but got %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				if key != tc.expectedKey {
					t.Errorf("Expected %v, but got %v", tc.expectedKey, key)
				}

				if domain != tc.expectedDomain {
					t.Errorf("Expected %v, but got %v", tc.expectedDomain, domain)
				}
			}
		})
	}
}

func TestUnMarshallObjectKey(t *testing.T) {
	testCases := []struct {
		name           string
		input          string
		expectedOutput client.ObjectKey
		expectedError  error
	}{
		{
			name:           "when valid key string then return valid ObjectKey",
			input:          "default/object1",
			expectedOutput: client.ObjectKey{Namespace: "default", Name: "object1"},
			expectedError:  nil,
		},
		{
			name:           "when valid key string with non-default namespace then return valid ObjectKey",
			input:          "kube-system/object2",
			expectedOutput: client.ObjectKey{Namespace: "kube-system", Name: "object2"},
			expectedError:  nil,
		},
		{
			name:           "when invalid namespace and name then return empty ObjectKey and error",
			input:          "invalid",
			expectedOutput: client.ObjectKey{},
			expectedError:  fmt.Errorf("failed to split on %s: 'invalid'", string(NamespaceSeparator)),
		},
		{
			name:           "when '#' separator used instead of default separator ('/') then return an error",
			input:          "default#object1",
			expectedOutput: client.ObjectKey{},
			expectedError:  fmt.Errorf("failed to split on %s: 'default#object1'", string(NamespaceSeparator)),
		},
		{
			name:           "when input string is empty then return an error",
			input:          "",
			expectedOutput: client.ObjectKey{},
			expectedError:  fmt.Errorf("failed to split on %s: ''", string(NamespaceSeparator)),
		},
		{
			name:           "when empty namespace and name then return valid empty ObjectKey",
			input:          "/",
			expectedOutput: client.ObjectKey{},
			expectedError:  nil,
		},
		{
			name:           "when valid namespace and empty name (strKey ends with '/') then return valid ObjectKey with namespace only",
			input:          "default/",
			expectedOutput: client.ObjectKey{Namespace: "default", Name: ""},
			expectedError:  nil,
		},
		{
			name:           "when valid name and empty namespace (strKey starts with '/') then return valid ObjectKey with name only",
			input:          "/object",
			expectedOutput: client.ObjectKey{Namespace: "", Name: "object"},
			expectedError:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := UnMarshallObjectKey(tc.input)

			if err != nil && tc.expectedError == nil {
				t.Errorf("unexpected error: got %v, want nil", err)
			} else if err == nil && tc.expectedError != nil {
				t.Errorf("expected error but got nil")
			} else if err != nil && tc.expectedError != nil && err.Error() != tc.expectedError.Error() {
				t.Errorf("unexpected error: got '%v', want '%v'", err, tc.expectedError)
			}

			if output != tc.expectedOutput {
				t.Errorf("unexpected output: got %v, want %v", output, tc.expectedOutput)
			}
		})
	}
}

func TestFilterValidSubdomains(t *testing.T) {
	testCases := []struct {
		name       string
		domains    []gatewayapiv1.Hostname
		subdomains []gatewayapiv1.Hostname
		expected   []gatewayapiv1.Hostname
	}{
		{
			name:       "when all subdomains are valid",
			domains:    []gatewayapiv1.Hostname{"my-app.apps.io", "*.acme.com"},
			subdomains: []gatewayapiv1.Hostname{"toystore.acme.com", "my-app.apps.io", "carstore.acme.com"},
			expected:   []gatewayapiv1.Hostname{"toystore.acme.com", "my-app.apps.io", "carstore.acme.com"},
		},
		{
			name:       "when some subdomains are valid and some are not",
			domains:    []gatewayapiv1.Hostname{"my-app.apps.io", "*.acme.com"},
			subdomains: []gatewayapiv1.Hostname{"toystore.acme.com", "my-app.apps.io", "other-app.apps.io"},
			expected:   []gatewayapiv1.Hostname{"toystore.acme.com", "my-app.apps.io"},
		},
		{
			name:       "when none of subdomains are valid",
			domains:    []gatewayapiv1.Hostname{"my-app.apps.io", "*.acme.com"},
			subdomains: []gatewayapiv1.Hostname{"other-app.apps.io"},
			expected:   []gatewayapiv1.Hostname{},
		},
		{
			name:       "when the set of super domains is empty",
			domains:    []gatewayapiv1.Hostname{},
			subdomains: []gatewayapiv1.Hostname{"toystore.acme.com"},
			expected:   []gatewayapiv1.Hostname{},
		},
		{
			name:       "when the set of subdomains is empty",
			domains:    []gatewayapiv1.Hostname{"my-app.apps.io", "*.acme.com"},
			subdomains: []gatewayapiv1.Hostname{},
			expected:   []gatewayapiv1.Hostname{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if r := FilterValidSubdomains(tc.domains, tc.subdomains); !reflect.DeepEqual(r, tc.expected) {
				t.Errorf("expected=%v; got=%v", tc.expected, r)
			}
		})
	}
}
