package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The shared inbound chain. Every entry point that can produce issue material
// — autopilot webhook and schedule, channels, agent-authored creates,
// quick-create, meetings, inbound email — runs the same three steps: capture
// the item against its source, announce it with `triage:new`, then let the
// source's own policy resolve it (auto_accept) or fall through to the
// workspace rules (K62) and the auto-classifier (K61).
//
// Keeping this in one place is the point: the meeting path had to grow its own
// copy of the announce+rules tail before this existed, and every source that
// skipped a step was silently second class in the queue — no realtime badge,
// no rule, no auto-decision.

// triageSourceRef names the source one inbound item belongs to. RefID is what
// the source's policy is configured per: the autopilot, the channel
// installation, the creating agent, the workspace.
type triageSourceRef struct {
	Kind      string
	RefID     pgtype.UUID
	Name      string
	CreatedBy pgtype.UUID
}

// triageRouteFor upserts the source row and returns its admission decision.
// Fail-open: any error resolves to direct, because a triage-table hiccup must
// never lose inbound work.
func (h *Handler) triageRouteFor(ctx context.Context, workspaceID pgtype.UUID, ref triageSourceRef) triage.Route {
	source, err := h.Queries.UpsertTriageSource(ctx, db.UpsertTriageSourceParams{
		WorkspaceID: workspaceID,
		Kind:        ref.Kind,
		RefID:       ref.RefID,
		Name:        ref.Name,
		CreatedByID: ref.CreatedBy,
	})
	if err != nil {
		slog.Warn("triage source lookup failed, admitting direct",
			"kind", ref.Kind, "ref_id", util.UUIDToString(ref.RefID), "error", err)
		return triage.RouteDirect
	}
	return triage.Decide(source.Mode)
}

// captureTriageInbound records one inbound item and runs the rest of the
// chain on it. It reports the captured item and whether capture succeeded;
// callers treat a false as "capture failed, carry on" — never as a reason to
// drop the delivery.
func (h *Handler) captureTriageInbound(ctx context.Context, p triage.CaptureParams) (db.TriageItem, bool) {
	item, source, err := triage.Capture(ctx, h.Queries, p)
	if err != nil {
		slog.Warn("triage capture failed", "kind", p.SourceKind, "error", err)
		return db.TriageItem{}, false
	}
	// A shadow measurement row and a drop audit row are not in anyone's queue:
	// no badge, no rule, no decision.
	if item.State != triage.StatePending || item.Shadow {
		return item, true
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventTriageNew,
		WorkspaceID: util.UUIDToString(p.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"item_id":   util.UUIDToString(item.ID),
			"source_id": util.UUIDToString(item.SourceID),
		},
	})
	h.onTriageParked(ctx, item, source)
	return item, true
}

// onTriageParked resolves a freshly parked item without a human when the
// source says so, and otherwise hands it to the workspace rules (K62), which
// fall through to the auto-classifier (K61).
//
// auto_accept needs an acceptor identity, because accepting creates an issue
// and an issue has a creator. The source's creator is that identity; a source
// nobody created (upserted by an unattended delivery) cannot auto-accept and
// its items wait for a human, which is the safe direction.
func (h *Handler) onTriageParked(ctx context.Context, item db.TriageItem, source db.TriageSource) {
	if triage.AutoAcceptEnabled(source.AutoAccept) && source.CreatedByID.Valid {
		res := h.acceptTriageItemCore(ctx, item.WorkspaceID, util.UUIDToString(source.CreatedByID), item.ID)
		if res.outcome == "accepted" || res.outcome == "duplicate" {
			return
		}
		slog.Warn("triage auto-accept did not resolve the item, leaving it for a human",
			"outcome", res.outcome, "item_id", util.UUIDToString(item.ID))
		return
	}
	h.ApplyTriageRules(ctx, item)
}

// AdmitChannelIssue implements engine.TriageGate: a `/issue` command typed in
// Slack, Telegram, Lark, DingTalk or WeCom is inbound material like any other
// delivery, so it answers to a triage source of its own — one per installed
// channel, keyed on the installation. Default direct, so nothing changes for a
// workspace that never configures the queue; the issue is still recorded as a
// shadow item so the source shows up in the queue's stats with real volume.
func (h *Handler) AdmitChannelIssue(ctx context.Context, in engine.ChannelIssueAdmission) engine.TriageDecision {
	if !in.WorkspaceID.Valid || !in.InstallationID.Valid {
		return engine.TriageAdmit
	}
	ref := triageSourceRef{
		Kind:      triage.SourceChannel,
		RefID:     in.InstallationID,
		Name:      channelSourceName(in.ChannelType),
		CreatedBy: in.CreatorUserID,
	}
	params := triage.CaptureParams{
		WorkspaceID:     in.WorkspaceID,
		SourceKind:      ref.Kind,
		SourceRefID:     ref.RefID,
		SourceName:      ref.Name,
		SourceCreatedBy: ref.CreatedBy,
		OriginType:      in.OriginType,
		OriginID:        in.OriginID,
		Title:           in.Title,
		BodyMarkdown:    in.Description,
		State:           triage.StatePending,
	}

	switch h.triageRouteFor(ctx, in.WorkspaceID, ref) {
	case triage.RouteQueue:
		if _, ok := h.captureTriageInbound(ctx, params); !ok {
			// Holding must never cost the report: a capture that failed
			// degrades to the ordinary create path.
			return engine.TriageAdmit
		}
		return engine.TriageHeld
	case triage.RouteDrop:
		params.State = triage.StateDropped
		params.DropReason = "source_blocked"
		h.captureTriageInbound(ctx, params)
		return engine.TriageRefused
	}

	// Direct: the issue is created by the caller. Record the shadow
	// measurement so the source has volume in the queue's stats.
	params.Shadow = true
	h.captureTriageInbound(ctx, params)
	return engine.TriageAdmit
}

// channelSourceName labels the source in the queue UI. The platform name is
// all a human needs to recognize it; the installation id is already the ref.
func channelSourceName(channelType string) string {
	if channelType == "" {
		return "Channel"
	}
	return "Channel: " + channelType
}
