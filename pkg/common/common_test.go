//go:build unit

package common

import (
	"fmt"
	"reflect"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestValidSubdomains(t *testing.T) {
	testCases := []struct {
		name             string
		domains          []string
		subdomains       []string
		expected         bool
		expectedHostname string
	}{
		{"nil", nil, nil, true, ""},
		{"nil subdomains", []string{"*.example.com"}, nil, true, ""},
		{"nil domains", nil, []string{"*.example.com"}, false, "*.example.com"},
		{"dot matters", []string{"*.example.com"}, []string{"example.com"}, false, "example.com"},
		{"dot matters2", []string{"example.com"}, []string{"*.example.com"}, false, "*.example.com"},
		{"happy path", []string{"*.example.com", "*.net"}, []string{"*.other.net", "test.example.com"}, true, ""},
		{"not all match", []string{"*.example.com", "*.net"}, []string{"*.other.com", "*.example.com"}, false, "*.other.com"},
		{"wildcard in subdomains does not match", []string{"*.example.com", "*.net"}, []string{"*", "*.example.com"}, false, "*"},
		{"wildcard in domains matches all", []string{"*", "*.net"}, []string{"*.net", "*.example.com"}, true, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			valid, hostname := ValidSubdomains(tc.domains, tc.subdomains)
			if valid != tc.expected {
				subT.Errorf("expected (%t), got (%t)", tc.expected, valid)
			}
			if hostname != tc.expectedHostname {
				subT.Errorf("expected hostname (%s), got (%s)", tc.expectedHostname, hostname)
			}
		})
	}
}

func TestFind(t *testing.T) {
	s := []string{"a", "ab", "abc"}

	if r, found := Find(s, func(el string) bool { return el == "ab" }); !found || r == nil || *r != "ab" {
		t.Error("should have found 'ab' in the slice")
	}

	if r, found := Find(s, func(el string) bool { return len(el) <= 3 }); !found || r == nil || *r != "a" {
		t.Error("should have found 'a' in the slice")
	}

	if r, found := Find(s, func(el string) bool { return len(el) >= 3 }); !found || r == nil || *r != "abc" {
		t.Error("should have found 'abc' in the slice")
	}

	if r, found := Find(s, func(el string) bool { return len(el) == 4 }); found || r != nil {
		t.Error("should not have found anything in the slice")
	}

	i := []int{1, 2, 3}

	if r, found := Find(i, func(el int) bool { return el/3 == 1 }); !found || r == nil || *r != 3 {
		t.Error("should have found 3 in the slice")
	}

	if r, found := Find(i, func(el int) bool { return el == 75 }); found || r != nil {
		t.Error("should not have found anything in the slice")
	}
}

func TestGetEmptySliceIfNil(t *testing.T) {
	t.Run("when a non-nil slice is provided then return same slice", func(t *testing.T) {
		value := []int{1, 2, 3}
		expected := value

		result := GetEmptySliceIfNil(value)

		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})

	t.Run("when a nil slice is provided then return an empty slice of the same type", func(t *testing.T) {
		var value []int
		expected := []int{}

		result := GetEmptySliceIfNil(value)

		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})
}

