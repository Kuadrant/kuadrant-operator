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

	"github.com/samber/lo"

	"github.com/go-logr/logr/funcr"
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

func TestOOPExtensionForwardsLog(t *testing.T) {
	var messages []string

	logger := funcr.New(func(_, args string) {
		messages = append(messages, args)
	}, funcr.Options{})

	oopErrorLog := OOPExtension{
		name:       "testErrorLog",
		executable: "/bin/ps",
		socket:     "--foobar",
		service:    newExtensionService(),
		logger:     logger,
	}

	if err := oopErrorLog.Start(); err != nil {
		t.Errorf("Should have started: %v", err)
	}

	for oopErrorLog.cmd.ProcessState == nil {
		time.Sleep(5 * time.Millisecond) // wait for the command to return
	}

	logAsString := strings.Join(messages, "\n")
	assert.Assert(t, strings.Contains(strings.ToLower(logAsString), "is not a valid log level range 0-1. log:"))

	_ = oopErrorLog.Stop() // gracefully kill the process/server
	assert.Assert(t, lo.Contains(messages, "\"msg\"=\"Extension \\\"testErrorLog\\\" finished with an error\" \"error\"=\"exit status 1\""))
}

func TestOOPExtensionParseStderr(t *testing.T) {
	lvl, text, err := parseStderr(append([]byte{0}, []byte("Info")...))
	assert.Equal(t, lvl, 0)
	assert.Equal(t, text, "Info")
	assert.Equal(t, err, nil)

	lvl, text, err = parseStderr(append([]byte{1}, []byte("Error")...))
	assert.Equal(t, lvl, 1)
	assert.Equal(t, text, "Error")
	assert.Equal(t, err, nil)

	lvl, text, err = parseStderr(append([]byte{5}, []byte("not valid log level")...))
	assert.Equal(t, lvl, 1)
	assert.Equal(t, text, "")
	assert.Error(t, err, "first byte is not a valid log level range 0-1. log: \"\\x05not valid log level\"")

	lvl, text, err = parseStderr([]byte{})
	assert.Equal(t, lvl, 1)
	assert.Equal(t, text, "")
	assert.Error(t, err, "input byte slice is empty")
}
