package cel

import "encoding/json"

// StringLiteral returns a properly escaped CEL string literal for the given value.
// It JSON-encodes the value, producing a double-quoted, escape-safe string that
// is also a valid CEL string literal. This prevents quote injection when
// interpolating user-supplied values into CEL expressions or predicates.
func StringLiteral(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
