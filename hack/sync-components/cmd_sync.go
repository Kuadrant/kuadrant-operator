// cmd_sync implements the "sync" subcommand: resolves each component's
// tracked branch to a commit SHA, downloads the chart at that SHA, and
// updates the ref in sync.yaml. Outputs JSON results to stdout.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type syncResult struct {
	Repo    string `json:"repo"`
	OldRef  string `json:"old-ref"`
	NewRef  string `json:"new-ref"`
	Changed bool   `json:"changed"`
	Status  string `json:"status"`
}

func cmdSync(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	configPath := fs.String("config", "component-charts/sync.yaml", "Path to sync config file")
	componentName := fs.String("component", "", "Sync a specific component (default: all)")
	fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	components := cfg.Components
	if *componentName != "" {
		c, ok := cfg.Components[*componentName]
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: component %q not found in config\n", *componentName)
			os.Exit(1)
		}
		components = map[string]component{*componentName: c}
	}

	names := make([]string, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make(map[string]syncResult)
	failed := false

	for _, name := range names {
		result := syncOne(name, components[name], *configPath)
		results[name] = result
		if result.Status == "error" {
			failed = true
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing results: %v\n", err)
		os.Exit(1)
	}

	if failed {
		os.Exit(1)
	}
}

func syncOne(name string, c component, configPath string) syncResult {
	outputBase := filepath.Dir(configPath)
	chartDir := filepath.Join(outputBase, name)

	resolveRef := c.Ref
	if c.TrackedBranch != "" {
		resolveRef = c.TrackedBranch
	}

	sha, err := resolveCommitSHA(c.Repo, resolveRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving %s@%s: %v\n", c.Repo, resolveRef, err)
		return syncResult{Repo: c.Repo, OldRef: c.Ref, Status: "error"}
	}

	if sha == c.Ref {
		fmt.Fprintf(os.Stderr, "Syncing %s from %s@%s\n", name, c.Repo, sha[:12])
		fmt.Fprintf(os.Stderr, "  No changes (already at %s).\n", sha[:12])
		return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Changed: false, Status: "up-to-date"}
	}

	fmt.Fprintf(os.Stderr, "Syncing %s from %s@%s\n", name, c.Repo, sha[:12])
	if c.Ref != "" && c.Ref != c.TrackedBranch {
		fmt.Fprintf(os.Stderr, "  Previous: %s\n", c.Ref[:min(12, len(c.Ref))])
	}

	oldHash, _ := hashDir(chartDir) // tolerate missing chart dir on first sync

	tmpDir, err := os.MkdirTemp(outputBase, ".sync-component-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Status: "error"}
	}

	if err := downloadChart(c.Repo, sha, c.ChartPath, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		fmt.Fprintf(os.Stderr, "Error syncing %s: %v\n", name, err)
		return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Status: "error"}
	}

	newHash, err := hashDir(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		fmt.Fprintf(os.Stderr, "Error hashing downloaded chart for %s: %v\n", name, err)
		return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Status: "error"}
	}

	if oldHash == newHash {
		os.RemoveAll(tmpDir)
		if err := updateRef(configPath, name, sha); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating ref for %s: %v\n", name, err)
			return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Status: "error"}
		}
		fmt.Fprintf(os.Stderr, "  Ref updated to %s (chart content unchanged).\n", sha[:12])
		return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Changed: true, Status: "ref-updated"}
	}

	// Backup existing chart so we can restore if the ref update fails.
	backupDir := chartDir + ".bak"
	os.RemoveAll(backupDir)
	hasBackup := false
	if _, err := os.Stat(chartDir); err == nil {
		if err := os.Rename(chartDir, backupDir); err != nil {
			os.RemoveAll(tmpDir)
			fmt.Fprintf(os.Stderr, "Error backing up %s: %v\n", chartDir, err)
			return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Status: "error"}
		}
		hasBackup = true
	}

	if err := os.Rename(tmpDir, chartDir); err != nil {
		if hasBackup {
			os.Rename(backupDir, chartDir)
		}
		fmt.Fprintf(os.Stderr, "Error moving chart to %s: %v\n", chartDir, err)
		return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Status: "error"}
	}

	if err := updateRef(configPath, name, sha); err != nil {
		// Restore backup on ref update failure
		os.RemoveAll(chartDir)
		if hasBackup {
			os.Rename(backupDir, chartDir)
		}
		fmt.Fprintf(os.Stderr, "Error updating ref for %s: %v\n", name, err)
		return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Status: "error"}
	}
	os.RemoveAll(backupDir)
	fmt.Fprintf(os.Stderr, "  Updated.\n")
	return syncResult{Repo: c.Repo, OldRef: c.Ref, NewRef: sha, Changed: true, Status: "updated"}
}

func hashDir(dir string) (string, error) {
	h := sha256.New()
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(files)
	for _, f := range files {
		fmt.Fprintf(h, "%s\n", f)
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return "", err
		}
		h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
