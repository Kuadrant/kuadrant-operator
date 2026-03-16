/*
Copyright 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package extension

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const reflectionTimeout = 30 * time.Second

type ReflectionClient struct {
	timeout time.Duration
}

func NewReflectionClient() *ReflectionClient {
	return &ReflectionClient{
		timeout: reflectionTimeout,
	}
}

func (rc *ReflectionClient) WithTimeout(timeout time.Duration) *ReflectionClient {
	rc.timeout = timeout
	return rc
}

func (rc *ReflectionClient) FetchServiceDescriptors(ctx context.Context, url, serviceName string) (*descriptorpb.FileDescriptorSet, error) {
	ctx, cancel := context.WithTimeout(ctx, rc.timeout)
	defer cancel()

	conn, err := grpc.NewClient(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", url, err)
	}
	defer conn.Close()

	refClient := rpb.NewServerReflectionClient(conn)
	stream, err := refClient.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("reflection not supported: %w", err)
	}
	defer stream.CloseSend()

	visited := make(map[string]bool)
	var allFiles []*descriptorpb.FileDescriptorProto

	err = rc.fetchFileContainingSymbol(stream, serviceName, visited, &allFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch service %s: %w", serviceName, err)
	}

	return &descriptorpb.FileDescriptorSet{File: allFiles}, nil
}

func (rc *ReflectionClient) fetchFileContainingSymbol(
	stream rpb.ServerReflection_ServerReflectionInfoClient,
	symbol string,
	visited map[string]bool,
	allFiles *[]*descriptorpb.FileDescriptorProto,
) error {
	err := stream.Send(&rpb.ServerReflectionRequest{
		MessageRequest: &rpb.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: symbol,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	return rc.processReflectionResponse(stream, visited, allFiles)
}

func (rc *ReflectionClient) fetchFileByName(
	stream rpb.ServerReflection_ServerReflectionInfoClient,
	fileName string,
	visited map[string]bool,
	allFiles *[]*descriptorpb.FileDescriptorProto,
) error {
	if visited[fileName] {
		return nil
	}

	err := stream.Send(&rpb.ServerReflectionRequest{
		MessageRequest: &rpb.ServerReflectionRequest_FileByFilename{
			FileByFilename: fileName,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	return rc.processReflectionResponse(stream, visited, allFiles)
}

func (rc *ReflectionClient) processReflectionResponse(
	stream rpb.ServerReflection_ServerReflectionInfoClient,
	visited map[string]bool,
	allFiles *[]*descriptorpb.FileDescriptorProto,
) error {
	resp, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive response: %w", err)
	}
	if errResp := resp.GetErrorResponse(); errResp != nil {
		return fmt.Errorf("reflection error: %s", errResp.GetErrorMessage())
	}
	fileDescResp := resp.GetFileDescriptorResponse()
	if fileDescResp == nil {
		return fmt.Errorf("unexpected response type")
	}

	for _, fdBytes := range fileDescResp.FileDescriptorProto {
		fd := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(fdBytes, fd); err != nil {
			return fmt.Errorf("failed to unmarshal file descriptor: %w", err)
		}
		fileName := fd.GetName()
		if visited[fileName] {
			continue
		}
		visited[fileName] = true
		*allFiles = append(*allFiles, fd)

		for _, dep := range fd.Dependency {
			if !visited[dep] {
				if err := rc.fetchFileByName(stream, dep, visited, allFiles); err != nil {
					return fmt.Errorf("failed to fetch dependency %s: %w", dep, err)
				}
			}
		}
	}

	return nil
}
