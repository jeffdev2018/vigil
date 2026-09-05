package vcs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Cross-provider review (K15): the diff of a pull / merge request is read
// with the connection token; GitLab's changes payload is rebuilt as one
// unified diff; a 401 maps to ErrUnauthorized.

func TestForgejoPullRequestDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/org/repo/pulls/7.diff" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("diff --git a/x b/x\n+hello\n"))
	}))
	defer srv.Close()
	p, _ := For("forgejo")
	diff, err := p.PullRequestDiff(context.Background(), srv.URL, "tok", "org", "repo", 7)
	if err != nil || !strings.HasPrefix(diff, "diff --git a/x") {
		t.Fatalf("diff = %q err = %v", diff, err)
	}
	if _, err := p.PullRequestDiff(context.Background(), srv.URL, "bad", "org", "repo", 7); err != ErrUnauthorized {
		t.Fatalf("bad token err = %v", err)
	}
}

func TestGitLabPullRequestDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/org%2Frepo/merge_requests/3/changes" && r.URL.EscapedPath() != "/api/v4/projects/org%2Frepo/merge_requests/3/changes" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"changes":[{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-x\n+y\n"},{"old_path":"b.go","new_path":"c.go","diff":"@@ -0,0 +1 @@\n+z"}]}`))
	}))
	defer srv.Close()
	p, _ := For("gitlab")
	diff, err := p.PullRequestDiff(context.Background(), srv.URL, "tok", "org", "repo", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"diff --git a/a.go b/a.go", "--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n", "diff --git a/b.go b/c.go", "+z\n"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
	if _, err := p.PullRequestDiff(context.Background(), srv.URL, "bad", "org", "repo", 3); err != ErrUnauthorized {
		t.Fatalf("bad token err = %v", err)
	}
}

// K42 debt: the platform updates the branch and merges; a refusal is a
// conflict, not an error.
func TestForgejoMergePullRequest(t *testing.T) {
	var calls []string
	mergeStatus := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/merge") {
			w.WriteHeader(mergeStatus)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p, _ := For("forgejo")
	out, err := p.MergePullRequest(context.Background(), srv.URL, "tok", "org", "repo", 4)
	if err != nil || !out.Merged || len(calls) != 2 || calls[0] != "POST /api/v1/repos/org/repo/pulls/4/update" || calls[1] != "POST /api/v1/repos/org/repo/pulls/4/merge" {
		t.Fatalf("merge = %+v err = %v calls = %v", out, err, calls)
	}
	mergeStatus = http.StatusMethodNotAllowed
	if out, err := p.MergePullRequest(context.Background(), srv.URL, "tok", "org", "repo", 4); err != nil || !out.Conflict || out.Merged {
		t.Fatalf("refused merge = %+v err = %v", out, err)
	}
}

func TestGitLabMergePullRequest(t *testing.T) {
	mergeStatus := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if strings.HasSuffix(r.URL.Path, "/merge") {
			w.WriteHeader(mergeStatus)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	p, _ := For("gitlab")
	if out, err := p.MergePullRequest(context.Background(), srv.URL, "tok", "org", "repo", 2); err != nil || !out.Merged {
		t.Fatalf("merge = %+v err = %v", out, err)
	}
	mergeStatus = http.StatusNotAcceptable
	if out, err := p.MergePullRequest(context.Background(), srv.URL, "tok", "org", "repo", 2); err != nil || !out.Conflict {
		t.Fatalf("refused merge = %+v err = %v", out, err)
	}
}
