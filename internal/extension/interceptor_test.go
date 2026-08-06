//go:build unit

package extension

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

func authenticatedSessionStore() (*SessionStore, string) {
	store := NewSessionStore(logr.Discard())
	cred := validCredential()
	store.SetCredential("test-extension", cred)
	token, _ := store.Authenticate("test-extension", cred, "TestPolicy")
	return store, token
}

func TestAuthInterceptor_HandshakeAllowed(t *testing.T) {
	store := NewSessionStore(logr.Discard())
	interceptor := NewAuthInterceptor(store, logr.Discard())

	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: extpb.ExtensionService_Handshake_FullMethodName,
	}

	resp, err := interceptor.UnaryInterceptor(context.Background(), nil, info, handler)
	if err != nil {
		t.Fatalf("expected no error for Handshake, got: %v", err)
	}
	if !called {
		t.Fatal("expected handler to be called")
	}
	if resp != "ok" {
		t.Fatalf("expected response %q, got %q", "ok", resp)
	}
}

func TestAuthInterceptor_RejectWithoutToken(t *testing.T) {
	store := NewSessionStore(logr.Discard())
	interceptor := NewAuthInterceptor(store, logr.Discard())

	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: extpb.ExtensionService_Resolve_FullMethodName,
	}

	_, err := interceptor.UnaryInterceptor(context.Background(), nil, info, handler)
	if err == nil {
		t.Fatal("expected error")
	}
	if code := grpcstatus.Code(err); code != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", code)
	}
}

func TestAuthInterceptor_RejectInvalidToken(t *testing.T) {
	store := NewSessionStore(logr.Discard())
	interceptor := NewAuthInterceptor(store, logr.Discard())

	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: extpb.ExtensionService_Resolve_FullMethodName,
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(SessionMetadataKey, "invalid-token"))
	_, err := interceptor.UnaryInterceptor(ctx, nil, info, handler)
	if err == nil {
		t.Fatal("expected error")
	}
	if code := grpcstatus.Code(err); code != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", code)
	}
}

func TestAuthInterceptor_AcceptValidToken(t *testing.T) {
	store, token := authenticatedSessionStore()
	interceptor := NewAuthInterceptor(store, logr.Discard())

	var capturedName string
	handler := func(ctx context.Context, req any) (any, error) {
		name, ok := NameFromContext(ctx)
		if !ok {
			t.Fatal("expected extension name in context")
		}
		capturedName = name
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: extpb.ExtensionService_Resolve_FullMethodName,
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(SessionMetadataKey, token))
	resp, err := interceptor.UnaryInterceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("expected response %q, got %q", "ok", resp)
	}
	if capturedName != "test-extension" {
		t.Fatalf("expected extension name %q, got %q", "test-extension", capturedName)
	}
}

type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func TestAuthInterceptor_StreamRejectWithoutToken(t *testing.T) {
	store := NewSessionStore(logr.Discard())
	interceptor := NewAuthInterceptor(store, logr.Discard())

	handler := func(srv any, stream grpc.ServerStream) error {
		t.Fatal("handler should not be called")
		return nil
	}

	info := &grpc.StreamServerInfo{
		FullMethod: extpb.ExtensionService_Subscribe_FullMethodName,
	}

	stream := &mockServerStream{ctx: context.Background()}
	err := interceptor.StreamInterceptor(nil, stream, info, handler)
	if err == nil {
		t.Fatal("expected error")
	}
	if code := grpcstatus.Code(err); code != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", code)
	}
}

func TestAuthInterceptor_StreamAcceptValidToken(t *testing.T) {
	store, token := authenticatedSessionStore()
	interceptor := NewAuthInterceptor(store, logr.Discard())

	var capturedName string
	handler := func(srv any, stream grpc.ServerStream) error {
		name, ok := NameFromContext(stream.Context())
		if !ok {
			t.Fatal("expected extension name in stream context")
		}
		capturedName = name
		return nil
	}

	info := &grpc.StreamServerInfo{
		FullMethod: extpb.ExtensionService_Subscribe_FullMethodName,
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(SessionMetadataKey, token))
	stream := &mockServerStream{ctx: ctx}
	err := interceptor.StreamInterceptor(nil, stream, info, handler)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if capturedName != "test-extension" {
		t.Fatalf("expected extension name %q, got %q", "test-extension", capturedName)
	}
}
