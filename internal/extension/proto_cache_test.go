//go:build unit

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
	"sync"
	"testing"

	"google.golang.org/protobuf/types/descriptorpb"
)

func TestProtoCache_SetAndGet(t *testing.T) {
	cache := NewProtoCache()

	key := ProtoCacheKey{
		ClusterName: "test-cluster",
		Service:     "test.Service",
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: strPtr("test.proto")},
		},
	}

	cache.Set(key, fds)

	retrieved, exists := cache.Get(key)
	if !exists {
		t.Fatal("Expected descriptor to exist in cache")
	}

	if retrieved != fds {
		t.Error("Retrieved descriptor does not match stored descriptor")
	}
}

func TestProtoCache_GetNonExistent(t *testing.T) {
	cache := NewProtoCache()

	key := ProtoCacheKey{
		ClusterName: "nonexistent",
		Service:     "nonexistent.Service",
	}

	_, exists := cache.Get(key)
	if exists {
		t.Error("Expected descriptor not to exist in cache")
	}
}

func TestProtoCache_Delete(t *testing.T) {
	cache := NewProtoCache()

	key := ProtoCacheKey{
		ClusterName: "test-cluster",
		Service:     "test.Service",
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: strPtr("test.proto")},
		},
	}

	cache.Set(key, fds)

	deleted := cache.Delete(key)
	if !deleted {
		t.Error("Expected Delete to return true for existing entry")
	}

	_, exists := cache.Get(key)
	if exists {
		t.Error("Expected descriptor to be removed from cache")
	}
}

func TestProtoCache_DeleteNonExistent(t *testing.T) {
	cache := NewProtoCache()

	key := ProtoCacheKey{
		ClusterName: "nonexistent",
		Service:     "nonexistent.Service",
	}

	deleted := cache.Delete(key)
	if deleted {
		t.Error("Expected Delete to return false for non-existent entry")
	}
}

func TestProtoCache_MultipleKeys(t *testing.T) {
	cache := NewProtoCache()

	key1 := ProtoCacheKey{ClusterName: "cluster1", Service: "service1"}
	key2 := ProtoCacheKey{ClusterName: "cluster2", Service: "service2"}

	fds1 := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: strPtr("first.proto")},
		},
	}

	fds2 := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: strPtr("second.proto")},
		},
	}

	cache.Set(key1, fds1)
	cache.Set(key2, fds2)

	retrieved1, exists1 := cache.Get(key1)
	if !exists1 || retrieved1 != fds1 {
		t.Error("Expected first descriptor to exist")
	}

	retrieved2, exists2 := cache.Get(key2)
	if !exists2 || retrieved2 != fds2 {
		t.Error("Expected second descriptor to exist")
	}

	cache.Delete(key1)

	_, exists1AfterDelete := cache.Get(key1)
	if exists1AfterDelete {
		t.Error("Expected first descriptor to be deleted")
	}

	retrieved2AfterDelete, exists2AfterDelete := cache.Get(key2)
	if !exists2AfterDelete || retrieved2AfterDelete != fds2 {
		t.Error("Expected second descriptor to still exist")
	}
}

func TestProtoCache_ConcurrentAccess(t *testing.T) {
	cache := NewProtoCache()
	var wg sync.WaitGroup

	numGoroutines := 100
	numOperations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				key := ProtoCacheKey{
					ClusterName: "cluster",
					Service:     "service",
				}

				fds := &descriptorpb.FileDescriptorSet{
					File: []*descriptorpb.FileDescriptorProto{
						{Name: strPtr("test.proto")},
					},
				}

				cache.Set(key, fds)
				cache.Get(key)

				if j%10 == 0 {
					cache.Delete(key)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestProtoCache_OverwriteExisting(t *testing.T) {
	cache := NewProtoCache()

	key := ProtoCacheKey{
		ClusterName: "test-cluster",
		Service:     "test.Service",
	}

	fds1 := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: strPtr("first.proto")},
		},
	}

	fds2 := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: strPtr("second.proto")},
		},
	}

	cache.Set(key, fds1)
	cache.Set(key, fds2)

	retrieved, exists := cache.Get(key)
	if !exists {
		t.Fatal("Expected descriptor to exist in cache")
	}

	if retrieved != fds2 {
		t.Error("Expected second descriptor to overwrite first")
	}
}

func strPtr(s string) *string {
	return &s
}
