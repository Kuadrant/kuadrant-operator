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

package log

import (
	"github.com/go-logr/logr"
)

// teeLogSink implements logr.LogSink and forwards to multiple sinks.
// This allows logging to multiple destinations simultaneously (e.g., Zap console + OTel remote).
//
// Each sink maintains its own filtering logic:
// - Zap sink: respects LOG_LEVEL and LOG_MODE for console output
// - OTel sink: exports all logs to remote collector
type teeLogSink struct {
	sinks []logr.LogSink
}

// NewTeeLogger creates a logger that writes to multiple loggers simultaneously.
// The first logger (typically Zap) controls console output with LOG_LEVEL/LOG_MODE.
// Additional loggers (e.g., OTel) receive all logs for remote export.
//
// Example:
//
//	zapLogger := log.NewLogger(log.SetLevel(log.InfoLevel), log.SetMode(log.ModeDev))
//	otelLogger, _ := log.SetupOTelLogging(ctx, config)
//	combinedLogger := log.NewTeeLogger(zapLogger, otelLogger)
//	log.SetLogger(combinedLogger)
func NewTeeLogger(loggers ...logr.Logger) logr.Logger {
	sinks := make([]logr.LogSink, len(loggers))
	for i, logger := range loggers {
		sinks[i] = logger.GetSink()
	}
	return logr.New(&teeLogSink{sinks: sinks})
}

// Init initializes all underlying sinks with runtime info.
func (t *teeLogSink) Init(info logr.RuntimeInfo) {
	for _, sink := range t.sinks {
		sink.Init(info)
	}
}

// Enabled returns true if ANY sink is enabled at this level.
// This ensures logs flow to all sinks that want them.
func (t *teeLogSink) Enabled(level int) bool {
	for _, sink := range t.sinks {
		if sink.Enabled(level) {
			return true
		}
	}
	return false
}

// Info logs an info message to all enabled sinks.
// Each sink applies its own level filtering.
func (t *teeLogSink) Info(level int, msg string, keysAndValues ...interface{}) {
	for _, sink := range t.sinks {
		if sink.Enabled(level) {
			sink.Info(level, msg, keysAndValues...)
		}
	}
}

// Error logs an error message to all sinks.
// Errors are always logged regardless of level.
func (t *teeLogSink) Error(err error, msg string, keysAndValues ...interface{}) {
	for _, sink := range t.sinks {
		sink.Error(err, msg, keysAndValues...)
	}
}

// WithValues returns a new tee sink with additional key-value pairs.
// The key-values are propagated to all underlying sinks.
func (t *teeLogSink) WithValues(keysAndValues ...interface{}) logr.LogSink {
	newSinks := make([]logr.LogSink, len(t.sinks))
	for i, sink := range t.sinks {
		newSinks[i] = sink.WithValues(keysAndValues...)
	}
	return &teeLogSink{sinks: newSinks}
}

// WithName returns a new tee sink with an additional name element.
// The name is propagated to all underlying sinks.
func (t *teeLogSink) WithName(name string) logr.LogSink {
	newSinks := make([]logr.LogSink, len(t.sinks))
	for i, sink := range t.sinks {
		newSinks[i] = sink.WithName(name)
	}
	return &teeLogSink{sinks: newSinks}
}
