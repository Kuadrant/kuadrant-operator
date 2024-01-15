//go:build unit

package utils

import (
	"reflect"
	"testing"

	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestNameSubsetOf(t *testing.T) {
	testCases := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{"equal hostnames", "foo.com", "foo.com", true},
		{"diff hostnames", "foo.com", "bar.com", false},
		{"wildcard hostname not subset of a hostname", "*.com", "foo.com", false},
		{"hostname subset of a wildcard hostname", "foo.com", "*.com", true},
		{"wildcard subdomain is not subset", "*.foo.com", "foo.com", false},
		{"hostname is not subset of wildcard subdomain", "foo.com", "*.foo.com", false},
		{"global wildcard is not subset of wildcard hostname", "*", "*.com", false},
		{"wildcard hostname is subset of global wildcard", "*.com", "*", true},
		{"wildcards with different TLDs", "*.com", "*.org", false},
		{"wildcards at different levels in domain hierarchy", "*.foo.com", "*.bar.foo.com", false},
		{"wildcards with subdomains", "*.foo.com", "*.baz.foo.com", false},
		{"empty hostnames", "", "", true},
		{"one empty hostname", "", "foo.com", false},
		{"multiple wildcards", "*.foo.*.com", "*.foo.*.com", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := Name(tc.a).SubsetOf(Name(tc.b))
			if res != tc.expected {
				subT.Errorf("expected (%t), got (%t)", tc.expected, res)
			}
		})
	}
}

func TestIsWildCarded(t *testing.T) {
	testCases := []struct {
		name     string
		hostname Name
		expected bool
	}{
		{"when wildcard at beginning then return true", "*.example.com", true},
		{"when empty string then return false", "", false},
		{"when no wildcard then return false", "example.com", false},
		{"when wildcard in middle then return false", "subdomain.*.example.com", false},
		{"when wildcard at end then return false", "subdomain.example.*", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := tc.hostname.IsWildCarded()
			if res != tc.expected {
				subT.Errorf("expected (%t) for hostname '%s', but got (%t)", tc.expected, tc.hostname, res)
			}
		})
	}
}

func TestString(t *testing.T) {
	testCases := []struct {
		name     string
		actual   Name
		expected string
	}{
		{"empty name", "", ""},
		{"non-empty name", "example.com", "example.com"},
		{"wildcarded name", "*.com", "*.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := tc.actual.String()
			if res != tc.expected {
				subT.Errorf("expected (%s), got (%s)", tc.expected, res)
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
