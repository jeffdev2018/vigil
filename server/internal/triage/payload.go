package triage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// maxStoredPayloadBytes bounds the raw trigger payload embedded in a triage
// item's payload JSONB. The column itself enforces pg_column_size <= 32768;
// the margin below leaves room for the wrapper keys and jsonb overhead.
const maxStoredPayloadBytes = 28 * 1024

type storedPayload struct {
	Size      int             `json:"size"`
	Body      json.RawMessage `json:"body,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}

// BuildPayload shapes the stored payload JSONB: the raw trigger payload is
// embedded when it is small enough and valid JSON, otherwise the item keeps a
// size stub. The result is always valid JSON so the column CHECK holds.
func BuildPayload(triggerPayload []byte) []byte {
	p := storedPayload{Size: len(triggerPayload)}
	if len(triggerPayload) > 0 && len(triggerPayload) <= maxStoredPayloadBytes && json.Valid(triggerPayload) {
		p.Body = json.RawMessage(triggerPayload)
	} else if len(triggerPayload) > 0 {
		p.Truncated = true
	}
	out, err := json.Marshal(p)
	if err != nil {
		// storedPayload marshals from plain primitives and a validated
		// RawMessage; a failure here is unreachable.
		return []byte(`{"size":0}`)
	}
	return out
}

// ContentDigest fingerprints the inbound content so two deliveries can be
// compared even when the transport carries no idempotency key.
func ContentDigest(title string, triggerPayload []byte) string {
	sum := sha256.New()
	sum.Write([]byte(NormalizeTitle(title)))
	sum.Write([]byte{0})
	sum.Write(triggerPayload)
	return hex.EncodeToString(sum.Sum(nil))
}
