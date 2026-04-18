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
	"google.golang.org/protobuf/types/descriptorpb"
)

type ProtoCacheKey struct {
	ClusterName string
	Service     string
}

type ProtoCache struct {
	cache map[ProtoCacheKey]*descriptorpb.FileDescriptorSet
}

func NewProtoCache() *ProtoCache {
	return &ProtoCache{
		cache: make(map[ProtoCacheKey]*descriptorpb.FileDescriptorSet),
	}
}

func (pc *ProtoCache) Set(key ProtoCacheKey, fds *descriptorpb.FileDescriptorSet) {
	pc.cache[key] = fds
}

func (pc *ProtoCache) Get(key ProtoCacheKey) (*descriptorpb.FileDescriptorSet, bool) {
	fds, exists := pc.cache[key]
	return fds, exists
}

func (pc *ProtoCache) Delete(key ProtoCacheKey) bool {
	_, existed := pc.cache[key]
	if existed {
		delete(pc.cache, key)
	}
	return existed
}
