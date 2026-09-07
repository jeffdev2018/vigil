package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The transition gate (F28) is only as good as its coverage: a status write
// that reaches the database without passing it is a bypass, and a status write
// the platform makes on its own behalf must NOT pass it — a workspace rule that
// could stop the stuck-issue sweeper would strand issues rather than govern
// people.
//
// Both halves are the same question — "what writes issue.status, and which
// side of the gate is it on" — so one enumeration answers both. The map below
// is the answer, and the test fails when the tree grows a call site that is not
// in it. Adding a status writer therefore forces a deliberate classification.

type statusWriterClass string

const (
	// gated: an HTTP entry point that runs the transition gate before writing.
	statusWriterGated statusWriterClass = "gated"
	// system: the platform writing its own state. Never gated, by design.
	// Every entry here must say WHY in its comment.
	statusWriterSystem statusWriterClass = "system"
	// downstream: the write a gated entry point performs after the gate has
	// already run, or a delegation to one. Gating it again would double-charge
	// the same request.
	statusWriterDownstream statusWriterClass = "downstream"
)

// issueStatusWriters classifies every non-test call site that can change
// issue.status. Key is "<path>:<line-content-fragment>"-free: it is just the
// file, because a file's call sites always share a class here. If that ever
// stops being true, split the key rather than loosening the check.
var issueStatusWriters = map[string]statusWriterClass{
	// --- Gated HTTP entry points -------------------------------------------
	// UpdateIssue, BatchUpdateIssues and CreateIssue all call
	// transitionAllowsStatus / transitionAllowsCreate before the write.
	"internal/handler/issue.go": statusWriterGated,

	// --- Downstream of a gate ----------------------------------------------
	// MoveIssue derives a position then delegates to UpdateIssue, which is the
	// gated path. Gating here would evaluate the same move twice.
	"internal/handler/issue_move.go": statusWriterDownstream,
	// The approve/reject handlers apply a move an approver just authorised.
	// The approver's decision IS the gate; re-running it would refuse the very
	// transition that was approved.
	"internal/handler/issue_transition_api.go": statusWriterDownstream,
	// IssueService.Create is called by the handlers above, which have already
	// gated the create. The service has no request and therefore no actor.
	"internal/service/issue.go": statusWriterDownstream,
	// UpdateIssuePublic is the service half of the public update path.
	"internal/service/issue_public.go": statusWriterDownstream,

	// --- System writers ----------------------------------------------------
	// The stuck-issue sweeper returns an in_progress issue with no live task to
	// todo. It is the platform undoing its own abandoned work.
	"cmd/server/runtime_sweeper.go": statusWriterSystem,
	// HandleFailedTasks does the same after a run fails.
	"internal/service/task.go": statusWriterSystem,
	// A merged pull request closes its issue. The move is a fact about the
	// repository, not a request from an actor.
	"internal/handler/github.go": statusWriterSystem,
	// Undo (K69) replays an effect's PREVIOUS value. A rule that blocked the
	// inverse would make an undone run unrecoverable.
	"internal/handler/agent_effect.go":         statusWriterSystem,
	"internal/handler/agent_effect_preview.go": statusWriterSystem,
	// A cancelled eval run cancels the issue it created.
	"internal/handler/eval.go": statusWriterSystem,
	// The interview parks an issue while it waits for an answer and restores it
	// afterwards; both are the platform's own bookkeeping.
	"internal/handler/interview.go": statusWriterSystem,
	// The watchdog resets a stalled issue to todo.
	"internal/handler/watchdog.go": statusWriterSystem,
	// The adversarial critic (F25) parks a finished delivery in review while
	// its critic reads it. The platform is holding its own work: a workspace
	// rule that could refuse the hold would leave the issue between a finished
	// run and a verdict nobody is waiting for.
	"internal/handler/critic.go": statusWriterSystem,
	// The Linear bridge (K21) mirrors a state change made in Linear.
	"internal/integrations/linear/sync.go": statusWriterSystem,
	// A low-confidence run is sent back for review by the platform.
	"internal/service/run_confidence.go": statusWriterSystem,
	// An autopilot creates the issue its schedule or webhook asked for.
	"internal/service/autopilot.go": statusWriterSystem,
}

var statusWriterPattern = regexp.MustCompile(`\.(UpdateIssueStatus|UpdateIssue|CreateIssueWithOrigin)\(`)

func TestIssueTransitionGateCoversEveryStatusWriter(t *testing.T) {
	root, err := repoServerRoot()
	if err != nil {
		t.Fatalf("locate server root: %v", err)
	}

	found := map[string]bool{}
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "generated", "testdata", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !statusWriterPattern.Match(body) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found[filepath.ToSlash(rel)] = true
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk server tree: %v", walkErr)
	}
	if len(found) == 0 {
		t.Fatalf("found no status writers at all; the scan is broken, not the tree")
	}

	var unclassified, stale []string
	for path := range found {
		if _, ok := issueStatusWriters[path]; !ok {
			unclassified = append(unclassified, path)
		}
	}
	for path := range issueStatusWriters {
		if !found[path] {
			stale = append(stale, path)
		}
	}
	sort.Strings(unclassified)
	sort.Strings(stale)

	for _, path := range unclassified {
		t.Errorf("%s writes issue.status but is not classified in issueStatusWriters. "+
			"Decide whether it runs the F28 transition gate (gated), sits behind one (downstream), "+
			"or is the platform writing its own state (system), then add it with the reason.", path)
	}
	for _, path := range stale {
		t.Errorf("issueStatusWriters classifies %s, which no longer writes issue.status; drop the entry", path)
	}
}

// gateEntryPoints are the files that must run the transition gate: the ones
// classified gated above, plus the create entry points that reach the database
// through IssueService.Create rather than writing the row themselves — those
// carry the actor, so the gate has to live in them and nowhere lower.
var gateEntryPoints = []string{
	"internal/handler/issue.go",
	"internal/handler/source_context.go",
}

// The gate must actually be wired into every entry point. A file can lose its
// call while keeping its classification, which the enumeration above cannot
// see.
func TestGatedStatusWritersCallTheTransitionGate(t *testing.T) {
	root, err := repoServerRoot()
	if err != nil {
		t.Fatalf("locate server root: %v", err)
	}
	for _, path := range gateEntryPoints {
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(body)
		if !strings.Contains(src, "transitionAllowsStatus") &&
			!strings.Contains(src, "transitionAllowsCreate") &&
			!strings.Contains(src, "decideTransition") {
			t.Errorf("%s is classified gated but calls no transition gate; the rule is bypassable there", path)
		}
	}
}

// repoServerRoot walks up from the package directory to the module root.
func repoServerRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", os.ErrNotExist
}
