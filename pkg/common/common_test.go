//go:build unit

package common

import (
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

func TestContains(t *testing.T) {
	testCases := []struct {
		name     string
		slice    []string
		target   string
		expected bool
	}{
		{
			name:     "when slice has one target item then return true",
			slice:    []string{"test-gw"},
			target:   "test-gw",
			expected: true,
		},
		{
			name:     "when slice is empty then return false",
			slice:    []string{},
			target:   "test-gw",
			expected: false,
		},
		{
			name:     "when target is in a slice then return true",
			slice:    []string{"test-gw1", "test-gw2", "test-gw3"},
			target:   "test-gw2",
			expected: true,
		},
		{
			name:     "when no target in a slice then return false",
			slice:    []string{"test-gw1", "test-gw2", "test-gw3"},
			target:   "test-gw4",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if Contains(tc.slice, tc.target) != tc.expected {
				t.Errorf("when slice=%v and target=%s, expected=%v, but got=%v", tc.slice, tc.target, tc.expected, !tc.expected)
			}
		})
	}
}
