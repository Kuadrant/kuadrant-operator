//go:build unit

package common

import (
	"testing"
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
