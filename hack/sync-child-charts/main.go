// sync-child-charts downloads a child operator's Helm chart from its upstream
// GitHub repo and places it as-is under config/child-operators/charts/<name>/.
// The chart is used at runtime by the kuadrant-operator to deploy child
// operators (CRDs, RBAC, Deployments) at startup.
//
// Usage:
//
//	go run ./hack/sync-child-charts/ \
//	    --repo Kuadrant/dns-operator \
//	    --ref main \
//	    --chart dns-operator \
//	    --output config/child-operators/charts
package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	repo := flag.String("repo", "", "GitHub org/repo (e.g. Kuadrant/dns-operator)")
	ref := flag.String("ref", "main", "Git ref (branch, tag, or SHA)")
	chartName := flag.String("chart", "", "Chart name within the repo's charts/ directory")
	output := flag.String("output", "", "Output directory (e.g. config/child-operators/charts)")
	flag.Parse()

	if *repo == "" || *chartName == "" || *output == "" {
		fmt.Fprintf(os.Stderr, "Usage: sync-child-charts --repo ORG/REPO --chart NAME --output DIR [--ref REF]\n")
		os.Exit(1)
	}

	chartDir := filepath.Join(*output, *chartName)

	fmt.Printf("Syncing %s from %s@%s → %s\n", *chartName, *repo, *ref, chartDir)

	// Clean existing chart
	if err := os.RemoveAll(chartDir); err != nil {
		fatal("removing chart dir: %v", err)
	}

	if err := downloadChart(*repo, *ref, *chartName, chartDir); err != nil {
		fatal("downloading chart: %v", err)
	}

	fmt.Printf("  Done.\n")
}

func downloadChart(repo, ref, chartName, outputDir string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/tarball/%s", repo, ref)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	prefix := fmt.Sprintf("charts/%s/", chartName)
	tr := tar.NewReader(gz)
	found := false

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// Strip the top-level directory (GitHub adds org-repo-sha/)
		parts := strings.SplitN(hdr.Name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		relPath := parts[1]

		if !strings.HasPrefix(relPath, prefix) {
			continue
		}
		found = true

		targetPath := filepath.Join(outputDir, strings.TrimPrefix(relPath, prefix))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			f, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}

	if !found {
		return fmt.Errorf("chart %s not found in %s@%s", chartName, repo, ref)
	}
	return nil
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
