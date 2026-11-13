package wasm

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// EncodingGzipBase64 identifies the wasm config wire encoding where the raw
// config JSON is gzip-compressed and then base64-encoded. The embedded wasm
// config can grow with the number of policies and the size of the Gateway API
// topology, approaching the Kubernetes custom-resource size ceiling (~1.5 MB);
// compressing it keeps the EnvoyFilter / EnvoyExtensionPolicy within limits.
const EncodingGzipBase64 = "gzip+base64"

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
func encodeConfig(raw []byte) ([]byte, error) {
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

	decoded, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("failed to gunzip wasm config: %w", err)
	}
	return decoded, nil
}
