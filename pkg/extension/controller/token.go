package controller

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"k8s.io/utils/env"
)

const defaultTokenFilePath = "/var/run/secrets/kuadrant/token" //nolint:gosec

type tokenSource func(ctx context.Context) ([]byte, error)

func staticTokenSource(token []byte) tokenSource {
	return func(_ context.Context) ([]byte, error) {
		return token, nil
	}
}

func fileTokenSource(path string) tokenSource {
	return func(ctx context.Context) ([]byte, error) {
		type readResult struct {
			token []byte
			err   error
		}
		done := make(chan readResult, 1)
		go func() {
			token, err := os.ReadFile(path)
			done <- readResult{token: token, err: err}
		}()

		var result readResult
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result = <-done:
		}

		if result.err != nil {
			return nil, fmt.Errorf("failed to read extension token file %q: %w", path, result.err)
		}
		token := bytes.TrimSpace(result.token)
		if len(token) == 0 {
			return nil, fmt.Errorf("extension token file %q is empty", path)
		}
		return token, nil
	}
}

func resolveTokenSource() tokenSource {
	if credential := os.Getenv("KUADRANT_EXTENSION_CREDENTIAL"); credential != "" {
		return staticTokenSource([]byte(credential))
	}
	return fileTokenSource(env.GetString("KUADRANT_EXTENSION_TOKEN_FILE", defaultTokenFilePath))
}
