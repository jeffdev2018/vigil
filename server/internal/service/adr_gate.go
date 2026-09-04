package service

import "encoding/json"

// ADR gate (K29): when an accepted run is complex enough that merging its PR
// requires a recorded decision. Stored in workspace.settings:
//
//	"adr_gate": {"file_threshold": 10, "require_on_migration": true}
//
// Absent, the defaults apply. A zero threshold and require_on_migration false
// together turn the gate off.
type ADRGate struct {
	FileThreshold      int  `json:"file_threshold"`
	RequireOnMigration bool `json:"require_on_migration"`
}

var DefaultADRGate = ADRGate{FileThreshold: 10, RequireOnMigration: true}

func ADRGateSettings(settings []byte) ADRGate {
	if len(settings) == 0 {
		return DefaultADRGate
	}
	var s struct {
		Gate *ADRGate `json:"adr_gate"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Gate == nil {
		return DefaultADRGate
	}
	if s.Gate.FileThreshold < 0 {
		s.Gate.FileThreshold = 0
	}
	return *s.Gate
}

func (g ADRGate) Enabled() bool { return g.FileThreshold > 0 || g.RequireOnMigration }

// Requires says whether a run that edited `files` distinct files, `migration`
// telling whether one of them lives under a migrations directory, needs a
// decision record before its PR merges.
func (g ADRGate) Requires(files int, migration bool) bool {
	return (g.FileThreshold > 0 && files >= g.FileThreshold) || (g.RequireOnMigration && migration)
}
