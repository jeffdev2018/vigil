package decisions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// refusingTransport records every request handed to it and performs none. It
// exists to assert an absence: checking only the returned error cannot tell
// "refused before dialing" apart from "dialed, failed, mapped to
// ErrNotConfigured".
type refusingTransport struct{ calls int }

func (t *refusingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("a disabled client must not reach upstream")
}

func TestUnconfiguredClientMakesZeroUpstreamRequests(t *testing.T) {
	// A deployment that set only the model is the shape most likely to be
	// mistaken for configured; it must be just as inert as an empty config.
	for name, cfg := range map[string]Config{
		"empty":                {},
		"model only":           {Model: "typesafe/jev-1.13"},
		"key without endpoint": {APIKey: "k"},
		"endpoint without key": {BaseURL: "https://example.invalid/decisions"},
	} {
		transport := &refusingTransport{}
		cfg.HTTPClient = &http.Client{Transport: transport}
		c := New(cfg)
		if c.Enabled() {
			t.Fatalf("%s: client reports enabled", name)
		}
		_, err := c.Ask(context.Background(), "state", map[string]Question{
			"q": Noul("ready?", "yes", "no"),
		})
		if !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("%s: got %v, want ErrNotConfigured", name, err)
		}
		if transport.calls != 0 {
			t.Fatalf("%s: made %d upstream request(s)", name, transport.calls)
		}
	}
}

// TestCriteriaShapePerType pins the wire contract that is easiest to get
// wrong: criteria is a map for choice and noul, and an ordered array for
// score. The upstream answers 400 on the wrong one, and a 400 discovered in
// production is a decision silently not taken.
func TestCriteriaShapePerType(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","answers":{"pick":{"type":"choice","choice":"a","confidence":0.9}}}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if _, err := c.Ask(context.Background(), "s", map[string]Question{
		"pick":  Choice("which?", map[string]string{"a": "first", "b": "second"}),
		"rate":  Score("how bad?", []string{"low", "mid", "high"}),
		"ready": Noul("ready?", "yes", "no"),
	}); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	questions, _ := got["questions"].(map[string]any)
	for name, wantArray := range map[string]bool{"pick": false, "rate": true, "ready": false} {
		q, ok := questions[name].(map[string]any)
		if !ok {
			t.Fatalf("question %q missing from request", name)
		}
		switch criteria := q["criteria"].(type) {
		case []any:
			if !wantArray {
				t.Errorf("%s: criteria encoded as array, want object", name)
			}
		case map[string]any:
			if wantArray {
				t.Errorf("%s: criteria encoded as object, want ordered array", name)
			}
			_ = criteria
		default:
			t.Errorf("%s: criteria encoded as %T", name, criteria)
		}
	}
	// Score levels must keep the order they were given: the answer's score is
	// an index into them, so a reordering silently changes its meaning.
	rate, _ := questions["rate"].(map[string]any)
	levels, _ := rate["criteria"].([]any)
	if len(levels) != 3 || levels[0] != "low" || levels[2] != "high" {
		t.Errorf("score levels reordered: %v", levels)
	}
}

// TestChoiceOutsideDeclaredSpaceIsRejected covers the one upstream answer a
// caller must never act on: an option that was not offered. Acting on it would
// mean a typed decision quietly behaving like free text.
func TestChoiceOutsideDeclaredSpaceIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","answers":{"pick":{"type":"choice","choice":"elsewhere","confidence":1}}}`))
	}))
	defer srv.Close()

	allowed := map[string]string{"a": "first", "b": "second"}
	res, err := New(Config{BaseURL: srv.URL, APIKey: "k"}).
		Ask(context.Background(), "s", map[string]Question{"pick": Choice("which?", allowed)})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if _, _, ok := res.ChoiceIn("pick", allowed); ok {
		t.Fatal("accepted a choice that was never offered")
	}
	if _, _, ok := res.ChoiceIn("absent", allowed); ok {
		t.Fatal("accepted an answer to a question that was not asked")
	}
}

func TestAnswerAccessors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","answers":{
			"pick":{"type":"choice","choice":"b","probabilities":{"a":0.1,"b":0.9},"confidence":0.81},
			"rate":{"type":"score","score":2,"confidence":1},
			"ready":{"type":"noul","noul":0.79}},
			"usage":{"input_tokens":512,"output_tokens":78,"cost":0.0000215}}`))
	}))
	defer srv.Close()

	allowed := map[string]string{"a": "first", "b": "second"}
	res, err := New(Config{BaseURL: srv.URL, APIKey: "k"}).Ask(
		context.Background(), map[string]any{"title": "t"},
		map[string]Question{"pick": Choice("which?", allowed)})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if choice, conf, ok := res.ChoiceIn("pick", allowed); !ok || choice != "b" || conf != 0.81 {
		t.Errorf("ChoiceIn = %q %v %v", choice, conf, ok)
	}
	if score, conf, ok := res.ScoreIn("rate"); !ok || score != 2 || conf != 1 {
		t.Errorf("ScoreIn = %v %v %v", score, conf, ok)
	}
	if p, ok := res.NoulIn("ready"); !ok || p != 0.79 {
		t.Errorf("NoulIn = %v %v", p, ok)
	}
	// A noul read as a choice, or the reverse, must not silently return a
	// zero value that reads as a real answer.
	if _, _, ok := res.ChoiceIn("ready", allowed); ok {
		t.Error("read a noul as a choice")
	}
	if _, ok := res.NoulIn("pick"); ok {
		t.Error("read a choice as a noul")
	}
	if res.Usage.Cost == 0 || res.Usage.InputTokens != 512 {
		t.Errorf("usage not decoded: %+v", res.Usage)
	}
}

func TestUpstreamFailuresAreReportedNotGuessed(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"429": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
		},
		"malformed body": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		},
		"no answers": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"model":"m","answers":{}}`))
		},
	} {
		srv := httptest.NewServer(handler)
		_, err := New(Config{BaseURL: srv.URL, APIKey: "k"}).Ask(
			context.Background(), "s", map[string]Question{"q": Noul("?", "y", "n")})
		srv.Close()
		if err == nil {
			t.Errorf("%s: call succeeded; a caller would act on nothing", name)
		}
	}
}

func TestAskRefusesAnEmptyQuestionSet(t *testing.T) {
	transport := &refusingTransport{}
	c := New(Config{BaseURL: "https://example.invalid/d", APIKey: "k",
		HTTPClient: &http.Client{Transport: transport}})
	if _, err := c.Ask(context.Background(), "s", nil); err == nil {
		t.Fatal("a call with no questions should not be sent")
	}
	if transport.calls != 0 {
		t.Fatalf("made %d upstream request(s) for an empty question set", transport.calls)
	}
}

func TestBearerTokenIsSent(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"model":"m","answers":{"q":{"type":"noul","noul":1}}}`))
	}))
	defer srv.Close()
	if _, err := New(Config{BaseURL: srv.URL, APIKey: "secret-key"}).Ask(
		context.Background(), "s", map[string]Question{"q": Noul("?", "y", "n")}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.HasPrefix(auth, "Bearer ") || !strings.HasSuffix(auth, "secret-key") {
		t.Errorf("Authorization header = %q", auth)
	}
}
