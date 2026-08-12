package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func buildTestTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractChart(t *testing.T) {
	tests := []struct {
		name      string
		chartPath string
		files     map[string]string
		wantFiles map[string]string
		wantErr   bool
	}{
		{
			name:      "simple chart extraction",
			chartPath: "charts/dns-operator",
			files: map[string]string{
				"Kuadrant-dns-operator-abc1234/charts/dns-operator/Chart.yaml":               "apiVersion: v2\nname: dns-operator",
				"Kuadrant-dns-operator-abc1234/charts/dns-operator/values.yaml":              "# empty",
				"Kuadrant-dns-operator-abc1234/charts/dns-operator/templates/manifests.yaml": "kind: Deployment",
				"Kuadrant-dns-operator-abc1234/README.md":                                    "# DNS Operator",
				"Kuadrant-dns-operator-abc1234/go.mod":                                       "module foo",
			},
			wantFiles: map[string]string{
				"Chart.yaml":               "apiVersion: v2\nname: dns-operator",
				"values.yaml":              "# empty",
				"templates/manifests.yaml": "kind: Deployment",
			},
		},
		{
			name:      "chart with crds directory",
			chartPath: "charts/mcp-gateway",
			files: map[string]string{
				"Kuadrant-mcp-gateway-def5678/charts/mcp-gateway/Chart.yaml":                           "apiVersion: v2",
				"Kuadrant-mcp-gateway-def5678/charts/mcp-gateway/values.yaml":                          "image: test",
				"Kuadrant-mcp-gateway-def5678/charts/mcp-gateway/crds/mcpservers.yaml":                 "kind: CRD",
				"Kuadrant-mcp-gateway-def5678/charts/mcp-gateway/templates/_helpers.tpl":               "{{/* helpers */}}",
				"Kuadrant-mcp-gateway-def5678/charts/mcp-gateway/templates/deployment-controller.yaml": "kind: Deployment",
			},
			wantFiles: map[string]string{
				"Chart.yaml":                           "apiVersion: v2",
				"values.yaml":                          "image: test",
				"crds/mcpservers.yaml":                 "kind: CRD",
				"templates/_helpers.tpl":               "{{/* helpers */}}",
				"templates/deployment-controller.yaml": "kind: Deployment",
			},
		},
		{
			name:      "chart not found in tarball",
			chartPath: "charts/nonexistent",
			files: map[string]string{
				"Kuadrant-dns-operator-abc1234/charts/dns-operator/Chart.yaml": "apiVersion: v2",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tarball := buildTestTarball(t, tt.files)
			outputDir := t.TempDir()

			err := extractChart(bytes.NewReader(tarball), tt.chartPath, outputDir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for relPath, wantContent := range tt.wantFiles {
				fullPath := filepath.Join(outputDir, relPath)
				got, err := os.ReadFile(fullPath)
				if err != nil {
					t.Errorf("expected file %s not found: %v", relPath, err)
					continue
				}
				if string(got) != wantContent {
					t.Errorf("file %s content = %q, want %q", relPath, string(got), wantContent)
				}
			}
		})
	}
}
