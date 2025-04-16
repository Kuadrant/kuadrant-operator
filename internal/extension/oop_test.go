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

	_ = oopErrorLog.Stop() // gracefully kill the process/server
	assert.Assert(t, lo.Contains(messages, "\"msg\"=\"Extension \\\"testErrorLog\\\" finished with an error\" \"error\"=\"exit status 1\""))
}

func TestOOPExtensionUnmarshalLogEntry(t *testing.T) {
	jsonString := "{\"level\":\"info\",\"ts\":\"2025-04-16T13:16:07Z\",\"logger\":\"test-extension\",\"msg\":\"Starting workers\",\"controller\":\"example-extension-controller\",\"worker count\":1}"
	jsonUnmarshaled, err := unmarshalLogEntry([]byte(jsonString))

	assert.NilError(t, err)
	assert.Equal(t, jsonUnmarshaled.Level, logLevel("info"))
	assert.Equal(t, jsonUnmarshaled.Timestamp, "2025-04-16T13:16:07Z")
	assert.Equal(t, jsonUnmarshaled.Msg, "Starting workers")
	assert.Equal(t, len(jsonUnmarshaled.KeysAndValues), 3)
	assert.Equal(t, jsonUnmarshaled.KeysAndValues["logger"], "test-extension")
	assert.Equal(t, jsonUnmarshaled.KeysAndValues["worker count"], float64(1))
	assert.Equal(t, jsonUnmarshaled.KeysAndValues["controller"], "example-extension-controller")
}

func TestOOPExtensionUnmarshalWrongFormat(t *testing.T) {
	jsonString := "Not JSON"
	jsonUnmarshaled, err := unmarshalLogEntry([]byte(jsonString))

	assert.Error(t, err, "failed to unmarshal JSON: invalid character 'N' looking for beginning of value")
	assert.Assert(t, jsonUnmarshaled == nil)
}

func TestOOPExtensionLogStderr(t *testing.T) {
	var messages []string

	logger := funcr.New(func(_, args string) {
		messages = append(messages, args)
	}, funcr.Options{})

	oop := OOPExtension{
		name:       "testLogStderr",
		executable: "some_exec",
		socket:     "socket",
		service:    newExtensionService(),
		logger:     logger,
	}

	logLineInfo := &oopLogEntry{
		Level: "info",
		Msg:   "Executing something",
	}
	logLineError := &oopLogEntry{
		Level: "error",
		Msg:   "Error executing something",
		Error: "Error executing something",
	}
	logLineExtraValues := &oopLogEntry{
		Level:         "info",
		Msg:           "Extra values executing something",
		KeysAndValues: map[string]interface{}{"controller": "test-controller"},
	}
	logLineWrongLogLevel := &oopLogEntry{
		Level: "wrong",
		Msg:   "mhmmm",
	}
	oop.logStderr(logLineInfo)
	oop.logStderr(logLineError)
	oop.logStderr(logLineExtraValues)
	oop.logStderr(logLineWrongLogLevel)

	assert.Equal(t, len(messages), 4)
	assert.Equal(t, messages[0], `"level"=0 "msg"="Executing something"`)
	assert.Equal(t, messages[1], `"msg"="Error executing something" "error"="Error executing something"`)
	assert.Equal(t, messages[2], `"level"=0 "msg"="Extra values executing something" "controller"="test-controller"`)
	assert.Equal(t, messages[3], `"msg"="mhmmm" "error"="unknown LogLevel wrong"`)
}
