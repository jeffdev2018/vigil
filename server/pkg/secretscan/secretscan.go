// Package secretscan masks values that look like secrets before they leave a
// trust boundary: a workspace export (K76), an approval card, a tool result
// shown to a model (K77). It is deliberately conservative and pattern-based;
// a value it misses is a value the caller must not have put there.
package secretscan

import (
	"encoding/json"
	"regexp"
)

// Mask replaces a matched value.
const Mask = "***"

var (
	// KeyRe names the JSON keys whose string values are replaced wholesale.
	KeyRe = regexp.MustCompile(`(?i)(token|secret|passw|api[_-]?key|apikey|authorization|bearer|credential|private[_-]?key|signing|cookie|session)`)
	// ValueRe catches well-known token shapes anywhere in a string.
	ValueRe = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9_-]{8,}|gh[pousr]_[A-Za-z0-9]{10,}|xox[abpr]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|Bearer\s+[A-Za-z0-9._-]{16,}|eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{10,})`)
)

// Value walks decoded JSON: a string under a secret-looking key, or a string
// shaped like a token, becomes the mask; everything else is kept.
func Value(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = Value(child, k)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = Value(child, key)
		}
		return out
	case string:
		if KeyRe.MatchString(key) || ValueRe.MatchString(t) {
			return Mask
		}
		return t
	default:
		return v
	}
}

// JSON scrubs a JSON document. Undecodable input becomes an empty object so a
// malformed blob can never carry a value through untouched.
func JSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage("{}")
	}
	out, err := json.Marshal(Value(v, ""))
	if err != nil {
		return json.RawMessage("{}")
	}
	return out
}

// Text masks token shapes inside free text.
func Text(s string) string {
	return ValueRe.ReplaceAllString(s, Mask)
}

// Found reports whether the text carries a token shape.
func Found(s string) bool { return ValueRe.MatchString(s) }
