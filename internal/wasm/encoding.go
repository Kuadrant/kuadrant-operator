package wasm

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sync"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/env"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// EncodingGzipBase64 identifies the wasm config wire encoding where the raw
// config JSON is gzip-compressed and then base64-encoded. The embedded wasm
// config can grow with the number of policies and the size of the Gateway API
// topology, approaching the Kubernetes custom-resource size ceiling (~1.5 MB);
// compressing it keeps the EnvoyFilter / EnvoyExtensionPolicy within limits.
const EncodingGzipBase64 = "gzip+base64"

// defaultMaxDecodedConfigSize bounds the decompressed wasm config. It defaults
// to 32 MiB, well above the Kubernetes/etcd object size limit (~1.5 MiB) so it
// never rejects a valid configuration, while still capping how much memory a
// single decode can allocate.
const defaultMaxDecodedConfigSize = 32 << 20 // 32 MiB

// maxDecodedConfigSizeEnvVar overrides defaultMaxDecodedConfigSize. Its value is a
// Kubernetes resource.Quantity, so it accepts binary/decimal suffixes as well as
// a bare byte integer, e.g. "128Mi", "64Mi", "32Mi", "60M", or "33554432".
const maxDecodedConfigSizeEnvVar = "WASM_MAX_DECODED_CONFIG_SIZE"

var (
	maxDecodedConfigSizeOnce  sync.Once
	maxDecodedConfigSizeValue int
)

// MaxDecodedConfigSize returns the maximum number of bytes decodeConfig will
// decompress, preventing a decompression bomb (gzip can expand repetitive data
// ~1000x) in an attacker-controlled EnvoyFilter/EnvoyExtensionPolicy from
// exhausting the operator's memory on reconcile.
//
// It is read once from WASM_MAX_DECODED_CONFIG_SIZE (a resource.Quantity such as
// "128Mi", "64Mi", "32Mi", or a bare byte integer) and cached; an empty, invalid,
// or non-positive value falls back to the 32 MiB default, and an invalid value is
// logged at error level. Call it once from main after the logger is configured so
// that error is reported with a real logger.
func MaxDecodedConfigSize() int {
	maxDecodedConfigSizeOnce.Do(func() {
		maxDecodedConfigSizeValue = defaultMaxDecodedConfigSize
		s := env.GetString(maxDecodedConfigSizeEnvVar, "")
		if s == "" {
			return
		}
		q, err := resource.ParseQuantity(s)
		if err != nil {
			logf.Log.WithName("wasm").Error(err, "invalid "+maxDecodedConfigSizeEnvVar+"; using default",
				"value", s, "defaultBytes", defaultMaxDecodedConfigSize)
			return
		}
		if v := q.Value(); v > 0 {
			maxDecodedConfigSizeValue = int(v)
		} else {
			logf.Log.WithName("wasm").Error(fmt.Errorf("value %q is not a positive size", s),
				"invalid "+maxDecodedConfigSizeEnvVar+"; using default", "defaultBytes", defaultMaxDecodedConfigSize)
		}
	})
	return maxDecodedConfigSizeValue
}

// encodedConfig is the self-describing envelope wrapping the encoded wasm config:
//
//	{ "encoding": "gzip+base64", "data": "<base64(gzip(configJSON))>", "maxDecompressedBytes": 33554432 }
//
// maxDecompressedBytes is read by the wasm-shim to cap decompression on the data
// plane (see Kuadrant/wasm-shim#450). The operator sets it to its own limit so
// both sides enforce the same bound; when omitted the shim defaults to 32 MiB.
type encodedConfig struct {
	Encoding             string `json:"encoding"`
	Data                 string `json:"data"`
	MaxDecompressedBytes int    `json:"maxDecompressedBytes,omitempty"`
}

