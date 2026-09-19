package triage

import (
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

// StoredBody returns the trigger payload BuildPayload kept, or nil when it
// was truncated or absent.
func StoredBody(payload []byte) []byte {
	var p storedPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.Truncated {
		return nil
	}
	return []byte(p.Body)
}
