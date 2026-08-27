package controller

import (
	"bytes"
	"fmt"
	"os"

	"k8s.io/utils/env"
)

const defaultTokenFilePath = "/var/run/secrets/kuadrant/token" //nolint:gosec

type tokenSource func() ([]byte, error)

func staticTokenSource(token []byte) tokenSource {
	return func() ([]byte, error) {
		return token, nil
	}
}

func fileTokenSource(path string) tokenSource {
	return func() ([]byte, error) {
		token, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read extension token file %q: %w", path, err)
		}
		token = bytes.TrimSpace(token)
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