// encodeConfig gzip-compresses and base64-encodes the raw config JSON and wraps
// it in the encodedConfig envelope. Output is deterministic for equal input:
// the gzip header ModTime/Name are left unset and a fixed compression level is
// used, so equal configs always produce byte-identical envelopes.
//
// It refuses to encode a config larger than MaxDecodedConfigSize, symmetric with
// decodeConfig, so the operator never emits a resource it would reject on read.
func encodeConfig(raw []byte) ([]byte, error) {
	maxSize := MaxDecodedConfigSize()
	if len(raw) > maxSize {
		logf.Log.WithName("wasm").Info(
			"wasm config exceeds the configured size limit; not generating it",
			"sizeBytes", len(raw), "limitBytes", maxSize, "envVar", maxDecodedConfigSizeEnvVar)
		return nil, fmt.Errorf("wasm config size %d exceeds %d bytes (set %s to raise the limit)", len(raw), maxSize, maxDecodedConfigSizeEnvVar)
	}

	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip writer: %w", err)
	}
	if _, err := gz.Write(raw); err != nil {
		return nil, fmt.Errorf("failed to gzip wasm config: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	envelope := encodedConfig{
		Encoding:             EncodingGzipBase64,
		Data:                 base64.StdEncoding.EncodeToString(buf.Bytes()),
		MaxDecompressedBytes: shimMaxDecompressedBytes(),
	}
	return json.Marshal(envelope)
}

// shimMaxDecompressedBytes returns MaxDecodedConfigSize clamped to the range the
// wasm-shim accepts for maxDecompressedBytes (1..math.MaxInt32); out-of-range
// values would make the shim reject the whole config.
func shimMaxDecompressedBytes() int {
	maxSize := MaxDecodedConfigSize()
	if maxSize > math.MaxInt32 {
		return math.MaxInt32
	}
	if maxSize < 1 {
		return defaultMaxDecodedConfigSize
	}
	return maxSize
}

// ConfigMaxDecompressedBytes returns the maxDecompressedBytes the wasm-shim will
// apply to the given (possibly enveloped) config: the envelope's field when
// present, otherwise the shim default (32 MiB). Plain (non-envelope) config also
// maps to the shim default. The reconcilers' equality checks use this so that a
// change to the limit (e.g. via WASM_MAX_DECODED_CONFIG_SIZE) is detected and
// the EnvoyFilter / EnvoyExtensionPolicy is updated accordingly.
func ConfigMaxDecompressedBytes(raw []byte) int {
	var envelope encodedConfig
	if err := json.Unmarshal(raw, &envelope); err == nil &&
		envelope.Encoding == EncodingGzipBase64 && envelope.Data != "" && envelope.MaxDecompressedBytes > 0 {
		return envelope.MaxDecompressedBytes
	}
	return defaultMaxDecodedConfigSize
}

// decodeConfig reverses encodeConfig. It is backward compatible: if raw is not a
// gzip+base64 envelope (e.g. plain config JSON), raw is returned unchanged so it
// can be unmarshalled directly.
func decodeConfig(raw []byte) ([]byte, error) {
	var envelope encodedConfig
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Encoding != EncodingGzipBase64 || envelope.Data == "" {
		// Not a (valid) envelope: treat as plain config JSON.
		return raw, nil
	}

	compressed, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode wasm config: %w", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gz.Close()

	maxSize := MaxDecodedConfigSize()
	decoded, err := io.ReadAll(io.LimitReader(gz, int64(maxSize)+1))
	if err != nil {
		return nil, fmt.Errorf("failed to gunzip wasm config: %w", err)
	}
	if len(decoded) > maxSize {
		logf.Log.WithName("wasm").Info(
			"rejected wasm config: decompressed size exceeds the configured limit; the resource will be overwritten with the operator-built config",
			"limitBytes", maxSize, "envVar", maxDecodedConfigSizeEnvVar)
		return nil, fmt.Errorf("decompressed wasm config exceeds %d bytes (set %s to raise the limit)", maxSize, maxDecodedConfigSizeEnvVar)
	}
	return decoded, nil
}
