package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Drift detection (K40), pure parts. A run drifts when it repeats the
// same tool call (same tool, same normalised arguments) N times in a row,
// or re-reads the same file N times without an editing call on that file
// in between. Thresholds come from the workspace; a normal run never
// trips them.

const (
	DriftRepeatedAction = "repeated_action"
	DriftFileRereadLoop = "file_reread_loop"
	ReasonDriftDetected = "drift_detected"
	DriftWindow         = 100
)

type Drift struct {
	Enabled                 bool `json:"enabled"`
	RepeatedActionThreshold int  `json:"repeated_action_threshold"`
	FileRereadThreshold     int  `json:"file_reread_threshold"`
}

var DefaultDrift = Drift{Enabled: true, RepeatedActionThreshold: 5, FileRereadThreshold: 8}

func DriftSettings(settings []byte) Drift {
	out := DefaultDrift
	if len(settings) == 0 {
		return out
	}
	var s struct {
		Drift *struct {
			Enabled                 *bool `json:"enabled"`
			RepeatedActionThreshold int   `json:"repeated_action_threshold"`
			FileRereadThreshold     int   `json:"file_reread_threshold"`
		} `json:"drift"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Drift == nil {
		return out
	}
	if s.Drift.Enabled != nil {
		out.Enabled = *s.Drift.Enabled
	}
	if s.Drift.RepeatedActionThreshold > 0 {
		out.RepeatedActionThreshold = s.Drift.RepeatedActionThreshold
	}
	if s.Drift.FileRereadThreshold > 0 {
		out.FileRereadThreshold = s.Drift.FileRereadThreshold
	}
	return out
}

// DriftVerdict is what tripped, and the seqs of the calls involved.
type DriftVerdict struct {
	Reason string  `json:"reason"`
	Detail string  `json:"detail"`
	Seqs   []int32 `json:"seqs"`
}

// fingerprint hashes a tool call with its arguments in a stable key order.
func fingerprint(tool string, input []byte) string {
	var args any
	if json.Unmarshal(input, &args) != nil {
		args = string(input)
	}
	norm, _ := json.Marshal(normalize(args))
	sum := sha256.Sum256([]byte(strings.ToLower(tool) + "\x00" + string(norm)))
	return hex.EncodeToString(sum[:8])
}

func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]any, 0, len(keys)*2)
		for _, k := range keys {
			out = append(out, k, normalize(t[k]))
		}
		return out
	case []any:
		for i := range t {
			t[i] = normalize(t[i])
		}
		return t
	case string:
		return strings.TrimSpace(t)
	}
	return v
}

var readingToolWords = []string{"read", "cat", "view", "open", "get_file", "show"}

func isReadingTool(name string) bool {
	lower := strings.ToLower(name)
	for _, w := range readingToolWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// DetectDrift inspects the run's recent tool calls, oldest first.
func DetectDrift(cfg Drift, calls []db.TaskMessage) (DriftVerdict, bool) {
	if !cfg.Enabled || len(calls) == 0 {
		return DriftVerdict{}, false
	}
	// Repeated action: the last N calls share one fingerprint.
	if n := cfg.RepeatedActionThreshold; n > 0 && len(calls) >= n {
		last := calls[len(calls)-1]
		fp := fingerprint(last.Tool.String, last.Input)
		same := true
		seqs := []int32{}
		for _, c := range calls[len(calls)-n:] {
			if fingerprint(c.Tool.String, c.Input) != fp {
				same = false
				break
			}
			seqs = append(seqs, c.Seq)
		}
		if same {
			return DriftVerdict{Reason: DriftRepeatedAction, Detail: last.Tool.String + " called " + itoa(n) + " times with the same arguments", Seqs: seqs}, true
		}
	}
	// Re-read loop: reads of one path since its last edit.
	if n := cfg.FileRereadThreshold; n > 0 {
		reads := map[string][]int32{}
		for _, c := range calls {
			paths := ToolInputPaths(c.Input)
			if len(paths) == 0 {
				continue
			}
			switch {
			case IsEditingTool(c.Tool.String):
				for _, p := range paths {
					delete(reads, p)
				}
			case isReadingTool(c.Tool.String):
				for _, p := range paths {
					reads[p] = append(reads[p], c.Seq)
					if len(reads[p]) >= n {
						return DriftVerdict{Reason: DriftFileRereadLoop, Detail: p + " read " + itoa(len(reads[p])) + " times without a write in between", Seqs: reads[p]}, true
					}
				}
			}
		}
	}
	return DriftVerdict{}, false
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
