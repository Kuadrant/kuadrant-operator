package controller

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

// extensionClient wraps the gRPC client connection to the core extension
// service over a unix domain socket and exposes a subset of RPCs used by the
// controller layer.
type extensionClient struct {
	conn   *grpc.ClientConn
	client extpb.ExtensionServiceClient
}

// newExtensionClient dials the unix domain socket at socketPath and returns a
// ready extensionClient.
func newExtensionClient(socketPath string) (*extensionClient, error) {
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}

	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	return &extensionClient{
		conn:   conn,
		client: extpb.NewExtensionServiceClient(conn),
	}, nil
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
