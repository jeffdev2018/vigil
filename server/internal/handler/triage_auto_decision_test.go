package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/decisions"
)

// No database here: triageMayAutoApply reads the item and suggestion it is
// handed and asks one question. The queue around it is covered by the
// DB-backed triage tests.

func decisionHandler(t *testing.T, reply string) *Handler {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &Handler{Decisions: decisions.New(decisions.Config{BaseURL: srv.URL, APIKey: "k"})}
}

func noulReply(p float64) string {
	body, _ := json.Marshal(map[string]any{
		"model": "m",
		"answers": map[string]any{
			"accept": map[string]any{"type": "noul", "noul": p},
		},
	})
	return string(body)
}

func triageItem() db.TriageItem {
	return db.TriageItem{
		Title:        "Payment webhook retries are not logged",
		BodyMarkdown: "Three customers affected since Tuesday.",
	}
}

// TestNoDecisionEndpointLeavesTheVoteAlone is the compatibility guarantee: a
// deployment without a decision endpoint behaves exactly as it did before this
// check existed.
func TestNoDecisionEndpointLeavesTheVoteAlone(t *testing.T) {
	h := &Handler{}
	ok, read := h.triageMayAutoApply(context.Background(), triageItem(),
		TriageSuggestion{Suggested: "accept", Confidence: 0.95})
	if !ok {
		t.Error("blocked an auto-apply with no endpoint configured")
	}
	if read.Asked {
		t.Error("reported a second read that never happened")
	}
}

func TestSecondReadMustBackTheVote(t *testing.T) {
	for _, tc := range []struct {
		name      string
		suggested string
		acceptP   float64
		wantApply bool
	}{
		// Accept needs a confident yes.
		{"accept, model agrees", "accept", 0.90, true},
		{"accept, at the floor", "accept", triageDecisionFloor, true},
		{"accept, model unsure", "accept", 0.60, false},
		{"accept, model says no", "accept", 0.05, false},
		// Dismiss needs a confident no, which is the mirror of the floor.
		{"dismiss, model agrees", "dismiss", 0.10, true},
		{"dismiss, at the mirror", "dismiss", 1 - triageDecisionFloor, true},
		{"dismiss, model unsure", "dismiss", 0.50, false},
		{"dismiss, model says accept", "dismiss", 0.95, false},
	} {
		h := decisionHandler(t, noulReply(tc.acceptP))
		ok, read := h.triageMayAutoApply(context.Background(), triageItem(),
			TriageSuggestion{Suggested: tc.suggested, Confidence: 0.95})
		if ok != tc.wantApply {
			t.Errorf("%s: mayApply=%v, want %v", tc.name, ok, tc.wantApply)
		}
		if !read.Asked {
			t.Errorf("%s: the read is not recorded", tc.name)
		}
		if read.Probability != tc.acceptP {
			t.Errorf("%s: probability lost: %v", tc.name, read.Probability)
		}
		// A refusal has to say why, or the audit entry cannot be explained.
		if !ok && read.Why == "" {
			t.Errorf("%s: refused without a reason", tc.name)
		}
	}
}

// TestSecondReadFailsClosed covers the case that decides whether this check is
// worth anything. An endpoint that was asked for and did not answer means the
// check did not happen, and a path that creates or discards work unattended
// must not treat "did not happen" as "passed".
func TestSecondReadFailsClosed(t *testing.T) {
	for name, reply := range map[string]string{
		"upstream error": `{"error":{"message":"overloaded"}}`,
		"empty answers":  `{"model":"m","answers":{}}`,
		"wrong question": `{"model":"m","answers":{"other":{"type":"noul","noul":0.9}}}`,
		"not json":       `nope`,
	} {
		h := decisionHandler(t, reply)
		ok, read := h.triageMayAutoApply(context.Background(), triageItem(),
			TriageSuggestion{Suggested: "dismiss", Confidence: 0.99})
		if ok {
			t.Errorf("%s: auto-applied without a second read", name)
		}
		if !read.Asked || read.Why == "" {
			t.Errorf("%s: the failure is not recorded: %+v", name, read)
		}
	}
}

// TestRawPayloadNeverLeaves pins the data bound. The nearest-neighbour vote
// matches on title, body AND payload, but a webhook or email payload carries
// addresses, identifiers and customer content that add nothing to "should this
// be accepted?" — so it is not sent.
func TestRawPayloadNeverLeaves(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(noulReply(0.9)))
	}))
	defer srv.Close()
	h := &Handler{Decisions: decisions.New(decisions.Config{BaseURL: srv.URL, APIKey: "k"})}

	item := triageItem()
	item.Payload = []byte(`{"customer_email":"someone@example.com","card_last4":"4242"}`)
	h.triageMayAutoApply(context.Background(), item,
		TriageSuggestion{Suggested: "accept", Confidence: 0.95,
			Neighbors: []TriageNeighbor{{Title: "Retry logging missing", State: "accepted"}}})

	body, _ := json.Marshal(sent)
	for _, secret := range []string{"someone@example.com", "card_last4", "4242"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("the raw payload reached the endpoint: %q found in the request", secret)
		}
	}
	// What is sent is the item's own words and the team's past decisions.
	if !strings.Contains(string(body), "Payment webhook retries") {
		t.Error("the item title was not sent, so the read had nothing to judge")
	}
	if !strings.Contains(string(body), "Retry logging missing") {
		t.Error("the team's past decisions were not sent")
	}
}

// TestLongBodyIsBounded keeps one item from sending an unbounded amount.
func TestLongBodyIsBounded(t *testing.T) {
	var sent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(noulReply(0.9)))
	}))
	defer srv.Close()
	h := &Handler{Decisions: decisions.New(decisions.Config{BaseURL: srv.URL, APIKey: "k"})}

	item := triageItem()
	item.BodyMarkdown = strings.Repeat("x", 200*1024)
	h.triageMayAutoApply(context.Background(), item,
		TriageSuggestion{Suggested: "accept", Confidence: 0.95})

	if len(sent) > triageItemBodyCap+4096 {
		t.Errorf("request is %d bytes for a %d-byte body cap", len(sent), triageItemBodyCap)
	}
}
