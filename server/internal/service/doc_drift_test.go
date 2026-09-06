package service

import (
	"encoding/json"
	"testing"
)

// Agent context drift detection (K56). Canonical layer for the settings shape:
// what a blank or corrupt blob reads as, what a submission may carry, and when
// a repository is due. The handler suite covers the endpoints and the hooks.

func TestDocDriftDefaultsAndCorruptSettings(t *testing.T) {
	for name, blob := range map[string][]byte{
		"empty":      nil,
		"not json":   []byte("{"),
		"no block":   []byte(`{"code_health":{"enabled":true}}`),
		"null block": []byte(`{"doc_drift":null}`),
	} {
		got := DocDriftFromSettings(blob)
		if got.Enabled {
			t.Fatalf("%s: a workspace that never configured this is never enabled: %+v", name, got)
		}
		if len(got.Docs) != len(DocDriftDefaultDocs) || got.Docs[0] != "CLAUDE.md" {
			t.Fatalf("%s: default documents: %+v", name, got.Docs)
		}
		if !got.OpenPR {
			t.Fatalf("%s: opening the draft pull request is the default: %+v", name, got)
		}
		if got.LastChecked == nil || got.ScanTasks == nil {
			t.Fatalf("%s: the server-owned maps are always usable: %+v", name, got)
		}
	}
}

func TestDocDriftFromSettingsRoundTrips(t *testing.T) {
	cfg := DocDriftSettings{
		Enabled: true, AgentID: "agent-1", OpenPR: false,
		Docs:        []string{" AGENTS.md ", "AGENTS.md", "", "docs/x.md"},
		LastChecked: map[string]string{"  ": "ignored", "https://git/repo": " abc123 "},
		ScanTasks:   map[string]string{"https://git/repo": "task-1"},
	}
	raw, err := json.Marshal(map[string]any{"doc_drift": cfg})
	if err != nil {
		t.Fatal(err)
	}
	got := DocDriftFromSettings(raw)
	if !got.Enabled || got.AgentID != "agent-1" || got.OpenPR {
		t.Fatalf("scalars: %+v", got)
	}
	if len(got.Docs) != 2 || got.Docs[0] != "AGENTS.md" || got.Docs[1] != "docs/x.md" {
		t.Fatalf("documents are trimmed and de-duplicated in order: %+v", got.Docs)
	}
	if got.LastChecked["https://git/repo"] != "abc123" || len(got.LastChecked) != 1 {
		t.Fatalf("last_checked is trimmed and blank keys dropped: %+v", got.LastChecked)
	}
	if got.ScanTasks["https://git/repo"] != "task-1" {
		t.Fatalf("scan_tasks: %+v", got.ScanTasks)
	}
}

func TestValidateDocDriftSettings(t *testing.T) {
	ok := DocDriftDefaults()
	ok.Enabled = true
	ok.AgentID = "agent-1"
	if err := ValidateDocDriftSettings(ok); err != nil {
		t.Fatalf("the defaults plus an agent are valid: %v", err)
	}

	bad := map[string]DocDriftSettings{
		"no documents":          {Docs: nil},
		"enabled without agent": {Docs: []string{"CLAUDE.md"}, Enabled: true},
		"absolute path":         {Docs: []string{"/etc/passwd"}},
		"parent segment":        {Docs: []string{"../../secrets.md"}},
		"windows separator":     {Docs: []string{`..\\secrets.md`}},
		"unnormalised":          {Docs: []string{"docs/./CLAUDE.md"}},
		"trailing slash":        {Docs: []string{"docs/"}},
	}
	for name, cfg := range bad {
		if err := ValidateDocDriftSettings(cfg); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}

	many := DocDriftSettings{Docs: make([]string, DocDriftMaxDocs+1)}
	for i := range many.Docs {
		many.Docs[i] = "doc" + string(rune('a'+i)) + ".md"
	}
	if err := ValidateDocDriftSettings(many); err == nil {
		t.Fatalf("more than %d documents must be refused", DocDriftMaxDocs)
	}
}

// The trigger is a moved commit, not a clock: the same commit is never checked
// twice, a new commit always is, and a repository nobody has indexed is not
// claimed to have drifted.
func TestDocDriftDue(t *testing.T) {
	cfg := DocDriftDefaults()
	cfg.Enabled = true
	cfg.AgentID = "agent-1"
	cfg.LastChecked["repo"] = "abc"

	cases := []struct {
		name   string
		repo   string
		commit string
		want   bool
	}{
		{"same commit", "repo", "abc", false},
		{"moved", "repo", "def", true},
		{"never indexed", "repo", "", false},
		{"never checked", "other", "abc", true},
	}
	for _, c := range cases {
		if got := DocDriftDue(cfg, c.repo, c.commit); got != c.want {
			t.Fatalf("%s: due=%v want %v", c.name, got, c.want)
		}
	}

	off := cfg
	off.Enabled = false
	if DocDriftDue(off, "repo", "def") {
		t.Fatalf("a disabled workspace is never due")
	}
	noAgent := cfg
	noAgent.AgentID = ""
	if DocDriftDue(noAgent, "repo", "def") {
		t.Fatalf("without an agent there is nothing to run")
	}
}
