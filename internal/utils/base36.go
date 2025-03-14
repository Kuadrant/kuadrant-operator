package utils

import (
	"crypto/sha256"
	"strings"

	"github.com/martinlindhe/base36"
)

func ToBase36Hash(s string) string {
	hash := sha256.Sum224([]byte(s))
	// convert the hash to base36 (alphanumeric) to decrease collision probabilities
	return strings.ToLower(base36.EncodeBytes(hash[:]))
}

func ToBase36HashLen(s string, l int) string {
	return ToBase36Hash(s)[:l]
}
