package handler

// The native runtime's doctrine tool (workspace doctrine, chantier 22).
//
// report_doctrine_conflict replays CreateDoctrineReport in-process, the way
// the calendar tools replay their handlers, so a run and a person file the
// same row, notify the same owners and land in the same review queue.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type doctrineToolAdapter struct{ h *Handler }

// Report files the run's doctrine report and returns its id. The actor
// headers are the run-token set the API already trusts, so the reporter is
// the agent and the report binds to this task (and, through it, its issue).
func (a doctrineToolAdapter) Report(ctx context.Context, task db.AgentTaskQueue, agent db.Agent, kind, summary, passage string) (string, error) {
	body := map[string]any{"kind": kind, "summary": summary}
	if strings.TrimSpace(passage) != "" {
		body["passage"] = passage
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workspace/doctrine/reports", strings.NewReader(string(raw))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", uuidToString(agent.WorkspaceID))
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", uuidToString(agent.ID))
	req.Header.Set("X-Task-ID", uuidToString(task.ID))
	req.Header.Set("X-User-ID", a.h.workspaceOwnerUserID(ctx, agent.WorkspaceID))

	rec := httptest.NewRecorder()
	a.h.CreateDoctrineReport(rec, req)

	var out struct {
		Report DoctrineReportResponse `json:"report"`
		Error  string                 `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		return "", fmt.Errorf("doctrine report: HTTP %d", rec.Code)
	}
	if rec.Code >= 400 {
		if out.Error != "" {
			return "", errors.New(out.Error)
		}
		return "", fmt.Errorf("doctrine report: HTTP %d", rec.Code)
	}
	if out.Report.ID == "" {
		return "", errors.New("doctrine report: the server filed no report")
	}
	return out.Report.ID, nil
}
