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
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/samber/lo"
	"gotest.tools/assert"

	"github.com/go-logr/logr"
)

func TestOOPExtensionManagesExternalProcess(t *testing.T) {
	oop := OOPExtension{
		name:       "test",
		executable: "/bin/sleep",
		socket:     "1d",
		service:    newExtensionService(),
		logger:     logr.Discard(),
		sync:       nil,
	}

	if oop.IsAlive() {
		t.Errorf("Must not be alive")
	}
	if err := oop.Start(); err != nil {
		t.Errorf("Should have started: %v", err)
	}
	if !oop.IsAlive() {
		t.Errorf("Must be alive")
	}
	if err := oop.Stop(); err != nil {
		t.Errorf("Should have stopped: %v", err)
	}
	if oop.IsAlive() {
		t.Errorf("Must not be alive")
	}
}

type writerMock struct {
	messages []string
}

func (w *writerMock) Write(p []byte) (n int, err error) {
	w.messages = append(w.messages, string(p))
	return len(p), nil
}

func TestOOPExtensionForwardsLog(t *testing.T) {
	writer := &writerMock{}

	logger := funcr.New(func(_, args string) {
		writer.Write([]byte(args))
	}, funcr.Options{})

	oopErrorLog := OOPExtension{
		name:       "testErrorLog",
		executable: "/bin/ps",
		socket:     "--foobar",
		service:    newExtensionService(),
		logger:     logger,
		sync:       writer,
	}

	if err := oopErrorLog.Start(); err != nil {
		t.Errorf("Should have started: %v", err)
	}

	for oopErrorLog.cmd.ProcessState == nil {
		time.Sleep(5 * time.Millisecond) // wait for the command to return
	}

	_ = oopErrorLog.Stop() // gracefully kill the process/server
	assert.Assert(t, lo.Contains(writer.messages, "\"msg\"=\"Extension \\\"testErrorLog\\\" finished with an error\" \"error\"=\"exit status 1\""))
	logAsString := strings.Join(writer.messages, "\n")
	assert.Assert(t, strings.Contains(strings.ToLower(logAsString), "usage:"))
}
