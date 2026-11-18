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
	"k8s.io/utils/ptr"
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

// decodeLimit lazily reads and validates a decompression cap from the environment
// variable named by envVar, caching the effective value and any validation error on
// first access. It bounds decodeConfig's output to prevent a decompression bomb
// (gzip can expand repetitive data ~1000x) in an attacker-controlled
// EnvoyFilter/EnvoyExtensionPolicy from exhausting the operator's memory on
// reconcile.
type decodeLimit struct {
	envVar string
	once   sync.Once
	value  int
	err    error
}

// load reads envVar (a resource.Quantity such as "128Mi", "64Mi", "32Mi", or a
// bare byte integer). An unset value uses the 32 MiB default; an invalid value
// (unparseable, non-positive, or greater than math.MaxInt32) records an error and
// leaves the value at the default.
func (l *decodeLimit) load() {
	l.value = defaultMaxDecodedConfigSize
	s := env.GetString(l.envVar, "")
	if s == "" {
		return
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		l.err = fmt.Errorf("invalid %s %q: %w", l.envVar, s, err)
		return
	}
	switch {
	case q.Sign() <= 0:
		l.err = fmt.Errorf("%s %q must be a positive size", l.envVar, s)
	case q.CmpInt64(math.MaxInt32) > 0:
		l.err = fmt.Errorf("%s %q exceeds the maximum of %d bytes", l.envVar, s, math.MaxInt32)
	default:
		l.value = int(q.Value())
	}
}

// Value returns the effective cap in bytes, falling back to the 32 MiB default on
// an invalid value. It is always within 1..math.MaxInt32, matching the wasm-shim's
// maxDecompressedBytes bound, so the operator's cap and the value it advertises to
// the shim are always the same.
func (l *decodeLimit) Value() int {
	l.once.Do(l.load)
	return l.value
}

// Err returns the validation error for WASM_MAX_DECODED_CONFIG_SIZE, if any.
func (l *decodeLimit) Err() error {
	l.once.Do(l.load)
	return l.err
}

// maxDecodedConfigSize is the process-wide decode limit.
var maxDecodedConfigSize = &decodeLimit{envVar: maxDecodedConfigSizeEnvVar}

// MaxDecodedConfigSize returns the effective wasm config decompression cap (bytes).
// An invalid WASM_MAX_DECODED_CONFIG_SIZE falls back to the 32 MiB default; callers
// that want to reject a misconfiguration should use ValidateMaxDecodedConfigSize.
func MaxDecodedConfigSize() int {
	return maxDecodedConfigSize.Value()
}

// ValidateMaxDecodedConfigSize returns an error if WASM_MAX_DECODED_CONFIG_SIZE is
// set to an invalid value (unparseable, non-positive, or greater than
// math.MaxInt32). Call it once at startup so the operator fails fast on a
// misconfigured limit instead of silently using the default.
func ValidateMaxDecodedConfigSize() error {
	return maxDecodedConfigSize.Err()
}

// encodedConfig is the self-describing envelope wrapping the encoded wasm config:
//
//	{ "encoding": "gzip+base64", "data": "<base64(gzip(configJSON))>", "maxDecompressedBytes": 33554432 }
//
// maxDecompressedBytes is read by the wasm-shim to cap decompression on the data
// plane (see Kuadrant/wasm-shim#450). The operator sets it to its own limit so
// both sides enforce the same bound; when omitted the shim defaults to 32 MiB.
// It is a pointer so an omitted field (nil, valid) is distinguishable from an
// explicit value (including an invalid non-positive one the shim rejects).
type encodedConfig struct {
	Encoding             string `json:"encoding"`
	Data                 string `json:"data"`
	MaxDecompressedBytes *int   `json:"maxDecompressedBytes,omitempty"`
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
		Encoding: EncodingGzipBase64,
		Data:     base64.StdEncoding.EncodeToString(buf.Bytes()),
		// maxSize is bounded to 1..math.MaxInt32, which is exactly the range the
		// wasm-shim accepts for maxDecompressedBytes, so no clamping is needed here.
		MaxDecompressedBytes: ptr.To(maxSize),
	}
	return json.Marshal(envelope)
}

// ConfigMaxDecompressedBytes returns the maxDecompressedBytes the wasm-shim will
// apply to the given (possibly enveloped) config: the envelope's field when
// present, otherwise the shim default (32 MiB). Plain (non-envelope) config also
// maps to the shim default. The reconcilers' equality checks use this so that a
// change to the limit (e.g. via WASM_MAX_DECODED_CONFIG_SIZE) is detected and
// the EnvoyFilter / EnvoyExtensionPolicy is updated accordingly.
func ConfigMaxDecompressedBytes(raw []byte) int {
	var envelope encodedConfig
	if err := json.Unmarshal(raw, &envelope); err != nil ||
		envelope.Encoding != EncodingGzipBase64 || envelope.Data == "" {
		// Not a (valid) envelope: the shim default applies.
		return defaultMaxDecodedConfigSize
	}
	if envelope.MaxDecompressedBytes == nil {
		// Omitted: the shim defaults to 32 MiB.
		return defaultMaxDecodedConfigSize
	}
	// Present: return it verbatim, including an invalid non-positive value the shim
	// rejects, so it differs from the operator's always-valid cap and the resource
	// is rewritten instead of being treated as the default.
	return *envelope.MaxDecompressedBytes
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
