package handler

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/mcpgov"
)

// The catalogue is data, and a typo in it is a tool a client can see and
// never call. These invariants hold for every leaf, current and future.
func TestMCPCatalogueLeavesAreWellFormed(t *testing.T) {
	names := map[string]bool{}
	actions := map[string]string{}
	risks := map[string]bool{}
	for _, risk := range mcpgov.Risks {
		risks[risk] = true
	}

	for _, leaf := range mcpLeaves {
		if names[leaf.Name] {
			t.Errorf("duplicate leaf name %q", leaf.Name)
		}
		names[leaf.Name] = true

		key := leaf.Group + "/" + leaf.Action
		if other, seen := actions[key]; seen {
			t.Errorf("leaf %q reuses action %q of %q; the compound surface resolves group+action to one leaf", leaf.Name, key, other)
		}
		actions[key] = leaf.Name

		if !risks[leaf.Risk] {
			t.Errorf("leaf %q has risk %q, which is not a governed class", leaf.Name, leaf.Risk)
		}
		if _, ok := mcpGroupDescriptions[leaf.Group]; !ok {
			t.Errorf("leaf %q is in group %q, which the compound surface cannot describe", leaf.Name, leaf.Group)
		}

		// A path parameter that does not name a placeholder leaves a "{...}"
		// in the URL and the call fails at dispatch, not at schema time.
		for _, p := range leaf.Params {
			if p.In != "path" {
				continue
			}
			if !strings.Contains(leaf.Path, "{"+p.Name+"}") {
				t.Errorf("leaf %q declares path param %q but its path %q has no such placeholder", leaf.Name, p.Name, leaf.Path)
			}
			if !p.Required {
				t.Errorf("leaf %q path param %q is optional; a missing placeholder is a dispatch error", leaf.Name, p.Name)
			}
		}
		if leaf.Method == "GET" && len(leaf.Fixed) > 0 {
			t.Errorf("leaf %q is a GET with a fixed body, which is never sent", leaf.Name)
		}
	}
}

// The Brain now has a capture inbox, and it is reachable from MCP: capturing
// is how a client parks something it is not sure the workspace wants as a
// note.
func TestMCPCatalogueCoversTheBrainCaptureInbox(t *testing.T) {
	for _, name := range []string{"note_search", "note_capture", "note_inbox", "note_organize", "note_capture_reopen", "note_capture_delete"} {
		leaf, ok := mcpLeafByName[name]
		if !ok {
			t.Fatalf("catalogue has no %q leaf", name)
		}
		if leaf.Group != "vigil_brain" {
			t.Errorf("%s is in group %q, want vigil_brain", name, leaf.Group)
		}
	}
	// A delete is the one destructive operation on this surface; it must not
	// be classed as an ordinary internal write.
	if got := mcpLeafByName["note_capture_delete"].Risk; got != mcpgov.RiskExternal {
		t.Errorf("note_capture_delete risk = %q, want %q so the trust dial gates it", got, mcpgov.RiskExternal)
	}
}

// Provenance is not the caller's to choose: whatever a client passes, a
// capture filed through MCP is stamped origin "mcp".
func TestMCPNoteCaptureStampsItsOwnOrigin(t *testing.T) {
	leaf := mcpLeafByName["note_capture"]
	_, _, body, err := leaf.build(map[string]any{"content": "worth keeping", "origin": "web"}, mcpCaller{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if body["origin"] != "mcp" {
		t.Fatalf("origin = %#v, want mcp even though the caller asked for web", body["origin"])
	}
	if body["content"] != "worth keeping" {
		t.Errorf("content = %#v", body["content"])
	}
}

func TestMCPCaptureLeafPathsCarryTheCaptureID(t *testing.T) {
	for _, name := range []string{"note_organize", "note_capture_reopen", "note_capture_delete"} {
		path, _, _, err := mcpLeafByName[name].build(map[string]any{"capture_id": "cap-1", "action": "discard"}, mcpCaller{})
		if err != nil {
			t.Fatalf("%s build: %v", name, err)
		}
		if !strings.Contains(path, "/api/brain/captures/cap-1") {
			t.Errorf("%s path = %q", name, path)
		}
	}
}

// Réveil programmé is reachable from MCP: a client schedules a wake-up on an
// issue, reads what is pending, cancels one, and proposes a recurring
// automation from a sentence. The risk classes are the point — drafting asks
// a model and writes nothing, so it must not be gated as a write, and
// proposing files a paused autopilot, so it must not be classed external.
func TestMCPCatalogueCoversWakeUps(t *testing.T) {
	want := map[string]struct {
		group  string
		risk   string
		method string
	}{
		"issue_followup":        {"vigil_issue", mcpgov.RiskInternalWrite, "POST"},
		"issue_followups":       {"vigil_issue", mcpgov.RiskRead, "GET"},
		"issue_followup_cancel": {"vigil_issue", mcpgov.RiskInternalWrite, "DELETE"},
		"autopilot_draft":       {"vigil_autopilot", mcpgov.RiskRead, "POST"},
		"autopilot_propose":     {"vigil_autopilot", mcpgov.RiskInternalWrite, "POST"},
	}
	for name, w := range want {
		leaf, ok := mcpLeafByName[name]
		if !ok {
			t.Fatalf("catalogue has no %q leaf", name)
		}
		if leaf.Group != w.group {
			t.Errorf("%s is in group %q, want %q", name, leaf.Group, w.group)
		}
		if leaf.Risk != w.risk {
			t.Errorf("%s risk = %q, want %q", name, leaf.Risk, w.risk)
		}
		if leaf.Method != w.method {
			t.Errorf("%s method = %q, want %q", name, leaf.Method, w.method)
		}
	}
}

// The cancel leaf carries both ids into the path; a missing placeholder is a
// dispatch error, not a schema one, so it is worth building once here.
func TestMCPFollowupCancelPathCarriesBothIDs(t *testing.T) {
	path, _, _, err := mcpLeafByName["issue_followup_cancel"].build(map[string]any{"id": "MUL-1", "followup_id": "f1"}, mcpCaller{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if path != "/api/issues/MUL-1/followups/f1" {
		t.Errorf("path = %q", path)
	}
}
