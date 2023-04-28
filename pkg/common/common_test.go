//go:build unit

package common

import (
	"os"
	"reflect"
	"testing"
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
	t.Run("when env var exists & has the same value as the default value then return the env var value", func(t *testing.T) {
		key := "LOG_LEVEL"
		defaultVal := "info"
		val := "info"
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
	t.Run("when key is empty or invalid then return the default value", func(t *testing.T) {
		key := ""
		defaultVal := "production"

		result := FetchEnv(key, defaultVal)

		if result != defaultVal {
			t.Errorf("Expected %v, but got %v", defaultVal, result)
		}
	})
	t.Run("when a runtime error occurs during execution then return the default value", func(t *testing.T) {
		key := "LOG_LEVEL"
		defaultVal := "info"

		os.Unsetenv(key)

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
	t.Run(" when a non-reserved env var name is used as the key then return the default value", func(t *testing.T) {
		key := "MY_VAR"
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
	t.Run("when value is not a pointer type and default value is provided then return default value", func(t *testing.T) {
		var val int
		def := 123

		result := GetDefaultIfNil(&val, def)

		if result != val {
			t.Errorf("Expected %v, but got %v", val, result)
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
