package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outbound.go subscribes the bridge to the in-process event bus, so a comment,
// a status change or a finished run on a mirrored issue reaches Linear without
// every write path having to know Linear exists.
//
// Payloads are read through a JSON round trip rather than a type assertion:
// the publishers put handler-layer response structs on the bus, and importing
// those here would be an import cycle. The round trip costs one small marshal
// per event and keeps this package independent of the handler.

// usageReader is the cost lookup for a finished run. Optional — an outcome
// note without a price is still worth posting.
type usageReader interface {
	GetTaskUsage(ctx context.Context, taskID pgtype.UUID) ([]db.TaskUsage, error)
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
}

// Outbound is the bus half of the bridge.
type Outbound struct {
	bridge *Bridge
	usage  usageReader
	logger *slog.Logger
}

// NewOutbound builds the subscriber. usage may be nil.
func NewOutbound(bridge *Bridge, usage usageReader, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{bridge: bridge, usage: usage, logger: logger}
}

// Register subscribes to the four events the bridge pushes on.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventCommentCreated, o.onComment)
	bus.Subscribe(protocol.EventIssueUpdated, o.onIssueUpdated)
	bus.Subscribe(protocol.EventTaskCompleted, o.onTaskDone)
	bus.Subscribe(protocol.EventTaskFailed, o.onTaskDone)
}

// callCtx bounds one push. Bus delivery is synchronous, so a stalled Linear
// request must never wedge the write that published the event.
func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

type commentEventPayload struct {
	Comment struct {
		ID         string `json:"id"`
		IssueID    string `json:"issue_id"`
		AuthorType string `json:"author_type"`
		Content    string `json:"content"`
		Type       string `json:"type"`
	} `json:"comment"`
}

func (o *Outbound) onComment(e events.Event) {
	var payload commentEventPayload
	if !decodePayload(e.Payload, &payload) {
		return
	}
	c := payload.Comment
	// A `system` comment is the bridge's own mirror of a Linear comment, or an
	// internal notice. Neither belongs back on Linear; PushComment's
	// comment-link guard covers the first, this covers the second.
	if c.IssueID == "" || c.AuthorType == "system" {
		return
	}
	issueID, ok := parseUUID(c.IssueID)
	if !ok {
		return
	}
	commentID, _ := parseUUID(c.ID)
	body := c.Content
	if c.AuthorType == "agent" {
		body = "**Agent** (Multica):\n\n" + body
	}
	ctx, cancel := callCtx()
	defer cancel()
	if err := o.bridge.PushComment(ctx, issueID, commentID, body); err != nil {
		o.logger.WarnContext(ctx, "linear outbound: push comment failed", "error", err, "issue_id", c.IssueID)
	}
}

type issueEventPayload struct {
	StatusChanged bool `json:"status_changed"`
	Issue         struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"issue"`
}

func (o *Outbound) onIssueUpdated(e events.Event) {
	var payload issueEventPayload
	if !decodePayload(e.Payload, &payload) {
		return
	}
	if !payload.StatusChanged || payload.Issue.ID == "" || payload.Issue.Status == "" {
		return
	}
	issueID, ok := parseUUID(payload.Issue.ID)
	if !ok {
		return
	}
	ctx, cancel := callCtx()
	defer cancel()
	if err := o.bridge.PushStatus(ctx, issueID, payload.Issue.Status); err != nil {
		o.logger.WarnContext(ctx, "linear outbound: push status failed", "error", err, "issue_id", payload.Issue.ID)
	}
}

type taskEventPayload struct {
	IssueID      string `json:"issue_id"`
	TaskID       string `json:"task_id"`
	RetryPending bool   `json:"retry_pending"`
}

func (o *Outbound) onTaskDone(e events.Event) {
	var payload taskEventPayload
	if !decodePayload(e.Payload, &payload) {
		return
	}
	// A failure that will be retried is not an outcome yet; reporting it would
	// put a "run failed" note on Linear that the next attempt contradicts.
	if payload.IssueID == "" || payload.RetryPending {
		return
	}
	issueID, ok := parseUUID(payload.IssueID)
	if !ok {
		return
	}
	ctx, cancel := callCtx()
	defer cancel()
	cost := ""
	if taskID, ok := parseUUID(payload.TaskID); ok {
		cost = o.costFor(ctx, taskID)
	}
	if err := o.bridge.PushRunOutcome(ctx, issueID, e.Type == protocol.EventTaskCompleted, cost); err != nil {
		o.logger.WarnContext(ctx, "linear outbound: push run outcome failed", "error", err, "issue_id", payload.IssueID)
	}
}

// costFor sums a task's priced usage rows. An unpriced run reports nothing
// rather than "$0.00", which would be a lie about a run that did cost money.
func (o *Outbound) costFor(ctx context.Context, taskID pgtype.UUID) string {
	if o.usage == nil {
		return ""
	}
	rows, err := o.usage.GetTaskUsage(ctx, taskID)
	if err != nil {
		return ""
	}
	var ticks int64
	priced := false
	for _, row := range rows {
		if row.CostUsdTicks.Valid {
			ticks += row.CostUsdTicks.Int64
			priced = true
		}
	}
	if !priced {
		return ""
	}
	return fmt.Sprintf("$%.4f", float64(ticks)/1e10)
}

// decodePayload copies an arbitrary bus payload into out. Reports false when
// the payload is not the shape this subscriber understands, which is the
// normal case for the many events that share these types.
func decodePayload(payload any, out any) bool {
	if payload == nil {
		return false
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

func parseUUID(s string) (pgtype.UUID, bool) {
	if s == "" {
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return id, true
}
