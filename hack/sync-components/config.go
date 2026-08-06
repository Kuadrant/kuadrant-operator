package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type component struct {
	Repo          string `yaml:"repo" json:"repo"`
	ChartPath     string `yaml:"chart-path" json:"chart-path"`
	TrackedBranch string `yaml:"tracked-branch" json:"tracked-branch"`
	Ref           string `yaml:"ref" json:"ref"`
	AutoMerge     bool   `yaml:"auto-merge" json:"auto-merge"`
}

type syncConfig struct {
	Components map[string]component `yaml:"components"`
}

func loadConfig(path string) (*syncConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return loadConfigFromBytes(data)
}

func loadConfigFromBytes(data []byte) (*syncConfig, error) {
	var cfg syncConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if len(cfg.Components) == 0 {
		return nil, fmt.Errorf("no components defined")
	}

	for name, c := range cfg.Components {
		if c.Repo == "" {
			return nil, fmt.Errorf("component %q: repo is required", name)
		}
		if c.ChartPath == "" {
			return nil, fmt.Errorf("component %q: chart-path is required", name)
		}
		if c.Ref == "" {
			return nil, fmt.Errorf("component %q: ref is required", name)
		}
	}

	return &cfg, nil
}

func updateRef(path, componentName, newRef string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	inComponent := false
	updated := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == componentName+":" {
			inComponent = true
			continue
		}

		if inComponent && strings.HasPrefix(trimmed, "ref:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			lines[i] = indent + "ref: " + newRef
			updated = true
			inComponent = false
			continue
		}

		// Left the component block: a non-empty, non-comment line at a
		// lower indent level (i.e. not starting with spaces on the raw line).
		if inComponent && len(trimmed) > 0 && !strings.HasPrefix(trimmed, "#") {
			leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
			if leadingSpaces == 0 {
				inComponent = false
			}
		}
	}

	if !updated {
		return fmt.Errorf("ref field for component %q not found in %s", componentName, path)
	}

	// Write to a temporary file in the same directory and atomically rename
	// into place to avoid leaving a partial sync.yaml on write failure.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sync-yaml-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(strings.Join(lines, "\n")); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}
	return os.Rename(tmpPath, path)
}
