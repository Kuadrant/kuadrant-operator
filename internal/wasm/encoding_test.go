//go:build unit

package wasm

import (
	"bytes"
	"encoding/json"
	"testing"

	"gotest.tools/assert"
)

func TestEncodeDecodeConfigRoundTrip(t *testing.T) {
	raw := []byte(`{"services":{"ratelimit":{"type":"ratelimit"}},"actionSets":[]}`)

	encoded, err := encodeConfig(raw)
	assert.NilError(t, err)

	decoded, err := decodeConfig(encoded)
	assert.NilError(t, err)

	assert.Assert(t, bytes.Equal(raw, decoded), "decoded bytes should equal the original input")
}

func TestEncodeConfigEnvelopeShape(t *testing.T) {
	encoded, err := encodeConfig([]byte(`{"actionSets":[]}`))
	assert.NilError(t, err)

	var envelope encodedConfig
	assert.NilError(t, json.Unmarshal(encoded, &envelope))
	assert.Equal(t, envelope.Encoding, EncodingGzipBase64)
	assert.Assert(t, envelope.Data != "", "envelope data should not be empty")
}

func TestDecodeConfigPlainJSONPassthrough(t *testing.T) {
	// Non-envelope (plain config JSON) must be returned unchanged for backward compat.
	plain := []byte(`{"services":{},"actionSets":[]}`)
	decoded, err := decodeConfig(plain)
	assert.NilError(t, err)
	assert.Assert(t, bytes.Equal(plain, decoded))

	// A JSON object that happens not to be our envelope is also passed through.
	other := []byte(`{"encoding":"something-else","data":"x"}`)
	decoded, err = decodeConfig(other)
	assert.NilError(t, err)
	assert.Assert(t, bytes.Equal(other, decoded))
}

func TestEncodeConfigDeterministic(t *testing.T) {
	raw := []byte(`{"services":{"a":{"type":"ratelimit"}},"actionSets":[{"name":"set"}]}`)

	first, err := encodeConfig(raw)
	assert.NilError(t, err)
	second, err := encodeConfig(raw)
	assert.NilError(t, err)

	assert.Assert(t, bytes.Equal(first, second), "equal input must produce byte-identical envelopes")
}
