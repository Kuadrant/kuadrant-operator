// cmd_find_targets implements the "find-targets" subcommand: checks the
// default branch of a given repo for a component-charts/sync.yaml that
// tracks a given component at a given source branch. Used by the sync
// workflow to determine whether to sync when a component repo dispatches
// an event.
//
// Currently only checks the default branch. Release branch support can be
// added later when the branch mapping strategy is better defined.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
)

type targetBranch struct {
	Branch    string `json:"branch"`
	AutoMerge bool   `json:"auto-merge"`
}

func cmdFindTargets(args []string) {
	fs := flag.NewFlagSet("find-targets", flag.ExitOnError)
	repo := fs.String("repo", "", "GitHub owner/repo to search (e.g. Kuadrant/kuadrant-operator). "+
		"The default branch is checked for a component-charts/sync.yaml file")
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

	data, err := getFileContent(*repo, "component-charts/sync.yaml", defaultBranch)
	if err != nil {
		var apiErr *ghAPIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			if err := json.NewEncoder(os.Stdout).Encode(matches); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing results: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintf(os.Stderr, "Error fetching sync.yaml from %s@%s: %v\n", *repo, defaultBranch, err)
		os.Exit(1)
	}

	cfg, err := loadConfigFromBytes(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing sync.yaml from %s@%s: %v\n", *repo, defaultBranch, err)
		os.Exit(1)
	}

	c, ok := cfg.Components[*componentName]
	if ok && c.TrackedBranch == *sourceBranch {
		matches = append(matches, targetBranch{
			Branch:    defaultBranch,
			AutoMerge: c.AutoMerge,
		})
	}

	if err := json.NewEncoder(os.Stdout).Encode(matches); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing results: %v\n", err)
		os.Exit(1)
	}
}
