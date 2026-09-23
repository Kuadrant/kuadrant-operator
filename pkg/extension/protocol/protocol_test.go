//go:build unit

package protocol

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"gotest.tools/assert"
)

func TestCompatible(t *testing.T) {
	testCases := []struct {
		name       string
		a          string
		b          string
		compatible bool
	}{
		{"identical unstable", "0.1.0", "0.1.0", true},
		{"unstable patch skew", "0.1.0", "0.1.7", true},
		{"unstable patch skew reversed", "0.1.7", "0.1.0", true},
		{"unstable minor break", "0.1.0", "0.2.0", false},
		{"unstable minor break reversed", "0.2.0", "0.1.0", false},
		{"unstable to stable", "0.1.0", "1.0.0", false},
		{"identical stable", "1.0.0", "1.0.0", true},
		{"stable minor skew", "1.0.0", "1.4.2", true},
		{"stable minor skew reversed", "1.4.2", "1.0.0", true},
		{"stable major break", "1.9.9", "2.0.0", false},
		{"prerelease within band", "0.1.0-rc1", "0.1.0", true},
		{"empty left", "", "0.1.0", false},
		{"empty right", "0.1.0", "", false},
		{"partial version", "0.1", "0.1.0", false},
		{"not a version", "banana", "0.1.0", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Compatible(tc.a, tc.b)
			if tc.compatible {
				assert.NilError(t, err)
			} else {
				assert.Assert(t, err != nil)
			}
		})
	}
}

func TestVersionIsStrictSemver(t *testing.T) {
	_, err := semver.StrictNewVersion(Version)
	assert.NilError(t, err)
}
