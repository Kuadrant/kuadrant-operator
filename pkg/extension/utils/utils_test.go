//go:build unit

package utils

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MockClient struct {
	client.Client
}

func TestClientFromContext_Success(t *testing.T) {
	expectedClient := &MockClient{}
	ctx := context.WithValue(context.Background(), ClientKey, expectedClient)

	c, err := ClientFromContext(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c != expectedClient {
		t.Errorf("expected client %v, got %v", expectedClient, c)
	}
}

func TestClientFromContext_Missing(t *testing.T) {
	ctx := context.Background()

	c, err := ClientFromContext(ctx)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if c != nil {
		t.Errorf("expected nil client, got %v", c)
	}
}

func TestSchemeFromContext_Success(t *testing.T) {
	expectedScheme := runtime.NewScheme()
	ctx := context.WithValue(context.Background(), SchemeKey, expectedScheme)

	s, err := SchemeFromContext(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s != expectedScheme {
		t.Errorf("expected scheme %v, got %v", expectedScheme, s)
	}
}

func TestSchemeFromContext_Missing(t *testing.T) {
	ctx := context.Background()

	s, err := SchemeFromContext(ctx)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if s != nil {
		t.Errorf("expected nil scheme, got %v", s)
	}
}
