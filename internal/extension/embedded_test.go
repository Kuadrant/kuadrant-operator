/*
Copyright 2025 Red Hat, Inc.

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
	"testing"

	"github.com/go-logr/logr"
)

func TestEmbeddedPluginManagesExternalProcess(t *testing.T) {
	plugin := EmbeddedPlugin{
		name:       "test",
		executable: "/bin/sleep",
		socket:     "1d",
		service:    newHeartBeatService(),
		logger:     logr.Discard(),
	}

	if plugin.IsAlive() {
		t.Errorf("Must not be alive")
	}
	if err := plugin.Start(); err != nil {
		t.Errorf("Should have started: %v", err)
	}
	if !plugin.IsAlive() {
		t.Errorf("Must be alive")
	}
	if err := plugin.Stop(); err != nil {
		t.Errorf("Should have stopped: %v", err)
	}
	if plugin.IsAlive() {
		t.Errorf("Must not be alive")
	}
}
