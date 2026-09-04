package service

import (
	"strings"
	"testing"
)

// Traffic control (K18), pure parts. The DB path (conflicts, inbox, pause)
// is in internal/handler/traffic_control_test.go.

func TestTrafficPathHelpers(t *testing.T) {
	for name, want := range map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "apply_patch": true, "str_replace_based_edit_tool": true, "Read": false, "Grep": false, "Bash": false} {
		if IsEditingTool(name) != want {
			t.Fatalf("IsEditingTool(%s) = %v", name, !want)
		}
	}
	paths := ToolInputPaths([]byte(`{"file_path":"/w/repo/src/a.go","paths":["docs/b.md"," "],"content":"x\ny"}`))
	if strings.Join(paths, ",") != "/w/repo/src/a.go,docs/b.md" {
		t.Fatalf("paths = %v", paths)
	}
	if RelativePath("/w/repo/src/a.go", "/w/repo") != "src/a.go" || RelativePath("./docs/b.md", "") != "docs/b.md" {
		t.Fatal("RelativePath")
	}
	over := OverlapPaths([]string{"src/a.go", "/other/tree/src/c.go", "docs/x.md"}, []string{"src/a.go", "src/c.go"})
	if strings.Join(over, ",") != "src/a.go,/other/tree/src/c.go" {
		t.Fatalf("overlap = %v", over)
	}
	if got := IntersectPaths([]string{"a", "b", "c"}, []string{"c", "a"}); strings.Join(got, ",") != "a,c" {
		t.Fatalf("intersect = %v", got)
	}
	if TrafficControlSettings([]byte(`{"traffic_control":{"pause_on_conflict":true}}`)).PauseOnConflict != true || TrafficControlSettings(nil).PauseOnConflict {
		t.Fatal("settings")
	}
}