func TestNamespacedNameToObjectKey(t *testing.T) {
	t.Run("when a namespaced name is provided then return an ObjectKey with corresponding namespace and name", func(t *testing.T) {
		namespacedName := "test-namespace/test-name"
		defaultNamespace := "default"

		result := NamespacedNameToObjectKey(namespacedName, defaultNamespace)

		expected := client.ObjectKey{Name: "test-name", Namespace: "test-namespace"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})

	t.Run("when only a name is provided then return an ObjectKey with the default namespace and provided name", func(t *testing.T) {
		namespacedName := "test-name"
		defaultNamespace := "default"

		result := NamespacedNameToObjectKey(namespacedName, defaultNamespace)

		expected := client.ObjectKey{Name: "test-name", Namespace: "default"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})

	t.Run("when an empty string is provided, then return an ObjectKey with default namespace and empty name", func(t *testing.T) {
		namespacedName := ""
		defaultNamespace := "default"

		result := NamespacedNameToObjectKey(namespacedName, defaultNamespace)

		expected := client.ObjectKey{Name: "", Namespace: "default"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})
}

func TestSameElements(t *testing.T) {
	testCases := []struct {
		name     string
		slice1   []string
		slice2   []string
		expected bool
	}{
		{
			name:     "when slice1 and slice2 contain the same elements then return true",
			slice1:   []string{"test-gw1", "test-gw2", "test-gw3"},
			slice2:   []string{"test-gw1", "test-gw2", "test-gw3"},
			expected: true,
		},
		{
			name:     "when slice1 and slice2 contain unique elements then return false",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw1", "test-gw3"},
			expected: false,
		},
		{
			name:     "when both slices are empty then return true",
			slice1:   []string{},
			slice2:   []string{},
			expected: true,
		},
		{
			name:     "when both slices are nil then return true",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if SameElements(tc.slice1, tc.slice2) != tc.expected {
				t.Errorf("when slice1=%v and slice2=%v, expected=%v, but got=%v", tc.slice1, tc.slice2, tc.expected, !tc.expected)
			}
		})
	}
}

func TestIntersect(t *testing.T) {
	testCases := []struct {
		name     string
		slice1   []string
		slice2   []string
		expected bool
	}{
		{
			name:     "when slice1 and slice2 have one common item then return true",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw1", "test-gw3", "test-gw4"},
			expected: true,
		},
		{
			name:     "when slice1 and slice2 have no common item then return false",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: false,
		},
		{
			name:     "when slice1 is empty then return false",
			slice1:   []string{},
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: false,
		},
		{
			name:     "when slice2 is empty then return false",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{},
			expected: false,
		},
		{
			name:     "when both slices are empty then return false",
			slice1:   []string{},
			slice2:   []string{},
			expected: false,
		},
		{
			name:     "when slice1 is nil then return false",
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: false,
		},
		{
			name:     "when slice2 is nil then return false",
			slice1:   []string{"test-gw1", "test-gw2"},
			expected: false,
		},
		{
			name:     "when both slices are nil then return false",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if Intersect(tc.slice1, tc.slice2) != tc.expected {
				t.Errorf("when slice1=%v and slice2=%v, expected=%v, but got=%v", tc.slice1, tc.slice2, tc.expected, !tc.expected)
			}
		})
	}
}

func TestIntersectWithInts(t *testing.T) {
	testCases := []struct {
		name     string
		slice1   []int
		slice2   []int
		expected bool
	}{
		{
			name:     "when slice1 and slice2 have one common item then return true",
			slice1:   []int{1, 2},
			slice2:   []int{1, 3, 4},
			expected: true,
		},
		{
			name:     "when slice1 and slice2 have no common item then return false",
			slice1:   []int{1, 2},
			slice2:   []int{3, 4},
			expected: false,
		},
		{
			name:     "when slice1 is empty then return false",
			slice1:   []int{},
			slice2:   []int{3, 4},
			expected: false,
		},
		{
			name:     "when slice2 is empty then return false",
			slice1:   []int{1, 2},
			slice2:   []int{},
			expected: false,
		},
		{
			name:     "when both slices are empty then return false",
			slice1:   []int{},
			slice2:   []int{},
			expected: false,
		},
		{
			name:     "when slice1 is nil then return false",
			slice2:   []int{3, 4},
			expected: false,
		},
		{
			name:     "when slice2 is nil then return false",
			slice1:   []int{1, 2},
			expected: false,
		},
		{
			name:     "when both slices are nil then return false",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if Intersect(tc.slice1, tc.slice2) != tc.expected {
				t.Errorf("when slice1=%v and slice2=%v, expected=%v, but got=%v", tc.slice1, tc.slice2, tc.expected, !tc.expected)
			}
		})
	}
}

