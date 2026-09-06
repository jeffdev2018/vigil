package service

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Agent context document drift detection (K56).
//
// The agent context document — CLAUDE.md, AGENTS.md, the conventions doc — is
// the one file every agent run reads before it does anything. It is also the
// file nobody remembers to update: a Makefile target is renamed, a package
// moves, a convention is dropped, and the document keeps confidently telling
// every run something that stopped being true. The cost is silent and
// compounding, because a run that trusts a stale instruction does not fail, it
// does the wrong thing correctly.
//
// This is the scheduled check that names the gap. When the default branch
// moves (the repo index's last indexed commit is the signal — no git event
// plumbing), a read-only agent run compares the document against what the
// repository actually declares and reports the sections that drifted. The
// server turns that into a proposal a human reviews as a DRAFT pull request.
//
// Never a direct commit, and never the whole document rewritten: the proposal
// is incremental and it is reviewed. A tool that silently edits the file every
// agent reads is a tool that can silently redirect every agent.
//
// Disabled by default, admin-only to configure. The whole configuration lives
// under workspace.settings.doc_drift, so there is no table for it.

const (
	// DocDriftMaxDocs bounds how many documents one repository declares. Each
	// one is a section of the scan brief and, potentially, its own proposal.
	DocDriftMaxDocs = 10
	// DocDriftMaxDocPathLen bounds one declared path.
	DocDriftMaxDocPathLen = 300
)

// DocDriftDefaultDocs is the set a workspace that never configured this reads
// as: the two root context documents every agent harness looks for, plus this
// repository's own conventions document. A path that does not exist in a given
// repository is simply reported as absent by the scan.
var DocDriftDefaultDocs = []string{
	"CLAUDE.md",
	"AGENTS.md",
	"apps/docs/content/docs/developers/conventions.mdx",
}

// DocDriftSettings is the workspace configuration of the drift check.
type DocDriftSettings struct {
	Enabled bool `json:"enabled"`
	// AgentID runs the read-only scan and, when OpenPR is set, the run that
	// opens the draft pull request. Required to enable.
	AgentID string `json:"agent_id"`
	// Docs are the repo-relative paths to compare against the repository.
	Docs []string `json:"docs"`
	// OpenPR turns a stored proposal into a draft pull request automatically.
	// Off means the proposal waits in Settings for someone to press the button
	// — the same proposal either way, only the trigger differs.
	OpenPR bool `json:"open_pr"`
	// LastChecked is repo identifier -> the commit that repo was last checked
	// at. Server-owned: it is what makes "the default branch moved" a signal
	// rather than a clock, and a client that could rewrite it could make the
	// check fire on every tick.
	LastChecked map[string]string `json:"last_checked"`
	// ScanTasks is repo identifier -> the scan run currently in flight for it.
	// Server-owned, written in the same update as LastChecked, and the only
	// thing that makes "a scan is already running" answerable per repository:
	// a scan that finds no drift leaves no proposal row to ask.
	ScanTasks map[string]string `json:"scan_tasks"`
}

// DocDriftDefaults is what a workspace that never configured this reads as.
func DocDriftDefaults() DocDriftSettings {
	docs := make([]string, len(DocDriftDefaultDocs))
	copy(docs, DocDriftDefaultDocs)
	return DocDriftSettings{Docs: docs, OpenPR: true, LastChecked: map[string]string{}, ScanTasks: map[string]string{}}
}

// DocDriftFromSettings reads the configuration off a workspace settings blob.
// Anything missing or unparseable falls back to the default — in particular
// Enabled stays false, so a corrupt blob never turns the check on.
func DocDriftFromSettings(settings []byte) DocDriftSettings {
	out := DocDriftDefaults()
	var s struct {
		DocDrift *DocDriftSettings `json:"doc_drift"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.DocDrift == nil {
		return out
	}
	got := *s.DocDrift
	out.Enabled = got.Enabled
	out.AgentID = got.AgentID
	out.OpenPR = got.OpenPR
	if docs := NormalizeDocDriftDocs(got.Docs); len(docs) > 0 {
		out.Docs = docs
	}
	out.LastChecked = copyDocDriftMap(got.LastChecked)
	out.ScanTasks = copyDocDriftMap(got.ScanTasks)
	return out
}

// copyDocDriftMap trims a server-owned repo -> value map, dropping blank keys.
func copyDocDriftMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for repo, value := range in {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		out[repo] = strings.TrimSpace(value)
	}
	return out
}

// NormalizeDocDriftDocs trims, de-duplicates and orders the declared paths.
// Order is preserved because it is the order the brief lists them in, and a
// reviewer reading the brief should see the list they typed.
func NormalizeDocDriftDocs(docs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(docs))
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" || seen[doc] {
			continue
		}
		seen[doc] = true
		out = append(out, doc)
	}
	return out
}

// ValidateDocDriftSettings reports what is wrong with a submitted
// configuration, or nil. The agent is checked by the handler, which is the
// only layer that can resolve it against the workspace.
//
// The path rules are a trust boundary, not tidiness: these strings are handed
// to an agent run as "the files to read and patch". An absolute path or a
// `..` segment would point the run outside the checkout it was given.
func ValidateDocDriftSettings(s DocDriftSettings) error {
	if len(s.Docs) == 0 {
		return fmt.Errorf("docs must list at least one document path")
	}
	if len(s.Docs) > DocDriftMaxDocs {
		return fmt.Errorf("docs must list at most %d document paths", DocDriftMaxDocs)
	}
	for _, doc := range s.Docs {
		if err := ValidateDocDriftPath(doc); err != nil {
			return err
		}
	}
	if s.Enabled && s.AgentID == "" {
		return fmt.Errorf("agent_id is required to enable the agent context drift check")
	}
	return nil
}

// ValidateDocDriftPath is the single place a document path is judged, so the
// settings endpoint and the completion hook agree: a path an agent invented in
// its report is no more trusted than one a client submitted.
func ValidateDocDriftPath(doc string) error {
	doc = strings.TrimSpace(doc)
	switch {
	case doc == "":
		return fmt.Errorf("a document path must not be empty")
	case len(doc) > DocDriftMaxDocPathLen:
		return fmt.Errorf("a document path must be at most %d characters", DocDriftMaxDocPathLen)
	case strings.HasPrefix(doc, "/"), strings.Contains(doc, "\\"):
		return fmt.Errorf("document path %q must be relative to the repository root", doc)
	case doc != path.Clean(doc):
		return fmt.Errorf("document path %q must be normalised (no . or .. segments, no trailing slash)", doc)
	}
	for _, seg := range strings.Split(doc, "/") {
		if seg == ".." || seg == "." || seg == "" {
			return fmt.Errorf("document path %q must not leave the repository root", doc)
		}
	}
	return nil
}

// DocDriftDue answers "has this repository moved since we last checked it?".
//
// The signal is the repo index's newest indexed commit (K47) rather than a git
// event: the index is already refreshed after every run that touches the
// repository, so its commit IS "the default branch as the daemon last saw it",
// and reusing it means this feature needs no webhook, no polling of a forge,
// and no credentials of its own.
//
// A repository with no indexed commit is not due: nothing has been observed to
// compare against, so there is no drift to claim.
func DocDriftDue(s DocDriftSettings, repoIdentifier, indexedCommit string) bool {
	indexedCommit = strings.TrimSpace(indexedCommit)
	if !s.Enabled || s.AgentID == "" || indexedCommit == "" {
		return false
	}
	return s.LastChecked[strings.TrimSpace(repoIdentifier)] != indexedCommit
}
