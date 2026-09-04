package triage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPayloadEmpty(t *testing.T) {
	out := BuildPayload(nil)
	if !json.Valid(out) {
		t.Fatalf("BuildPayload(nil) produced invalid JSON: %s", out)
	}
	if !strings.Contains(string(out), `"size":0`) {
		t.Fatalf("BuildPayload(nil) = %s, want size 0", out)
	}
}

func TestBuildPayloadEmbedsSmallValidJSON(t *testing.T) {
	raw := []byte(`{"alert":"payment-gateway","count":3}`)
	out := BuildPayload(raw)
	if !json.Valid(out) {
		t.Fatalf("invalid JSON: %s", out)
	}
	var p struct {
		Size      int             `json:"size"`
		Body      json.RawMessage `json:"body"`
		Truncated bool            `json:"truncated"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Size != len(raw) || p.Truncated {
		t.Fatalf("got size=%d truncated=%v, want size=%d embedded", p.Size, p.Truncated, len(raw))
	}
	if string(p.Body) != string(raw) {
		t.Fatalf("body = %s, want the raw payload embedded", p.Body)
	}
}

func TestBuildPayloadStubsInvalidJSON(t *testing.T) {
	out := BuildPayload([]byte("this is not json"))
	if !json.Valid(out) {
		t.Fatalf("invalid JSON: %s", out)
	}
	if !strings.Contains(string(out), `"truncated":true`) {
		t.Fatalf("got %s, want truncated stub for invalid JSON", out)
	}
	if strings.Contains(string(out), "this is not json") {
		t.Fatalf("invalid payload bytes must not be embedded: %s", out)
	}
}

func TestBuildPayloadStubsOversized(t *testing.T) {
	raw := []byte(`"` + strings.Repeat("a", maxStoredPayloadBytes+100) + `"`)
	if !json.Valid(raw) {
		t.Fatal("test fixture must be valid JSON")
	}
	out := BuildPayload(raw)
	if !json.Valid(out) {
		t.Fatalf("invalid JSON: %s", out)
	}
	if !strings.Contains(string(out), `"truncated":true`) {
		t.Fatalf("got %s, want truncated stub for oversized payload", out)
	}
	// The column CHECK caps pg_column_size at 32 KiB; the stored form must
	// stay far below it no matter how large the delivery was.
	if len(out) > 1024 {
		t.Fatalf("stored payload grew to %d bytes, want a small stub", len(out))
	}
}
