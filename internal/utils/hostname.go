package utils

// From https://github.com/istio/istio/blob/8af3aff0648fcf7ed3829e82ee0bd741bfc99a2e/pkg/config/host/name.go

import (
	"strings"
)

// Name describes a (possibly wildcarded) hostname
type Name string

// SubsetOf returns true if this hostname is a valid subset of the other hostname. The semantics are
// the same as "Matches", but only in one direction (i.e., h is covered by o).
func (n Name) SubsetOf(o Name) bool {
	hWildcard := n.IsWildCarded()
	oWildcard := o.IsWildCarded()

	if hWildcard {
		if oWildcard {
			// both n and o are wildcards
			if len(n) < len(o) {
				return false
			}
			return strings.HasSuffix(string(n[1:]), string(o[1:]))
		}
		// only n is wildcard
		return false
	}

	if oWildcard {
		// only o is wildcard
		return strings.HasSuffix(string(n), string(o[1:]))
	}

	// both are non-wildcards, so do normal string comparison
	return n == o
}

func (n Name) IsWildCarded() bool {
	return len(n) > 0 && n[0] == '*'
}

func (n Name) String() string {
	return string(n)
}

// CompareHostnamesSpecificity returns true if hostname1 is more specific than hostname2
func CompareHostnamesSpecificity(hostname1, hostname2 string) bool {
	labels1 := len(strings.Split(hostname1, "."))
	labels2 := len(strings.Split(hostname2, "."))
	if labels1 != labels2 {
		return labels1 > labels2
	}
	hasWildcard1 := strings.HasPrefix(hostname1, "*")
	hasWildcard2 := strings.HasPrefix(hostname2, "*")
	if hasWildcard1 != hasWildcard2 {
		return !hasWildcard1
	}
	return hostname1 < hostname2
}
