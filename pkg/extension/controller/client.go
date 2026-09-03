package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

const sessionMetadataKey = "x-kuadrant-session"

// sessionCredentials carries the session token as per-RPC metadata. The token
// is read on every RPC and rewritten across re-handshakes, so access is guarded.
type sessionCredentials struct {
	mu    sync.RWMutex
	token string
}

func (c *sessionCredentials) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	token := c.getToken()
	if token == "" {
		return nil, nil
	}
	return map[string]string{sessionMetadataKey: token}, nil
}

func (c *sessionCredentials) RequireTransportSecurity() bool {
	return false
}

func (c *sessionCredentials) setToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *sessionCredentials) getToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// extensionClient wraps the gRPC client connection to the operator's extension
// service and exposes a subset of RPCs used by the controller layer.
type extensionClient struct {
	conn    *grpc.ClientConn
	client  extpb.ExtensionServiceClient
	session *sessionCredentials
}

// newExtensionClient dials the operator's extension service at the given TCP
// address and returns a ready extensionClient.
func newExtensionClient(address string) (*extensionClient, error) {
	session := &sessionCredentials{}

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(session),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	)
	if err != nil {
		return nil, err
	}

	return &extensionClient{
		conn:    conn,
		client:  extpb.NewExtensionServiceClient(conn),
		session: session,
	}, nil
}

func (ec *extensionClient) handshake(ctx context.Context, token []byte, policyKind string) error {
	resp, err := ec.client.Handshake(ctx, &extpb.HandshakeRequest{
		Token:      token,
		PolicyKind: policyKind,
	})
	if err != nil {
		return fmt.Errorf("handshake RPC failed: %w", err)
	}
	if !resp.Accepted {
		return fmt.Errorf("handshake rejected: %s", resp.Reason)
	}
	ec.session.setToken(resp.SessionToken)
	return nil
}

func (ec *extensionClient) ping(ctx context.Context) (*extpb.PongResponse, error) {
	return ec.client.Ping(ctx, &extpb.PingRequest{
		Out: timestamppb.New(time.Now()),
	})
}

func (ec *extensionClient) releaseSession(ctx context.Context) error {
	_, err := ec.client.ReleaseSession(ctx, &emptypb.Empty{})
	return err
}

// subscribe opens a streaming RPC for the given policy kind. Responses are
// forwarded to the provided callback until the stream ends or an error
// occurs.
func (ec *extensionClient) subscribe(ctx context.Context, policyKind string, callback func(response *extpb.SubscribeResponse)) error {
	stream, err := ec.client.Subscribe(ctx, &extpb.SubscribeRequest{
		PolicyKind: policyKind,
	})
	if err != nil {
		return err
	}
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		callback(response)
	}
	return nil
}

func (ec *extensionClient) close() error {
	if ec.conn == nil {
		return nil
	}
	return ec.conn.Close()
}
