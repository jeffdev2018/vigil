package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Brain injection reference evaluation (JEF-414). The corpus of B02 plus 24
// tickets, each naming the note a person would expect the run to receive.
// For every ticket the test builds the claim query the same way the claim
// does, then compares what the OLD selection (pinned + 20 most recent) and
// the NEW one (pinned + relevant + recent) would inject.
//
// Default runs rank lexically (no embeddings provider). The floor holds the
// new selection; the old rate is reported for comparison, never asserted.

const brainInjectMinRate = 0.90

type brainInjectNote struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

type brainInjectIssue struct {
	ID          string   `json:"id"`
	Lang        string   `json:"lang"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Project     string   `json:"project"`
	Labels      []string `json:"labels"`
	Relevant    []string `json:"relevant"`
	OldNote     bool     `json:"old_note"`
}

func readBrainInjectJSON(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func TestBrainInjectionReferenceRate(t *testing.T) {
	dir := filepath.Join("..", "service", "testdata")
	var notes []brainInjectNote
	var issues []brainInjectIssue
	readBrainInjectJSON(t, filepath.Join(dir, "brain_search_eval", "corpus.json"), &notes)
	readBrainInjectJSON(t, filepath.Join(dir, "brain_inject_eval", "issues.json"), &issues)
	if len(notes) == 0 || len(issues) == 0 {
		t.Fatal("empty reference set")
	}

	origEmbedder := testHandler.BrainEmbedder
	testHandler.BrainEmbedder = nil
	t.Cleanup(func() { testHandler.BrainEmbedder = origEmbedder })

	workspaceID := brainWorkspace(t)
	keyByID := map[string]string{}
	for _, n := range notes {
		tags := n.Tags
		if len(tags) > 8 {
			tags = tags[:8]
		}
		created := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: n.Title, Content: n.Content, Tags: tags})
		keyByID[created.ID] = n.Key
	}

	ctx := context.Background()
	wsUUID := parseUUID(workspaceID)
	keysOf := func(rows []db.WorkspaceNote) map[string]bool {
		out := make(map[string]bool, len(rows))
		for _, row := range rows {
			out[keyByID[uuidToString(row.ID)]] = true
		}
		return out
	}

	type miss struct{ id, query string }
	var misses []miss
	newHits, oldHits := 0, 0
	byKind := map[string][2]int{}
	for _, issue := range issues {
		query := brainClaimQuery(issue.Title, issue.Description, issue.Project, issue.Labels)
		selected, _, err := testHandler.TaskService.SelectWorkspaceNotesForBrief(ctx, wsUUID, query)
		if err != nil {
			t.Fatalf("select for %s: %v", issue.ID, err)
		}
		legacy, err := testHandler.TaskService.LoadWorkspaceNotesForBrief(ctx, wsUUID)
		if err != nil {
			t.Fatalf("legacy select for %s: %v", issue.ID, err)
		}
		gotNew, gotOld := keysOf(selected), keysOf(legacy)
		hitNew, hitOld := true, true
		for _, want := range issue.Relevant {
			if !gotNew[want] {
				hitNew = false
			}
			if !gotOld[want] {
				hitOld = false
			}
		}
		if hitNew {
			newHits++
		} else {
			misses = append(misses, miss{issue.ID, issue.Title})
		}
		if hitOld {
			oldHits++
		}
		c := byKind[issue.Kind]
		if hitNew {
			c[0]++
		}
		c[1]++
		byKind[issue.Kind] = c
	}

	rate := float64(newHits) / float64(len(issues))
	var report strings.Builder
	fmt.Fprintf(&report, "brain injection: new %.3f (%d/%d), old pinned+recent %.3f (%d/%d)\n",
		rate, newHits, len(issues), float64(oldHits)/float64(len(issues)), oldHits, len(issues))
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		c := byKind[k]
		fmt.Fprintf(&report, "  %-18s %.3f (%d/%d)\n", k, float64(c[0])/float64(c[1]), c[0], c[1])
	}
	for _, m := range misses {
		fmt.Fprintf(&report, "  miss %s %q\n", m.id, m.query)
	}
	t.Log(report.String())
	if rate < brainInjectMinRate {
		t.Errorf("injection rate %.3f is below the floor %.2f", rate, brainInjectMinRate)
	}
}
