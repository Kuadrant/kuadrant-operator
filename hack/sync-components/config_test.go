package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantCount int
		wantErr   bool
		check     func(t *testing.T, cfg *syncConfig)
	}{
		{
			name: "single component",
			yaml: `components:
  dns-operator:
    repo: Kuadrant/dns-operator
    chart-path: charts/dns-operator
    tracked-branch: main
    ref: main
`,
			wantCount: 1,
			check: func(t *testing.T, cfg *syncConfig) {
				c, ok := cfg.Components["dns-operator"]
				if !ok {
					t.Fatal("dns-operator not found")
				}
				if c.Repo != "Kuadrant/dns-operator" {
					t.Errorf("repo = %q, want %q", c.Repo, "Kuadrant/dns-operator")
				}
				if c.ChartPath != "charts/dns-operator" {
					t.Errorf("chart-path = %q, want %q", c.ChartPath, "charts/dns-operator")
				}
				if c.TrackedBranch != "main" {
					t.Errorf("tracked-branch = %q, want %q", c.TrackedBranch, "main")
				}
				if c.Ref != "main" {
					t.Errorf("ref = %q, want %q", c.Ref, "main")
				}
			},
		},
		{
			name: "multiple components",
			yaml: `components:
  dns-operator:
    repo: Kuadrant/dns-operator
    chart-path: charts/dns-operator
    tracked-branch: main
    ref: main
  mcp-gateway:
    repo: Kuadrant/mcp-gateway
    chart-path: charts/mcp-gateway
    tracked-branch: main
    ref: v0.1.0
`,
			wantCount: 2,
			check: func(t *testing.T, cfg *syncConfig) {
				mcpgw := cfg.Components["mcp-gateway"]
				if mcpgw.Ref != "v0.1.0" {
					t.Errorf("mcp-gateway ref = %q, want %q", mcpgw.Ref, "v0.1.0")
				}
			},
		},
		{
			name:    "empty file",
			yaml:    "",
			wantErr: true,
		},
		{
			name: "missing required field repo",
			yaml: `components:
  dns-operator:
    chart-path: charts/dns-operator
    tracked-branch: main
    ref: main
`,
			wantErr: true,
		},
		{
			name: "missing required field chart-path",
			yaml: `components:
  dns-operator:
    repo: Kuadrant/dns-operator
    tracked-branch: main
    ref: main
`,
			wantErr: true,
		},
		{
			name: "missing required field ref",
			yaml: `components:
  dns-operator:
    repo: Kuadrant/dns-operator
    chart-path: charts/dns-operator
    tracked-branch: main
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "sync.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}

			cfg, err := loadConfig(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cfg.Components) != tt.wantCount {
				t.Errorf("component count = %d, want %d", len(cfg.Components), tt.wantCount)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestUpdateRef(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		component string
		newRef    string
		wantErr   bool
	}{
		{
			name: "updates ref for component",
			input: `components:
  dns-operator:
    repo: Kuadrant/dns-operator
    chart-path: charts/dns-operator
    tracked-branch: main
    ref: old-sha
    auto-merge: true
`,
			component: "dns-operator",
			newRef:    "new-sha-abc123",
		},
		{
			name: "preserves comments and other components",
			input: `# header comment
components:
  dns-operator:
    repo: Kuadrant/dns-operator
    ref: old-sha
#  other:
#    ref: untouched
`,
			component: "dns-operator",
			newRef:    "new-sha",
		},
		{
			name: "component not found",
			input: `components:
  dns-operator:
    repo: Kuadrant/dns-operator
    ref: old-sha
`,
			component: "nonexistent",
			newRef:    "new-sha",
			wantErr:   true,
		},
		{
			name: "preserves indentation",
			input: `components:
    dns-operator:
        repo: Kuadrant/dns-operator
        ref: old-sha
`,
			component: "dns-operator",
			newRef:    "new-sha",
		},
		{
			name: "additional top-level keys below components",
			input: `components:
  dns-operator:
    repo: Kuadrant/dns-operator
    ref: old-sha
settings:
  timeout: 30
`,
			component: "dns-operator",
			newRef:    "new-sha",
		},
		{
			name: "component name is not substring matched",
			input: `components:
  dns:
    repo: Kuadrant/dns
    ref: keep-this
  dns-operator:
    repo: Kuadrant/dns-operator
    ref: old-sha
`,
			component: "dns-operator",
			newRef:    "new-sha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "sync.yaml")
			if err := os.WriteFile(path, []byte(tt.input), 0o600); err != nil {
				t.Fatal(err)
			}

			err := updateRef(path, tt.component, tt.newRef)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var cfg syncConfig
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("failed to parse updated config: %v", err)
			}
			c, ok := cfg.Components[tt.component]
			if !ok {
				t.Fatalf("component %q not found in updated config", tt.component)
			}
			if c.Ref != tt.newRef {
				t.Errorf("ref = %q, want %q", c.Ref, tt.newRef)
			}
		})
	}
}
