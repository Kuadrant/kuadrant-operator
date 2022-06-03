/*
Copyright 2021 Red Hat, Inc.

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
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	// Log is the base logger
	Log logr.Logger = logr.New(ctrllog.NullLogSink{})
)

// Level configures the verbosity of the logging.
type Level zapcore.Level

const (
	// DebugLevel logs are typically voluminous, and are usually disabled in production.
	DebugLevel = Level(zapcore.DebugLevel)
	// InfoLevel is the default logging priority.
	InfoLevel = Level(zapcore.InfoLevel)
	// WarnLevel logs are more important than Info, but don't need individual human review.
	WarnLevel = Level(zapcore.WarnLevel)
	// ErrorLevel logs are high-priority. If an application is running smoothly,
	// it shouldn't generate any error-level logs.
	ErrorLevel = Level(zapcore.ErrorLevel)
	// DPanicLevel logs are particularly important errors. In development the
	// logger panics after writing the message.
	DPanicLevel = Level(zapcore.DPanicLevel)
	// PanicLevel logs a message, then panics.
	PanicLevel = Level(zapcore.PanicLevel)
	// FatalLevel logs a message, then calls os.Exit(1).
	FatalLevel = Level(zapcore.FatalLevel)
)

// ToLevel converts a string to a log level.
func ToLevel(level string) Level {
	var l zapcore.Level
	err := l.UnmarshalText([]byte(level))
	if err != nil {
		panic(err)
	}
	return Level(l)
}

// Mode defines the log output mode.
type Mode int8

const (
	// ModeProd is the log mode for production.
	ModeProd Mode = iota
	// ModeDev is for more human-readable outputs, extra stack traces
	// and logging info. (aka Zap's "development config".)
	// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/log/zap#UseDevMode
	ModeDev
)

// ToMode converts a string to a log mode.
// Use either 'production' for `LogModeProd` or 'development' for `LogModeDev`.
func ToMode(mode string) Mode {
	switch strings.ToLower(mode) {
	case "production":
		return ModeProd
	case "development":
		return ModeDev
	default:
		panic(fmt.Sprintf("unknown log mode: %s", mode))
	}
}

// Opts allows to manipulate Options.
type Opts func(*Options)

// Options contains all possible settings.
type Options struct {
	// LogLevel configures the verbosity of the logging.
	LogLevel Level
	// LogMode defines the log output mode.
	LogMode Mode
	// DestWriter controls the destination of the log output.  Defaults to
	// os.Stderr.
	DestWriter io.Writer
}

// SetLogger sets a concrete logging implementation for all deferred Loggers.
// Being top level application and not a library or dependency,
// let's delegation logger used by any dependency
func SetLogger(logger logr.Logger) {
	Log = logger

	ctrl.SetLogger(logger) // fulfills `logger` as the de facto logger used by controller-runtime
	klog.SetLogger(logger)
}

// WriteTo configures the logger to write to the given io.Writer, instead of standard error.
// See Options.DestWriter.
func WriteTo(out io.Writer) Opts {
	return func(o *Options) {
		o.DestWriter = out
	}
}

// SetLevel sets Options.LogLevel, which configures the the minimum enabled logging level e.g Debug, Info.
func SetLevel(level Level) func(o *Options) {
	return func(o *Options) {
		o.LogLevel = level
	}
}

// SetMode sets Options.LogMode, which configures to use (or not use) development mode
func SetMode(mode Mode) func(o *Options) {
	return func(o *Options) {
		o.LogMode = mode
	}
}

// NewLogger creates new Logger based on controller runtime zap logger
func NewLogger(opts ...Opts) logr.Logger {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	// defaults
	if o.DestWriter == nil {
		o.DestWriter = os.Stderr
	}

	return zap.New(
		zap.Level(zapcore.Level(o.LogLevel)),
		zap.UseDevMode(o.LogMode == ModeDev),
		zap.WriteTo(o.DestWriter),
	)
}
