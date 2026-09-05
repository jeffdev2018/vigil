package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Drift detection (K40), pure parts: repeated calls trip only past the
// threshold, re-reads reset on a write, a varied run never trips, settings
// parse with defaults. The DB path is in internal/handler/drift_test.go.

func call(seq int32, tool, input string) db.TaskMessage {
	return db.TaskMessage{Seq: seq, Type: "tool_use", Tool: pgtype.Text{String: tool, Valid: true}, Input: []byte(input)}
}

func TestDetectDrift(t *testing.T) {
	cfg := Drift{Enabled: true, RepeatedActionThreshold: 3, FileRereadThreshold: 3}
	same := []db.TaskMessage{call(1, "Bash", `{"command":"go test ./..."}`), call(2, "Bash", `{ "command" : "go test ./..." }`), call(3, "bash", `{"command":"go test ./..."}`)}
	if v, ok := DetectDrift(cfg, same); !ok || v.Reason != DriftRepeatedAction || len(v.Seqs) != 3 {
		t.Fatalf("repeated = %+v ok=%v", v, ok)
	}
	if _, ok := DetectDrift(cfg, same[:2]); ok {
		t.Fatal("two identical calls are under the threshold")
	}
	varied := []db.TaskMessage{call(1, "Read", `{"file_path":"a.go"}`), call(2, "Edit", `{"file_path":"a.go"}`), call(3, "Read", `{"file_path":"a.go"}`), call(4, "Bash", `{"command":"go build"}`), call(5, "Read", `{"file_path":"b.go"}`), call(6, "Read", `{"file_path":"a.go"}`)}
	if v, ok := DetectDrift(cfg, varied); ok {
		t.Fatalf("a normal run must not trip: %+v", v)
	}
	loop := append(varied, call(7, "Read", `{"file_path":"a.go"}`))
	if v, ok := DetectDrift(cfg, loop); !ok || v.Reason != DriftFileRereadLoop || len(v.Seqs) != 3 || v.Seqs[0] != 3 {
		t.Fatalf("reread = %+v ok=%v", v, ok)
	}
	if _, ok := DetectDrift(Drift{Enabled: false, RepeatedActionThreshold: 1, FileRereadThreshold: 1}, same); ok {
		t.Fatal("disabled never trips")
	}
	d := DriftSettings([]byte(`{"drift":{"enabled":false,"repeated_action_threshold":9}}`))
	if d.Enabled || d.RepeatedActionThreshold != 9 || d.FileRereadThreshold != 8 {
		t.Fatalf("settings = %+v", d)
	}
	if DriftSettings(nil) != DefaultDrift {
		t.Fatal("defaults")
	}
}
