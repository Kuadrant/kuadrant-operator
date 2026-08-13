package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

const sessionMetadataKey = "x-kuadrant-session"

type sessionCredentials struct {
	token string
}

func (c *sessionCredentials) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	if c.token == "" {
		return nil, nil
	}
	return map[string]string{sessionMetadataKey: c.token}, nil
}

func (c *sessionCredentials) RequireTransportSecurity() bool {
	return false
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

func (ec *extensionClient) handshake(ctx context.Context, name string, credential []byte, policyKind string) error {
	resp, err := ec.client.Handshake(ctx, &extpb.HandshakeRequest{
		Name:       name,
		Credential: credential,
		PolicyKind: policyKind,
	})
	if err != nil {
		return fmt.Errorf("handshake RPC failed: %w", err)
	}
	if !resp.Accepted {
		return fmt.Errorf("handshake rejected: %s", resp.Reason)
	}
	ec.session.token = resp.SessionToken
	return nil
}

//lint:ignore U1000
func (ec *extensionClient) ping(ctx context.Context) (*extpb.PongResponse, error) {
	return ec.client.Ping(ctx, &extpb.PingRequest{
		Out: timestamppb.New(time.Now()),
	})
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

//lint:ignore U1000
func (ec *extensionClient) close() error {
	return ec.conn.Close()
}