func TestIntersection(t *testing.T) {
	testCases := []struct {
		name     string
		slice1   []string
		slice2   []string
		expected []string
	}{
		{
			name:     "when slice1 and slice2 have one common item then return that item",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw1", "test-gw3", "test-gw4"},
			expected: []string{"test-gw1"},
		},
		{
			name:     "when slice1 and slice2 have no common item then return nil",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: nil,
		},
		{
			name:     "when slice1 is empty then return nil",
			slice1:   []string{},
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: nil,
		},
		{
			name:     "when slice2 is empty then return nil",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{},
			expected: nil,
		},
		{
			name:     "when both slices are empty then return nil",
			slice1:   []string{},
			slice2:   []string{},
			expected: nil,
		},
		{
			name:     "when slice1 is nil then return nil",
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: nil,
		},
		{
			name:     "when slice2 is nil then return nil",
			slice1:   []string{"test-gw1", "test-gw2"},
			expected: nil,
		},
		{
			name:     "when both slices are nil then return nil",
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if r := Intersection(tc.slice1, tc.slice2); !reflect.DeepEqual(r, tc.expected) {
				t.Errorf("expected=%v; got=%v", tc.expected, r)
			}
		})
	}
}

func TestMap(t *testing.T) {
	slice1 := []int{1, 2, 3, 4}
	f1 := func(x int) int { return x + 1 }
	expected1 := []int{2, 3, 4, 5}
	result1 := Map(slice1, f1)
	t.Run("when mapping an int slice with an increment function then return new slice with the incremented values", func(t *testing.T) {
		if !reflect.DeepEqual(result1, expected1) {
			t.Errorf("result1 = %v; expected %v", result1, expected1)
		}
	})

	slice2 := []string{"hello", "world", "buz", "a"}
	f2 := func(s string) int { return len(s) }
	expected2 := []int{5, 5, 3, 1}
	result2 := Map(slice2, f2)
	t.Run("when mapping a string slice with string->int mapping then return new slice with the mapped values", func(t *testing.T) {
		if !reflect.DeepEqual(result2, expected2) {
			t.Errorf("result2 = %v; expected %v", result2, expected2)
		}
	})

	slice3 := []int{}
	f3 := func(x int) float32 { return float32(x) / 2 }
	expected3 := []float32{}
	result3 := Map(slice3, f3)
	t.Run("when mapping an empty int slice then return an empty slice", func(t *testing.T) {
		if !reflect.DeepEqual(result3, expected3) {
			t.Errorf("result3 = %v; expected %v", result3, expected3)
		}
	})
}

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

func TestHostnamesToStrings(t *testing.T) {
	testCases := []struct {
		name           string
		inputHostnames []gatewayapiv1.Hostname
		expectedOutput []string
	}{
		{
			name:           "when input is empty then return empty output",
			inputHostnames: []gatewayapiv1.Hostname{},
			expectedOutput: []string{},
		},
		{
			name:           "when input has a single precise hostname then return a single string",
			inputHostnames: []gatewayapiv1.Hostname{"example.com"},
			expectedOutput: []string{"example.com"},
		},
		{
			name:           "when input has multiple precise hostnames then return the corresponding strings",
			inputHostnames: []gatewayapiv1.Hostname{"example.com", "test.com", "localhost"},
			expectedOutput: []string{"example.com", "test.com", "localhost"},
		},
		{
			name:           "when input has a wildcard hostname then return the wildcard string",
			inputHostnames: []gatewayapiv1.Hostname{"*.example.com"},
			expectedOutput: []string{"*.example.com"},
		},
		{
			name:           "when input has both precise and wildcard hostnames then return the corresponding strings",
			inputHostnames: []gatewayapiv1.Hostname{"example.com", "*.test.com"},
			expectedOutput: []string{"example.com", "*.test.com"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := HostnamesToStrings(tc.inputHostnames)
			if !reflect.DeepEqual(tc.expectedOutput, output) {
				t.Errorf("Unexpected output. Expected %v but got %v", tc.expectedOutput, output)
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
