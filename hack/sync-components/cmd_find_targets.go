// cmd_find_targets implements the "find-targets" subcommand: discovers
// candidate target branches in a repo and checks which ones have a
// component-charts/sync.yaml that tracks a given component at a given
// source branch. Used by the sync workflow to determine where to sync
// when a component repo dispatches an event.
//
// Candidate branches are:
// - The repo's default branch
// - Branches matching ^release-[0-9]+\.[0-9]+$ that have component-charts/sync.yaml
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
)

type targetBranch struct {
	Branch    string `json:"branch"`
	AutoMerge bool   `json:"auto-merge"`
}

func filterReleaseBranches(branches []string) []string {
	releaseBranchRegex := regexp.MustCompile(`^release-[0-9]+\.[0-9]+$`)
	matches := make([]string, 0, len(branches))
	for _, branch := range branches {
		if releaseBranchRegex.MatchString(branch) {
			matches = append(matches, branch)
		}
	}
	return matches
}

func findTargets(branchConfigs map[string]*syncConfig, defaultBranch, componentName, sourceBranch string) []targetBranch {
	matches := make([]targetBranch, 0)
	for branch, cfg := range branchConfigs {
		// When source branch is the default branch, only match the default branch.
		// This prevents the default branch from syncing to release branches that
		// also happen to track the default branch.
		if sourceBranch == defaultBranch && branch != defaultBranch {
			continue
		}

		c, ok := cfg.Components[componentName]
		if !ok || c.TrackedBranch != sourceBranch {
			continue
		}

		matches = append(matches, targetBranch{
			Branch:    branch,
			AutoMerge: c.AutoMerge,
		})
	}
	return matches
}

func cmdFindTargets(args []string) {
	fs := flag.NewFlagSet("find-targets", flag.ExitOnError)
	repo := fs.String("repo", "", "GitHub owner/repo to search (e.g. Kuadrant/kuadrant-operator)")
	componentName := fs.String("component", "", "Component name to look up in sync.yaml (e.g. dns-operator)")
	sourceBranch := fs.String("source-branch", "", "The upstream branch that changed (e.g. main). "+
		"Matched against the tracked-branch field in sync.yaml")
	fs.Parse(args)

	if *repo == "" || *componentName == "" || *sourceBranch == "" {
		fmt.Fprintf(os.Stderr, "Error: --repo, --component, and --source-branch are required\n")
		os.Exit(1)
	}

	defaultBranch, err := getDefaultBranch(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting default branch: %v\n", err)
		os.Exit(1)
	}

	// List branches and filter for release-* candidates
	branches, err := listBranches(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing branches: %v\n", err)
		os.Exit(1)
	}
	releaseBranches := filterReleaseBranches(branches)

	// Collect candidate branches: default branch plus matching release-* branches.
	// When source is the default branch, only consider the default branch
	// since release branches will be excluded anyway.
	candidateBranches := []string{defaultBranch}
	if *sourceBranch != defaultBranch {
		candidateBranches = append(candidateBranches, releaseBranches...)
	}

	// Load configs for each candidate branch
	branchConfigs := make(map[string]*syncConfig)
	for _, branch := range candidateBranches {
		data, err := getFileContent(*repo, "component-charts/sync.yaml", branch)
		if err != nil {
			var apiErr *ghAPIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
				// This branch doesn't have sync.yaml yet; skip it
				continue
			}
			// Other errors are fatal
			fmt.Fprintf(os.Stderr, "Error fetching sync.yaml from %s@%s: %v\n", *repo, branch, err)
			os.Exit(1)
		}

		cfg, err := loadConfigFromBytes(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing sync.yaml from %s@%s: %v\n", *repo, branch, err)
			os.Exit(1)
		}
		branchConfigs[branch] = cfg
	}

	// Find matching targets
	matches := findTargets(branchConfigs, defaultBranch, *componentName, *sourceBranch)

	if err := json.NewEncoder(os.Stdout).Encode(matches); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing results: %v\n", err)
		os.Exit(1)
	}
}
