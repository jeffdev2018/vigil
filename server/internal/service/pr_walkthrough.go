package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

// Narrative pull request walkthrough (F05 / JEF-16).
//
// A reviewer opening a pull request gets a file list sorted alphabetically by
// a tool that does not know which files matter. The real change — the two
// functions that moved the behaviour — sits between a lockfile, a generated
// client and forty lines of test scaffolding, and the reviewer reconstructs
// the story before they can start reviewing it.
//
// So a read-only agent run reads the diff and tells the story instead:
// ordered groups, each with a title, a rationale, and a line-anchored
// explanation per hunk. Grouping is the product — `core` first, then `test`,
// then `generated`, then `noise` — because "which of these 40 files do I
// actually have to read" is the question the file list refuses to answer.
//
// The whole configuration lives under workspace.settings.pr_walkthrough, so
// there is no table for it. Disabled by default: this spends agent budget on
// every push, and that has to be an opt-in someone chose.

const (
	// PrWalkthroughMaxGroups bounds one stored walkthrough. Past this the
	// narrative has stopped being a narrative.
	PrWalkthroughMaxGroups = 12
	// PrWalkthroughMaxFilesPerGroup and PrWalkthroughMaxHunksPerFile bound
	// what one group may claim, so a run cannot turn a capped diff back into
	// an unbounded row.
	PrWalkthroughMaxFilesPerGroup = 200
	PrWalkthroughMaxHunksPerFile  = 100
	PrWalkthroughMaxTitle         = 200
	PrWalkthroughMaxRationale     = 2000
	PrWalkthroughMaxExplanation   = 2000
	// PrWalkthroughMaxHunkLines bounds one stored hunk body.
	PrWalkthroughMaxHunkLines = 20000
	PrWalkthroughMaxPath      = 500
)

// PrWalkthroughKinds is the fixed render order. It is the product decision the
// grouping exists for, so it lives here and not in a component: `core` is what
// the reviewer must read, `noise` is what they may skip.
var PrWalkthroughKinds = []string{"core", "test", "generated", "noise"}

// PrWalkthroughSettings is the workspace configuration.
type PrWalkthroughSettings struct {
	Enabled bool `json:"enabled"`
	// AgentID runs the read-only walkthrough. Required to enable.
	AgentID string `json:"agent_id"`
}

// PrWalkthroughFromSettings reads the configuration off a workspace settings
// blob. Anything missing or unparseable falls back to the default — in
// particular Enabled stays false, so a corrupt blob never turns this on.
func PrWalkthroughFromSettings(settings []byte) PrWalkthroughSettings {
	var s struct {
		PrWalkthrough *PrWalkthroughSettings `json:"pr_walkthrough"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.PrWalkthrough == nil {
		return PrWalkthroughSettings{}
	}
	return PrWalkthroughSettings{
		Enabled: s.PrWalkthrough.Enabled,
		AgentID: strings.TrimSpace(s.PrWalkthrough.AgentID),
	}
}

// ValidatePrWalkthroughSettings reports what is wrong with a submitted
// configuration, or nil. The agent is checked by the handler, which is the
// only layer that can resolve it against the workspace.
func ValidatePrWalkthroughSettings(s PrWalkthroughSettings) error {
	if s.Enabled && strings.TrimSpace(s.AgentID) == "" {
		return fmt.Errorf("agent_id is required to enable pull request walkthroughs")
	}
	return nil
}

// --- the report contract ---------------------------------------------------

// PrWalkthroughHunk is one changed region with the explanation of why it
// changed. OldStart / NewStart anchor it in the diff the reviewer is reading.
type PrWalkthroughHunk struct {
	OldStart    int    `json:"old_start"`
	NewStart    int    `json:"new_start"`
	Lines       string `json:"lines"`
	Explanation string `json:"explanation"`
	// MovedFrom names the path this hunk came from when the run recognised a
	// move, which is how a reviewer skips a rename that reads as a rewrite.
	MovedFrom string `json:"moved_from"`
}

type PrWalkthroughFile struct {
	Path  string              `json:"path"`
	Hunks []PrWalkthroughHunk `json:"hunks"`
}

// PrWalkthroughGroup is one chapter of the story.
type PrWalkthroughGroup struct {
	Title     string              `json:"title"`
	Kind      string              `json:"kind"`
	Rationale string              `json:"rationale"`
	Files     []PrWalkthroughFile `json:"files"`
}

// PrWalkthroughReport is the fenced block the run ends with.
type PrWalkthroughReport struct {
	Groups []PrWalkthroughGroup `json:"groups"`
}

// NormalizePrWalkthroughGroups is the ingest boundary: everything below comes
// out of an agent's free-text answer, so nothing here is trusted for length,
// vocabulary or order.
//
// An unknown kind becomes `noise` rather than being dropped — a newer agent
// vocabulary should degrade into "the reviewer may skip this", never into a
// missing chapter. Groups are returned in the fixed render order so the stored
// row is already what the UI shows and no consumer has to re-sort it.
func NormalizePrWalkthroughGroups(in []PrWalkthroughGroup) []PrWalkthroughGroup {
	byKind := map[string][]PrWalkthroughGroup{}
	kept := 0
	for _, g := range in {
		if kept >= PrWalkthroughMaxGroups {
			break
		}
		kind := strings.ToLower(strings.TrimSpace(g.Kind))
		if !isPrWalkthroughKind(kind) {
			kind = "noise"
		}
		out := PrWalkthroughGroup{
			Title:     clip(g.Title, PrWalkthroughMaxTitle),
			Kind:      kind,
			Rationale: clip(g.Rationale, PrWalkthroughMaxRationale),
			Files:     normalizePrWalkthroughFiles(g.Files),
		}
		byKind[kind] = append(byKind[kind], out)
		kept++
	}
	ordered := make([]PrWalkthroughGroup, 0, kept)
	for _, kind := range PrWalkthroughKinds {
		ordered = append(ordered, byKind[kind]...)
	}
	return ordered
}

func normalizePrWalkthroughFiles(in []PrWalkthroughFile) []PrWalkthroughFile {
	out := make([]PrWalkthroughFile, 0, len(in))
	for _, f := range in {
		if len(out) >= PrWalkthroughMaxFilesPerGroup {
			break
		}
		path := clip(f.Path, PrWalkthroughMaxPath)
		if path == "" {
			continue
		}
		hunks := make([]PrWalkthroughHunk, 0, len(f.Hunks))
		for _, h := range f.Hunks {
			if len(hunks) >= PrWalkthroughMaxHunksPerFile {
				break
			}
			hunks = append(hunks, PrWalkthroughHunk{
				OldStart:    maxInt(h.OldStart, 0),
				NewStart:    maxInt(h.NewStart, 0),
				Lines:       clip(h.Lines, PrWalkthroughMaxHunkLines),
				Explanation: clip(h.Explanation, PrWalkthroughMaxExplanation),
				MovedFrom:   clip(h.MovedFrom, PrWalkthroughMaxPath),
			})
		}
		out = append(out, PrWalkthroughFile{Path: path, Hunks: hunks})
	}
	return out
}

func isPrWalkthroughKind(kind string) bool {
	for _, k := range PrWalkthroughKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// clip trims and truncates one free-text field from an agent answer.
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return util.TruncateUTF8Bytes(s, max)
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
