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
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"gotest.tools/assert"
)

func TestOOPExtensionManagesExternalProcess(t *testing.T) {
	oop := OOPExtension{
		name:       "test",
		executable: "/bin/cat",
		logger:     logr.Discard(),
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

func TestOOPExtensionForwardsStderr(t *testing.T) {
	writer := newWriterMock()

	oop := OOPExtension{
		name:   "test",
		logger: logr.Discard(),
		sync:   writer,
	}

	pr, pw := io.Pipe()
	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		oop.monitorStderr(pr, ready)
		close(done)
	}()
	<-ready

	_, err := pw.Write([]byte("something went wrong\n"))
	assert.NilError(t, err)
	pw.Close()

	<-done

	messages := writer.getMessages()
	logAsString := strings.Join(messages, "\n")
	assert.Assert(t, strings.Contains(logAsString, "something went wrong"), "Expected stderr output to be forwarded")
}
