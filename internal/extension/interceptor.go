package extension

import (
	"context"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

const SessionMetadataKey = "x-kuadrant-session"

type contextKey struct{}

var extensionNameKey = contextKey{}

func NameFromContext(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(extensionNameKey).(string)
	return name, ok
}

type AuthInterceptor struct {
	sessionStore *SessionStore
	logger       logr.Logger
}

func NewAuthInterceptor(sessionStore *SessionStore, logger logr.Logger) *AuthInterceptor {
	return &AuthInterceptor{
		sessionStore: sessionStore,
		logger:       logger,
	}
}

func (a *AuthInterceptor) UnaryInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	if info.FullMethod == extpb.ExtensionService_Handshake_FullMethodName {
		return handler(ctx, req)
	}

	name, err := a.authenticate(ctx)
	if err != nil {
		return nil, err
	}

	ctx = context.WithValue(ctx, extensionNameKey, name)
	return handler(ctx, req)
}

func (a *AuthInterceptor) StreamInterceptor(
	srv any,
	ss grpc.ServerStream,
	_ *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	name, err := a.authenticate(ss.Context())
	if err != nil {
		return err
	}

	wrapped := &authenticatedServerStream{
		ServerStream: ss,
		ctx:          context.WithValue(ss.Context(), extensionNameKey, name),
	}
	return handler(srv, wrapped)
}

func (a *AuthInterceptor) authenticate(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}

	tokens := md.Get(SessionMetadataKey)
	if len(tokens) == 0 {
		return "", status.Error(codes.Unauthenticated, "missing session token")
	}

	name, valid := a.sessionStore.ValidateSession(tokens[0])
	if !valid {
		return "", status.Error(codes.Unauthenticated, "invalid session token")
	}

	return name, nil
}

type authenticatedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedServerStream) Context() context.Context {
	return s.ctx
}
