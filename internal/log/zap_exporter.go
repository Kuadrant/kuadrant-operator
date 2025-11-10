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
	"context"
	"io"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// zapExporter is an OpenTelemetry log exporter that writes to a zap logger.
// This allows OTel logs to be formatted and output using zap's console formatter,
// while still sending raw logs to remote collectors via OTLP.
type zapExporter struct {
	logger *zap.Logger
	level  zapcore.Level
}

// NewZapExporter creates a new OTel log exporter that writes to a zap logger.
// The exporter respects the configured log level and mode for console output.
func newZapExporter(level Level, mode Mode, writer io.Writer) *zapExporter {
	// Create encoder config based on mode
	var encoderConfig zapcore.EncoderConfig
	if mode == ModeDev {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
	} else {
		encoderConfig = zap.NewProductionEncoderConfig()
	}

	// Create core with console encoder
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(writer),
		zapcore.Level(level),
	)

	return &zapExporter{
		logger: zap.New(core),
		level:  zapcore.Level(level),
	}
}

// Export converts OTel log records to zap log calls.
// It maps OTel severity levels to zap levels and extracts all attributes as zap fields.
func (e *zapExporter) Export(_ context.Context, records []sdklog.Record) error {
	for i := range records {
		record := &records[i]

		// Convert OTel severity to zap level
		zapLevel := e.otelSeverityToZapLevel(record.Severity())

		// Skip if this level is not enabled
		if !e.logger.Core().Enabled(zapLevel) {
			continue
		}

		// Extract fields from the record
		fields := e.recordToZapFields(record)

		// Log the message at the appropriate level
		switch zapLevel {
		case zapcore.DebugLevel:
			e.logger.Debug(record.Body().AsString(), fields...)
		case zapcore.InfoLevel:
			e.logger.Info(record.Body().AsString(), fields...)
		case zapcore.WarnLevel:
			e.logger.Warn(record.Body().AsString(), fields...)
		case zapcore.ErrorLevel:
			e.logger.Error(record.Body().AsString(), fields...)
		case zapcore.DPanicLevel:
			e.logger.DPanic(record.Body().AsString(), fields...)
		case zapcore.PanicLevel:
			e.logger.Panic(record.Body().AsString(), fields...)
		case zapcore.FatalLevel:
			e.logger.Fatal(record.Body().AsString(), fields...)
		}
	}
	return nil
}

// Shutdown flushes any buffered logs.
func (e *zapExporter) Shutdown(_ context.Context) error {
	return e.logger.Sync()
}

// ForceFlush flushes any buffered logs.
func (e *zapExporter) ForceFlush(_ context.Context) error {
	return e.logger.Sync()
}

// otelSeverityToZapLevel maps OpenTelemetry severity levels to zap levels.
func (e *zapExporter) otelSeverityToZapLevel(severity log.Severity) zapcore.Level {
	// OTel severity mapping based on https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-severitynumber
	switch {
	case severity <= log.SeverityTrace4: // 1-4: Trace
		return zapcore.DebugLevel
	case severity <= log.SeverityDebug4: // 5-8: Debug
		return zapcore.DebugLevel
	case severity <= log.SeverityInfo4: // 9-12: Info
		return zapcore.InfoLevel
	case severity <= log.SeverityWarn4: // 13-16: Warn
		return zapcore.WarnLevel
	case severity <= log.SeverityError4: // 17-20: Error
		return zapcore.ErrorLevel
	default: // 21-24: Fatal
		return zapcore.FatalLevel
	}
}

// recordToZapFields converts OTel record attributes to zap fields.
func (e *zapExporter) recordToZapFields(record *sdklog.Record) []zap.Field {
	var fields []zap.Field

	// Add timestamp if present
	if !record.Timestamp().IsZero() {
		fields = append(fields, zap.Time("timestamp", record.Timestamp()))
	}

	// Add trace context if present
	if record.TraceID().IsValid() {
		fields = append(fields, zap.String("trace_id", record.TraceID().String()))
	}
	if record.SpanID().IsValid() {
		fields = append(fields, zap.String("span_id", record.SpanID().String()))
	}

	// Extract all attributes
	record.WalkAttributes(func(kv log.KeyValue) bool {
		fields = append(fields, e.otelValueToZapField(kv.Key, kv.Value))
		return true
	})

	return fields
}

// otelValueToZapField converts an OTel key-value pair to a zap field.
func (e *zapExporter) otelValueToZapField(key string, value log.Value) zap.Field {
	switch value.Kind() {
	case log.KindBool:
		return zap.Bool(key, value.AsBool())
	case log.KindFloat64:
		return zap.Float64(key, value.AsFloat64())
	case log.KindInt64:
		return zap.Int64(key, value.AsInt64())
	case log.KindString:
		return zap.String(key, value.AsString())
	case log.KindBytes:
		return zap.Binary(key, value.AsBytes())
	case log.KindSlice:
		// Convert slice to []interface{} for zap
		slice := value.AsSlice()
		values := make([]interface{}, len(slice))
		for i, v := range slice {
			values[i] = e.otelValueToInterface(v)
		}
		return zap.Any(key, values)
	case log.KindMap:
		// Convert map to map[string]interface{} for zap
		kvs := value.AsMap()
		m := make(map[string]interface{}, len(kvs))
		for _, kv := range kvs {
			m[kv.Key] = e.otelValueToInterface(kv.Value)
		}
		return zap.Any(key, m)
	default:
		return zap.Any(key, value.AsString())
	}
}

// otelValueToInterface converts an OTel value to a Go interface{}.
func (e *zapExporter) otelValueToInterface(value log.Value) interface{} {
	switch value.Kind() {
	case log.KindBool:
		return value.AsBool()
	case log.KindFloat64:
		return value.AsFloat64()
	case log.KindInt64:
		return value.AsInt64()
	case log.KindString:
		return value.AsString()
	case log.KindBytes:
		return value.AsBytes()
	case log.KindSlice:
		slice := value.AsSlice()
		values := make([]interface{}, len(slice))
		for i, v := range slice {
			values[i] = e.otelValueToInterface(v)
		}
		return values
	case log.KindMap:
		kvs := value.AsMap()
		m := make(map[string]interface{}, len(kvs))
		for _, kv := range kvs {
			m[kv.Key] = e.otelValueToInterface(kv.Value)
		}
		return m
	default:
		return value.AsString()
	}
}
