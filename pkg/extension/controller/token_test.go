//go:build unit

package controller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"gotest.tools/assert"
)

func TestStaticTokenSource(t *testing.T) {
	source := staticTokenSource([]byte("ephemeral-credential"))

	token, err := source(context.Background())
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("ephemeral-credential"))
}

func TestFileTokenSource_ReadsCurrentContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	assert.NilError(t, os.WriteFile(path, []byte("first-token\n"), 0o600))

	source := fileTokenSource(path)

	token, err := source(context.Background())
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("first-token"))

	assert.NilError(t, os.WriteFile(path, []byte("rotated-token\n"), 0o600))

	token, err = source(context.Background())
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("rotated-token"))
}

func TestFileTokenSource_MissingFile(t *testing.T) {
	source := fileTokenSource(filepath.Join(t.TempDir(), "absent"))

	_, err := source(context.Background())
	assert.ErrorContains(t, err, "failed to read extension token file")
}

func TestFileTokenSource_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	assert.NilError(t, os.WriteFile(path, []byte("  \n"), 0o600))

	source := fileTokenSource(path)

	_, err := source(context.Background())
	assert.ErrorContains(t, err, "is empty")
}

func TestFileTokenSource_HonorsCancellationWhileBlocked(t *testing.T) {
	// A FIFO with no writer makes os.ReadFile block until cancellation unblocks it.
	path := filepath.Join(t.TempDir(), "fifo")
	assert.NilError(t, syscall.Mkfifo(path, 0o600))

	source := fileTokenSource(path)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := source(ctx)
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		assert.Assert(t, errors.Is(err, context.Canceled))
	case <-time.After(5 * time.Second):
		t.Fatal("fileTokenSource did not return after context cancellation")
	}
}

func TestResolveTokenSource_CredentialTakesPrecedence(t *testing.T) {
	t.Setenv("KUADRANT_EXTENSION_CREDENTIAL", "builtin-credential")
	t.Setenv("KUADRANT_EXTENSION_TOKEN_FILE", filepath.Join(t.TempDir(), "unused"))

	token, err := resolveTokenSource()(context.Background())
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("builtin-credential"))
}

func TestResolveTokenSource_FallsBackToTokenFile(t *testing.T) {
	t.Setenv("KUADRANT_EXTENSION_CREDENTIAL", "")
	path := filepath.Join(t.TempDir(), "token")
	assert.NilError(t, os.WriteFile(path, []byte("sa-token"), 0o600))
	t.Setenv("KUADRANT_EXTENSION_TOKEN_FILE", path)

	token, err := resolveTokenSource()(context.Background())
	assert.NilError(t, err)
	assert.DeepEqual(t, token, []byte("sa-token"))
}
