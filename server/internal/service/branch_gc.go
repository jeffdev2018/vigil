package service

import "encoding/json"

// Dead run-branch garbage collection (JEF-388).
//
// Terminal runs leave their delivered branch on the daemon's machine when
// nobody promotes or discards it. A workspace that opts in
// (settings.branch_gc.enabled) gets a periodic sweep that enqueues a discard
// for every such branch older than ttl_days, executed by the daemon through
// the branch-action channel (JEF-255). The whole configuration lives under
// workspace.settings.branch_gc, so there is no table for it.

const (
	// BranchGCDefaultTTLDays is how old a dead branch gets before the sweep
	// discards it when the workspace does not say otherwise.
	BranchGCDefaultTTLDays = 30
	BranchGCMinTTLDays     = 1
	BranchGCMaxTTLDays     = 365
)

// BranchGCSettings is the workspace configuration of the sweep.
type BranchGCSettings struct {
	Enabled bool `json:"enabled"`
	// TTLDays is the age a terminal run's branch reaches before the sweep
	// discards it.
	TTLDays int `json:"ttl_days"`
}

// BranchGCDefaults is what a workspace that never configured this reads as.
func BranchGCDefaults() BranchGCSettings {
	return BranchGCSettings{TTLDays: BranchGCDefaultTTLDays}
}

// BranchGCFromSettings reads the configuration off a workspace settings blob.
// Anything missing, unparseable or out of range falls back to the default —
// in particular Enabled stays false, so a corrupt blob never turns branch
// deletion on.
func BranchGCFromSettings(settings []byte) BranchGCSettings {
	out := BranchGCDefaults()
	var s struct {
		BranchGC *BranchGCSettings `json:"branch_gc"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.BranchGC == nil {
		return out
	}
	out.Enabled = s.BranchGC.Enabled
	if s.BranchGC.TTLDays >= BranchGCMinTTLDays && s.BranchGC.TTLDays <= BranchGCMaxTTLDays {
		out.TTLDays = s.BranchGC.TTLDays
	}
	return out
}
