package main

import (
	"slices"
	"testing"
)

func TestFilterReleaseBranches(t *testing.T) {
	tests := []struct {
		name     string
		branches []string
		want     []string
	}{
		{
			name:     "empty list",
			branches: []string{},
			want:     []string{},
		},
		{
			name:     "only default branch",
			branches: []string{"main", "develop"},
			want:     []string{},
		},
		{
			name:     "single release branch",
			branches: []string{"main", "release-1.6", "develop"},
			want:     []string{"release-1.6"},
		},
		{
			name:     "multiple release branches",
			branches: []string{"main", "release-1.6", "release-1.5", "release-0.27", "develop"},
			want:     []string{"release-1.6", "release-1.5", "release-0.27"},
		},
		{
			name:     "filters out invalid release formats",
			branches: []string{"release-1", "release-v1.6", "release-1.6.0", "release-1.6", "release-workflow-update"},
			want:     []string{"release-1.6"},
		},
		{
			name:     "case sensitive matching",
			branches: []string{"Release-1.6", "release-1.6", "RELEASE-1.6"},
			want:     []string{"release-1.6"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterReleaseBranches(tt.branches)
			if !slices.Equal(got, tt.want) {
				t.Errorf("filterReleaseBranches(%v) = %v, want %v", tt.branches, got, tt.want)
			}
		})
	}
}

func TestFindTargets(t *testing.T) {
	tests := []struct {
		name              string
		branchConfigs     map[string]*syncConfig
		defaultBranch     string
		componentName     string
		sourceBranch      string
		wantBranches      []string
		wantAutoMergeVals []bool
	}{
		{
			name: "no matching component",
			branchConfigs: map[string]*syncConfig{
				"main": {Components: map[string]component{}},
			},
			defaultBranch: "main",
			componentName: "dns-operator",
			sourceBranch:  "main",
			wantBranches:  []string{},
		},
		{
			name: "default branch matches",
			branchConfigs: map[string]*syncConfig{
				"main": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "main", AutoMerge: false},
				}},
			},
			defaultBranch:     "main",
			componentName:     "dns-operator",
			sourceBranch:      "main",
			wantBranches:      []string{"main"},
			wantAutoMergeVals: []bool{false},
		},
		{
			name: "release branch matches",
			branchConfigs: map[string]*syncConfig{
				"main": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "main", AutoMerge: false},
				}},
				"release-1.6": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "release-1.6", AutoMerge: true},
				}},
			},
			defaultBranch:     "main",
			componentName:     "dns-operator",
			sourceBranch:      "release-1.6",
			wantBranches:      []string{"release-1.6"},
			wantAutoMergeVals: []bool{true},
		},
		{
			name: "default branch source excludes release branches",
			branchConfigs: map[string]*syncConfig{
				"main": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "main", AutoMerge: false},
				}},
				"release-1.6": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "main", AutoMerge: true},
				}},
			},
			defaultBranch:     "main",
			componentName:     "dns-operator",
			sourceBranch:      "main",
			wantBranches:      []string{"main"},
			wantAutoMergeVals: []bool{false},
		},
		{
			name: "multiple branches, mixed results",
			branchConfigs: map[string]*syncConfig{
				"main": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "main", AutoMerge: false},
					"other-op":     {TrackedBranch: "develop", AutoMerge: false},
				}},
				"release-1.6": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "release-1.6", AutoMerge: true},
				}},
				"release-1.5": {Components: map[string]component{
					"other-op": {TrackedBranch: "release-1.5", AutoMerge: true},
				}},
			},
			defaultBranch:     "main",
			componentName:     "dns-operator",
			sourceBranch:      "main",
			wantBranches:      []string{"main"},
			wantAutoMergeVals: []bool{false},
		},
		{
			name: "tracked branch mismatch excludes branch",
			branchConfigs: map[string]*syncConfig{
				"main": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "main", AutoMerge: false},
				}},
				"release-1.6": {Components: map[string]component{
					"dns-operator": {TrackedBranch: "main", AutoMerge: true},
				}},
			},
			defaultBranch: "main",
			componentName: "dns-operator",
			sourceBranch:  "release-1.6",
			wantBranches:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findTargets(tt.branchConfigs, tt.defaultBranch, tt.componentName, tt.sourceBranch)
			if len(got) != len(tt.wantBranches) {
				t.Errorf("findTargets() returned %d branches, want %d", len(got), len(tt.wantBranches))
				return
			}
			// Build map of results for order-independent comparison
			gotByBranch := make(map[string]bool)
			for _, result := range got {
				gotByBranch[result.Branch] = result.AutoMerge
			}
			// Check each expected branch
			for i, wantBranch := range tt.wantBranches {
				if autoMerge, ok := gotByBranch[wantBranch]; !ok {
					t.Errorf("findTargets() missing branch %q", wantBranch)
				} else if autoMerge != tt.wantAutoMergeVals[i] {
					t.Errorf("findTargets()[%q].AutoMerge = %v, want %v", wantBranch, autoMerge, tt.wantAutoMergeVals[i])
				}
			}
		})
	}
}
