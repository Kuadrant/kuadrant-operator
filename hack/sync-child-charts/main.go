// sync-child-charts downloads a child operator's Helm chart from its upstream
// GitHub repo and splits it into three outputs under a base directory:
//
//   - crds/<name>.yaml: CRDs extracted by rendering the chart and from the
//     native crds/ directory. These are included in the kuadrant-operator
//     bundle and Helm chart by the installer (OLM or Helm).
//   - rbac/<name>.yaml: ClusterRoles extracted from the rendered chart output.
//     These are also managed by the installer, not applied at runtime.
//   - charts/<name>/: The raw chart (Chart.yaml, values.yaml, templates/) copied
//     as-is for runtime rendering by the kuadrant-operator when a Kuadrant CR
//     is created.
//
// The tool uses the Helm SDK to render charts, ensuring correct resource
// classification regardless of chart complexity (simple kustomize-generated
// charts or mature charts with helpers and conditionals).
//
// Usage:
//
//	go run ./hack/sync-child-charts/ \
//	    --repo Kuadrant/dns-operator \
//	    --ref main \
//	    --chart dns-operator \
//	    --output config/child-operators
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"
)

func main() {
	repo := flag.String("repo", "", "GitHub org/repo (e.g. Kuadrant/dns-operator)")
	ref := flag.String("ref", "main", "Git ref (branch, tag, or SHA)")
	chartName := flag.String("chart", "", "Chart name within the repo's charts/ directory")
	output := flag.String("output", "", "Base output directory (e.g. config/child-operators)")
	flag.Parse()

	if *repo == "" || *chartName == "" || *output == "" {
		fmt.Fprintf(os.Stderr, "Usage: sync-child-charts --repo ORG/REPO --chart NAME --output DIR [--ref REF]\n")
		os.Exit(1)
	}

	fmt.Printf("Syncing %s from %s@%s...\n", *chartName, *repo, *ref)

	tmpDir, err := os.MkdirTemp("", "sync-child-chart-*")
	if err != nil {
		fatal("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download and extract chart from GitHub
	chartDir := filepath.Join(tmpDir, *chartName)
	if err := downloadChart(*repo, *ref, *chartName, chartDir); err != nil {
		fatal("downloading chart: %v", err)
	}

	// Load chart with Helm SDK
	chart, err := loader.Load(chartDir)
	if err != nil {
		fatal("loading chart: %v", err)
	}

	// Collect CRDs from the native crds/ directory
	var crds [][]byte
	for _, crd := range chart.CRDObjects() {
		crds = append(crds, crd.File.Data)
	}

	// Render the full chart to classify all resources
	client := action.NewInstall(&action.Configuration{})
	client.ClientOnly = true
	client.DryRun = true
	client.ReleaseName = *chartName
	client.Namespace = "kuadrant-system"
	client.DisableHooks = true
	client.SkipCRDs = true

	rel, err := client.Run(chart, nil)
	if err != nil {
		fatal("rendering chart: %v", err)
	}

	// Classify rendered resources by kind
	var clusterRoles [][]byte
	decoder := kubeyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(rel.Manifest)), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err.Error() == "EOF" {
				break
			}
			fatal("decoding rendered manifest: %v", err)
		}
		if obj.GetKind() == "" {
			continue
		}
		switch obj.GetKind() {
		case "CustomResourceDefinition":
			yamlData, err := sigsyaml.Marshal(obj.Object)
			if err != nil {
				fatal("marshalling CRD: %v", err)
			}
			crds = append(crds, yamlData)
		case "ClusterRole":
			yamlData, err := sigsyaml.Marshal(obj.Object)
			if err != nil {
				fatal("marshalling ClusterRole: %v", err)
			}
			clusterRoles = append(clusterRoles, yamlData)
		}
	}

	crdsDir := filepath.Join(*output, "crds")
	rbacDir := filepath.Join(*output, "rbac")
	chartsDir := filepath.Join(*output, "charts", *chartName)

	// Clean and prepare chart output directory
	if err := os.RemoveAll(chartsDir); err != nil {
		fatal("removing chart dir: %v", err)
	}
	os.MkdirAll(crdsDir, 0o755)
	os.MkdirAll(rbacDir, 0o755)
	os.MkdirAll(chartsDir, 0o755)

	// Write extracted CRDs
	if len(crds) > 0 {
		crdFile := filepath.Join(crdsDir, *chartName+".yaml")
		writeCombinedYAML(crdFile, crds)
		fmt.Printf("  CRDs → crds/%s.yaml (%d resources)\n", *chartName, len(crds))
	}

	// Write extracted ClusterRoles
	if len(clusterRoles) > 0 {
		rbacFile := filepath.Join(rbacDir, *chartName+".yaml")
		writeCombinedYAML(rbacFile, clusterRoles)
		fmt.Printf("  ClusterRoles → rbac/%s.yaml (%d resources)\n", *chartName, len(clusterRoles))
	}

	// Copy raw chart files for runtime rendering
	copyFile(filepath.Join(chartDir, "Chart.yaml"), filepath.Join(chartsDir, "Chart.yaml"))
	copyFile(filepath.Join(chartDir, "values.yaml"), filepath.Join(chartsDir, "values.yaml"))
	if err := copyDir(filepath.Join(chartDir, "templates"), filepath.Join(chartsDir, "templates")); err != nil {
		fatal("copying templates: %v", err)
	}

	fmt.Printf("  Chart → charts/%s\n", *chartName)
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

func writeCombinedYAML(path string, docs [][]byte) {
	var buf bytes.Buffer
	for _, doc := range docs {
		buf.WriteString("---\n")
		buf.Write(doc)
		if !bytes.HasSuffix(doc, []byte("\n")) {
			buf.WriteString("\n")
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		fatal("writing %s: %v", path, err)
	}
}

func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		fatal("reading %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		fatal("writing %s: %v", dst, err)
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(src, path)
		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
