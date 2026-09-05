package daemon

import (
	"testing"
)

// TestConvertMemoriesForEnv pins the claim-wire mapping (JEF-269): states ride
// through as a parallel array, and a fact without a state — a payload from a
// pre-governance server, or a short/missing states array — defaults to
// approved rather than being quarantined as a draft.
func TestConvertMemoriesForEnv(t *testing.T) {
	t.Parallel()

	if got := convertMemoriesForEnv(nil, nil); got != nil {
		t.Fatalf("nil memories = %v, want nil", got)
	}

	contents := []string{"Approved fact.", "Draft hypothesis.", "Stateless legacy fact.", "Newer fact."}
	states := []string{"approved", "draft", ""} // deliberately short: index 3 has no state

	got := convertMemoriesForEnv(contents, states)
	if len(got) != 4 {
		t.Fatalf("converted %d memories, want 4", len(got))
	}
	want := []struct{ content, state string }{
		{"Approved fact.", "approved"},
		{"Draft hypothesis.", "draft"},
		{"Stateless legacy fact.", "approved"},
		{"Newer fact.", "approved"},
	}
	for i, w := range want {
		if got[i].Content != w.content || got[i].State != w.state {
			t.Errorf("memory[%d] = %q/%q, want %q/%q", i, got[i].Content, got[i].State, w.content, w.state)
		}
	}

	// An unknown state value never reaches the execenv as-is.
	got = convertMemoriesForEnv([]string{"x"}, []string{"bogus"})
	if got[0].State != "approved" {
		t.Errorf("unknown state = %q, want approved", got[0].State)
	}
}
