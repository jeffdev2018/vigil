// Package twentytest is a fake Twenty instance for tests: enough of the REST
// and GraphQL surface for the integration to connect, register a webhook,
// list members and file a task. It records what it received and can sign a
// webhook delivery the way Twenty does.
package twentytest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Member is one Twenty workspace member the fake reports.
type Member struct {
	ID, Email, FirstName, LastName string
}

// Webhook is one subscription the fake holds.
type Webhook struct {
	ID         string
	TargetURL  string
	Operations []string
	Secret     string
}

// Task is one task filed on the fake, with its target when linked.
type Task struct {
	ID, Title, Body, TargetField, TargetID string
}

// Server is the fake. Zero value fields are filled by New.
type Server struct {
	*httptest.Server
	APIKey  string
	Members []Member

	mu       sync.Mutex
	webhooks map[string]Webhook
	tasks    []Task
	seq      int
	// RefuseKey, when set, makes every request answer 401.
	RefuseKey bool
}

// New starts a fake accepting the given API key.
func New(apiKey string, members ...Member) *Server {
	s := &Server{APIKey: apiKey, Members: members, webhooks: map[string]Webhook{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/workspaceMembers", s.handleMembers)
	mux.HandleFunc("/graphql", s.handleGraphQL)
	mux.HandleFunc("/rest/tasks", s.handleTasks)
	mux.HandleFunc("/rest/taskTargets", s.handleTaskTargets)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.RefuseKey || r.Header.Get("Authorization") != "Bearer "+s.APIKey {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return s
}

func (s *Server) next(prefix string) string {
	s.seq++
	return prefix + "-" + strconv.Itoa(s.seq)
}

// Webhooks returns the live subscriptions.
func (s *Server) Webhooks() []Webhook {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Webhook, 0, len(s.webhooks))
	for _, h := range s.webhooks {
		out = append(out, h)
	}
	return out
}

// Tasks returns the tasks filed so far.
func (s *Server) Tasks() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Task(nil), s.tasks...)
}

// Sign produces the headers Twenty sends with a delivery signed by secret.
func Sign(secret string, body []byte, at time.Time) http.Header {
	ts := strconv.FormatInt(at.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + ":"))
	mac.Write(body)
	h := http.Header{}
	h.Set("X-Twenty-Webhook-Timestamp", ts)
	h.Set("X-Twenty-Webhook-Signature", hex.EncodeToString(mac.Sum(nil)))
	h.Set("X-Twenty-Webhook-Nonce", "nonce-"+ts)
	h.Set("Content-Type", "application/json")
	return h
}

// Event builds a Twenty webhook payload.
func Event(name, object string, record map[string]any, updated ...string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"eventName": name, "eventDate": time.Now().UTC().Format(time.RFC3339), "workspaceId": "ws-twenty",
		"objectMetadata": map[string]any{"id": "meta-" + object, "nameSingular": object},
		"record":         record, "updatedFields": updated,
	})
	return raw
}

func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	rows := make([]map[string]any, 0, len(s.Members))
	for _, m := range s.Members {
		rows = append(rows, map[string]any{"id": m.ID, "userEmail": m.Email, "name": map[string]any{"firstName": m.FirstName, "lastName": m.LastName}})
	}
	json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"workspaceMembers": rows}})
}

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case strings.Contains(req.Query, "createWebhook"):
		input, _ := req.Variables["input"].(map[string]any)
		ops := []string{}
		if list, ok := input["operations"].([]any); ok {
			for _, o := range list {
				ops = append(ops, o.(string))
			}
		}
		hook := Webhook{ID: s.next("wh"), TargetURL: input["targetUrl"].(string), Operations: ops, Secret: s.next("secret")}
		s.webhooks[hook.ID] = hook
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"createWebhook": map[string]any{
			"id": hook.ID, "targetUrl": hook.TargetURL, "operations": hook.Operations, "secret": hook.Secret,
		}}})
	case strings.Contains(req.Query, "deleteWebhook"):
		id, _ := req.Variables["id"].(string)
		if _, ok := s.webhooks[id]; !ok {
			json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"message": "Webhook not found"}}})
			return
		}
		delete(s.webhooks, id)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"deleteWebhook": map[string]any{"id": id}}})
	default:
		json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"message": "unknown operation"}}})
	}
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title  string `json:"title"`
		BodyV2 struct {
			Markdown string `json:"markdown"`
		} `json:"bodyV2"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task := Task{ID: s.next("task"), Title: body.Title, Body: body.BodyV2.Markdown}
	s.tasks = append(s.tasks, task)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"createTask": map[string]any{"id": task.ID}}})
}

func (s *Server) handleTaskTargets(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	taskID, _ := body["taskId"].(string)
	for i := range s.tasks {
		if s.tasks[i].ID != taskID {
			continue
		}
		for k, v := range body {
			if strings.HasPrefix(k, "target") {
				s.tasks[i].TargetField, s.tasks[i].TargetID = k, v.(string)
			}
		}
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"createTaskTarget": map[string]any{"id": s.next("tt")}}})
}
