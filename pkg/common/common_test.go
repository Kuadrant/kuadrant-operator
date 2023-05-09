//go:build unit

package common

import (
	"os"
	"reflect"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
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

func TestFetchEnv(t *testing.T) {
	t.Run("when env var exists & has a value different from the default value then return the env var value", func(t *testing.T) {
		key := "LOG_LEVEL"
		defaultVal := "info"
		val := "debug"
		os.Setenv(key, val)
		defer os.Unsetenv(key)

		result := FetchEnv(key, defaultVal)

		if result != val {
			t.Errorf("Expected %v, but got %v", val, result)
		}
	})
	t.Run("when env var exists but has an empty value then return the empty value", func(t *testing.T) {
		key := "LOG_MODE"
		defaultVal := "production"
		val := ""
		os.Setenv(key, val)
		defer os.Unsetenv(key)

		result := FetchEnv(key, defaultVal)

		if result != val {
			t.Errorf("Expected %v, but got %v", val, result)
		}
	})
	t.Run("when env var does not exist & the default value is used then return the default value", func(t *testing.T) {
		key := "LOG_MODE"
		defaultVal := "production"

		result := FetchEnv(key, defaultVal)

		if result != defaultVal {
			t.Errorf("Expected %v, but got %v", defaultVal, result)
		}
	})
	t.Run("when default value is an empty string then return an empty string", func(t *testing.T) {
		key := "LOG_MODE"
		defaultVal := ""

		result := FetchEnv(key, defaultVal)

		if result != defaultVal {
			t.Errorf("Expected %v, but got %v", defaultVal, result)
		}
	})
}

func TestGetDefaultIfNil(t *testing.T) {
	t.Run("when value is non-nil pointer type and default value is provided then return value", func(t *testing.T) {
		val := "test"
		def := "default"

		result := GetDefaultIfNil(&val, def)

		if result != val {
			t.Errorf("Expected %v, but got %v", val, result)
		}
	})
	t.Run("when value is nil pointer type and default value is provided then return default value", func(t *testing.T) {
		var val *string
		def := "default"

		result := GetDefaultIfNil(val, def)

		if result != def {
			t.Errorf("Expected %v, but got %v", def, result)
		}
	})
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
			name:           "when namespace contains more than one '#' then return an error",
			namespace:      "exampleNS/exampleGW#domain.com#extra",
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
			name:           "when namespace only has gateway name (missing 'namespace/') then return an error",
			namespace:      "exampleGW#domain.com",
			expectedKey:    client.ObjectKey{},
			expectedDomain: "",
			expectedError:  true,
		},
		{
			name:           "when namespace only has namespace name (missing '/gwName') then return an error",
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
