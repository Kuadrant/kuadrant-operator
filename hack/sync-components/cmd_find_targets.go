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

	matches := []targetBranch{}

	// Regex for valid release branch names: release-X.Y (e.g., release-1.6)
	releaseBranchRegex := regexp.MustCompile(`^release-[0-9]+\.[0-9]+$`)

	// Collect candidate branches: default branch plus matching release-* branches
	candidateBranches := []string{defaultBranch}

	// List branches to find release-* candidates
	branches, err := listBranches(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing branches: %v\n", err)
		os.Exit(1)
	}
	for _, branch := range branches {
		if releaseBranchRegex.MatchString(branch) {
			candidateBranches = append(candidateBranches, branch)
		}
	}

	// Check each candidate branch for sync.yaml and matching component
	for _, branch := range candidateBranches {
		// If dispatch is from the default branch, only match the default branch.
		// This prevents a dispatch from main from syncing to release branches
		// that also happen to track main.
		if *sourceBranch == defaultBranch && branch != defaultBranch {
			fmt.Fprintf(os.Stderr, "Skipping %s: release branch should not be synced from default branch dispatch\n", branch)
			continue
		}

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

		c, ok := cfg.Components[*componentName]
		if !ok || c.TrackedBranch != *sourceBranch {
			continue
		}

		matches = append(matches, targetBranch{
			Branch:    branch,
			AutoMerge: c.AutoMerge,
		})
	}

	if err := json.NewEncoder(os.Stdout).Encode(matches); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing results: %v\n", err)
		os.Exit(1)
	}
}
