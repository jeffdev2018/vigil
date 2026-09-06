package service

import "testing"

// Canonical layer for the walkthrough settings shape and the ingest
// normalisation. The handler suite covers the endpoints and the completion
// hook; it does not re-run this matrix.

func TestPrWalkthroughFromSettingsNeverEnablesOnGarbage(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"not json":     "{",
		"absent block": `{"doc_drift":{"enabled":true}}`,
		"wrong type":   `{"pr_walkthrough":"yes"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if got := PrWalkthroughFromSettings([]byte(raw)); got.Enabled || got.AgentID != "" {
				t.Fatalf("settings = %+v, want the disabled default", got)
			}
		})
	}
	got := PrWalkthroughFromSettings([]byte(`{"pr_walkthrough":{"enabled":true,"agent_id":"  a1  "}}`))
	if !got.Enabled || got.AgentID != "a1" {
		t.Fatalf("settings = %+v", got)
	}
}

func TestValidatePrWalkthroughSettingsNeedsAnAgentToEnable(t *testing.T) {
	if err := ValidatePrWalkthroughSettings(PrWalkthroughSettings{Enabled: true}); err == nil {
		t.Fatal("enabling with no agent was accepted")
	}
	if err := ValidatePrWalkthroughSettings(PrWalkthroughSettings{Enabled: true, AgentID: "  "}); err == nil {
		t.Fatal("enabling with a blank agent was accepted")
	}
	if err := ValidatePrWalkthroughSettings(PrWalkthroughSettings{}); err != nil {
		t.Fatalf("disabled settings rejected: %v", err)
	}
}

func TestNormalizeGroupsOrdersByKindAndKeepsUnknownKindsAsNoise(t *testing.T) {
	groups := NormalizePrWalkthroughGroups([]PrWalkthroughGroup{
		{Title: "generated client", Kind: "generated"},
		{Title: "lockfile", Kind: "wildcard-from-a-newer-agent"},
		{Title: "the change", Kind: "CORE"},
		{Title: "coverage", Kind: " test "},
	})
	var got []string
	for _, g := range groups {
		got = append(got, g.Kind)
	}
	// Fixed render order, and nothing dropped: an unknown vocabulary must
	// degrade to "skippable", never to a missing chapter.
	want := []string{"core", "test", "generated", "noise"}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
	if groups[3].Title != "lockfile" {
		t.Errorf("unknown kind lost its group: %+v", groups[3])
	}
}

func TestNormalizeGroupsCapsEverythingAnAgentCanInflate(t *testing.T) {
	many := make([]PrWalkthroughGroup, PrWalkthroughMaxGroups+5)
	for i := range many {
		many[i] = PrWalkthroughGroup{Kind: "core", Title: "g"}
	}
	if got := len(NormalizePrWalkthroughGroups(many)); got != PrWalkthroughMaxGroups {
		t.Errorf("groups = %d, want the cap %d", got, PrWalkthroughMaxGroups)
	}

	long := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'x'
		}
		return string(b)
	}
	files := make([]PrWalkthroughFile, PrWalkthroughMaxFilesPerGroup+3)
	for i := range files {
		files[i] = PrWalkthroughFile{Path: "p", Hunks: []PrWalkthroughHunk{{
			OldStart: -5, Lines: long(PrWalkthroughMaxHunkLines + 10),
			Explanation: long(PrWalkthroughMaxExplanation + 10),
		}}}
	}
	out := NormalizePrWalkthroughGroups([]PrWalkthroughGroup{{
		Kind: "core", Title: long(PrWalkthroughMaxTitle + 10),
		Rationale: long(PrWalkthroughMaxRationale + 10), Files: files,
	}})
	g := out[0]
	if len(g.Title) != PrWalkthroughMaxTitle || len(g.Rationale) != PrWalkthroughMaxRationale {
		t.Errorf("title/rationale not clipped: %d/%d", len(g.Title), len(g.Rationale))
	}
	if len(g.Files) != PrWalkthroughMaxFilesPerGroup {
		t.Errorf("files = %d, want the cap %d", len(g.Files), PrWalkthroughMaxFilesPerGroup)
	}
	h := g.Files[0].Hunks[0]
	if len(h.Lines) != PrWalkthroughMaxHunkLines || len(h.Explanation) != PrWalkthroughMaxExplanation {
		t.Errorf("hunk not clipped: %d/%d", len(h.Lines), len(h.Explanation))
	}
	if h.OldStart != 0 {
		t.Errorf("negative line anchor stored as %d, want 0", h.OldStart)
	}
}

func TestNormalizeGroupsDropsPathlessFiles(t *testing.T) {
	out := NormalizePrWalkthroughGroups([]PrWalkthroughGroup{{
		Kind:  "core",
		Files: []PrWalkthroughFile{{Path: "  "}, {Path: "real.go"}},
	}})
	if len(out[0].Files) != 1 || out[0].Files[0].Path != "real.go" {
		t.Fatalf("files = %+v", out[0].Files)
	}
}
