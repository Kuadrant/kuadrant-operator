package wasm

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

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

// maxDecodedConfigSizeEnvVar overrides defaultMaxDecodedConfigSize (in bytes).
const maxDecodedConfigSizeEnvVar = "WASM_MAX_DECODED_CONFIG_BYTES"

// maxDecodedConfigSize caps the number of bytes decodeConfig will decompress,
// preventing a decompression bomb (gzip can expand repetitive data ~1000x) in
// an attacker-controlled EnvoyFilter/EnvoyExtensionPolicy from exhausting the
// operator's memory on reconcile.
var maxDecodedConfigSize = func() int {
	v, err := env.GetInt(maxDecodedConfigSizeEnvVar, defaultMaxDecodedConfigSize)
	if err != nil || v <= 0 {
		return defaultMaxDecodedConfigSize
	}
	return v
}()

// encodedConfig is the self-describing envelope wrapping the encoded wasm config:
//
//	{ "encoding": "gzip+base64", "data": "<base64(gzip(configJSON))>" }
type encodedConfig struct {
	Encoding string `json:"encoding"`
	Data     string `json:"data"`
}

// encodeConfig gzip-compresses and base64-encodes the raw config JSON and wraps
// it in the encodedConfig envelope. Output is deterministic for equal input:
// the gzip header ModTime/Name are left unset and a fixed compression level is
// used, so equal configs always produce byte-identical envelopes.
//
// It refuses to encode a config larger than maxDecodedConfigSize, symmetric with
// decodeConfig, so the operator never emits a resource it would reject on read.
func encodeConfig(raw []byte) ([]byte, error) {
	if len(raw) > maxDecodedConfigSize {
		logf.Log.WithName("wasm").Info(
			"wasm config exceeds the configured size limit; not generating it",
			"sizeBytes", len(raw), "limitBytes", maxDecodedConfigSize, "envVar", maxDecodedConfigSizeEnvVar)
		return nil, fmt.Errorf("wasm config size %d exceeds %d bytes (set %s to raise the limit)", len(raw), maxDecodedConfigSize, maxDecodedConfigSizeEnvVar)
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
	}
	return json.Marshal(envelope)
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

	decoded, err := io.ReadAll(io.LimitReader(gz, int64(maxDecodedConfigSize)+1))
	if err != nil {
		return nil, fmt.Errorf("failed to gunzip wasm config: %w", err)
	}
	if len(decoded) > maxDecodedConfigSize {
		logf.Log.WithName("wasm").Info(
			"rejected wasm config: decompressed size exceeds the configured limit; the resource will be overwritten with the operator-built config",
			"limitBytes", maxDecodedConfigSize, "envVar", maxDecodedConfigSizeEnvVar)
		return nil, fmt.Errorf("decompressed wasm config exceeds %d bytes (set %s to raise the limit)", maxDecodedConfigSize, maxDecodedConfigSizeEnvVar)
	}
	return decoded, nil
}
