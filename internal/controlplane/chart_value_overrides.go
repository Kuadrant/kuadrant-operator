package controlplane

import (
	"os"
	"strings"
)

// ChartValueOverride applies an environment variable override to Helm chart values
// at render time. Implementations determine how the env var value is
// transformed before injection.
type ChartValueOverride interface {
	Apply(values map[string]any)
}

// ImageReporter is an optional interface that ChartValueOverride implementations
// can implement to report image information for KuadrantControlPlane status.
type ImageReporter interface {
	ImageInfo() (description, image string)
}

// ImageValue sets a RELATED_IMAGE env var as a single string chart value.
// Use for charts that accept a complete image reference as one value.
type ImageValue struct {
	EnvVar      string // RELATED_IMAGE env var name
	ValueKey    string // Helm chart value key
	Description string // human-readable label for status (e.g., "controller")
}

func (m *ImageValue) Apply(values map[string]any) {
	image := os.Getenv(m.EnvVar)
	if image != "" {
		setNestedValue(values, m.ValueKey, image)
	}
}

func (m *ImageValue) ImageInfo() (string, string) {
	return m.Description, os.Getenv(m.EnvVar)
}

// ImageSplitValue splits a RELATED_IMAGE env var into {repository, tag} and
// sets them as a nested map at the specified value key. Use for charts that
// expect separate repository and tag fields (e.g., mcp-gateway).
// RepoField defaults to "repository", TagField defaults to "tag".
type ImageSplitValue struct {
	ImageValue
	RepoField string // defaults to "repository" if empty
	TagField  string // defaults to "tag" if empty
}

func (m *ImageSplitValue) Apply(values map[string]any) {
	image := os.Getenv(m.EnvVar)
	if image == "" {
		return
	}
	repoField := m.RepoField
	if repoField == "" {
		repoField = "repository"
	}
	tagField := m.TagField
	if tagField == "" {
		tagField = "tag"
	}
	repo, tag := parseImageRef(image)
	imageValues := map[string]any{repoField: repo}
	if tag != "" {
		imageValues[tagField] = tag
	}
	setNestedValue(values, m.ValueKey, imageValues)
}

// setNestedValue sets a value at a potentially dotted key path in a nested map.
// For example, setNestedValue(m, "controller.image", "v1") creates
// m["controller"] = map[string]any{"image": "v1"}, merging with existing maps.
func setNestedValue(values map[string]any, key string, val any) {
	parts := strings.Split(key, ".")
	target := values
	for _, part := range parts[:len(parts)-1] {
		existing, ok := target[part]
		if !ok {
			nested := make(map[string]any)
			target[part] = nested
			target = nested
			continue
		}
		nested, ok := existing.(map[string]any)
		if !ok {
			nested = make(map[string]any)
			target[part] = nested
		}
		target = nested
	}
	target[parts[len(parts)-1]] = val
}

func parseImageRef(ref string) (repository, tag string) {
	if atIdx := strings.Index(ref, "@"); atIdx != -1 {
		return ref[:atIdx], ref[atIdx:]
	}
	if colonIdx := strings.LastIndex(ref, ":"); colonIdx != -1 {
		afterColon := ref[colonIdx+1:]
		if !strings.Contains(afterColon, "/") {
			return ref[:colonIdx], afterColon
		}
	}
	return ref, ""
}
