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
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr/funcr"
	"gotest.tools/assert"

	"github.com/go-logr/logr"
)

func TestOOPExtensionManagesExternalProcess(t *testing.T) {
	oop := OOPExtension{
		name:       "test",
		executable: "/bin/sleep",
		socket:     "1d",
		service:    newExtensionService(nil, logr.Discard()),
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
	mu       sync.Mutex
	messages []string
}

func newWriterMock() *writerMock {
	return &writerMock{}
}

func (w *writerMock) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	message := string(p)
	w.messages = append(w.messages, message)
	return len(p), nil
}

func (w *writerMock) getMessages() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]string, len(w.messages))
	copy(result, w.messages)
	return result
}

func TestOOPExtensionForwardsLog(t *testing.T) {
	writer := newWriterMock()

	logger := funcr.New(func(_, args string) {
		writer.Write([]byte(args))
	}, funcr.Options{})

	socketPath := fmt.Sprintf("/tmp/kuadrant-test-oop-%d.sock", os.Getpid())
	defer os.Remove(socketPath)

	oopErrorLog := OOPExtension{
		name:       "testErrorLog",
		executable: "/bin/ps",
		socket:     socketPath,
		service:    newExtensionService(nil, logger),
		logger:     logger,
		sync:       writer,
	}

	if err := oopErrorLog.Start(); err != nil {
		t.Fatalf("Should have started: %v", err)
	}

	// Wait for the process to finish
	oopErrorLog.Wait()

	_ = oopErrorLog.Stop()

	messages := writer.getMessages()
	logAsString := strings.Join(messages, "\n")

	hasStderrOutput := strings.Contains(strings.ToLower(logAsString), "usage:") ||
		strings.Contains(strings.ToLower(logAsString), "illegal option")

	hasErrorMessage := strings.Contains(logAsString, "Extension") &&
		strings.Contains(logAsString, "finished with an error")

	assert.Assert(t, hasErrorMessage, "Expected process error completion message")
	assert.Assert(t, hasStderrOutput, "Expected ps stderr output to be captured")
}
