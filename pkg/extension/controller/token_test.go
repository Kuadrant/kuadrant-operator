//go:build unit

package controller

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/assert"
)

func TestStaticTokenSource(t *testing.T) {
	source := staticTokenSource([]byte("ephemeral-credential"))

	token, err := source()
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("ephemeral-credential"))
}

func TestFileTokenSource_ReadsCurrentContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	assert.NilError(t, os.WriteFile(path, []byte("first-token\n"), 0o600))

	source := fileTokenSource(path)

	token, err := source()
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("first-token"))

	assert.NilError(t, os.WriteFile(path, []byte("rotated-token\n"), 0o600))

	token, err = source()
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("rotated-token"))
}

func TestFileTokenSource_MissingFile(t *testing.T) {
	source := fileTokenSource(filepath.Join(t.TempDir(), "absent"))

	_, err := source()
	assert.ErrorContains(t, err, "failed to read extension token file")
}

func TestFileTokenSource_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	assert.NilError(t, os.WriteFile(path, []byte("  \n"), 0o600))

	source := fileTokenSource(path)

	_, err := source()
	assert.ErrorContains(t, err, "is empty")
}

func TestResolveTokenSource_CredentialTakesPrecedence(t *testing.T) {
	t.Setenv("KUADRANT_EXTENSION_CREDENTIAL", "builtin-credential")
	t.Setenv("KUADRANT_EXTENSION_TOKEN_FILE", filepath.Join(t.TempDir(), "unused"))

	token, err := resolveTokenSource()()
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("builtin-credential"))
}

func TestResolveTokenSource_FallsBackToTokenFile(t *testing.T) {
	t.Setenv("KUADRANT_EXTENSION_CREDENTIAL", "")
	path := filepath.Join(t.TempDir(), "token")
	assert.NilError(t, os.WriteFile(path, []byte("sa-token"), 0o600))
	t.Setenv("KUADRANT_EXTENSION_TOKEN_FILE", path)

	token, err := resolveTokenSource()()
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("sa-token"))
}
