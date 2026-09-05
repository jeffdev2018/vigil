package handler

// The per-channel triage policy: one source per installed channel, keyed on
// the installation, defaulting to direct so a workspace that never configures
// the queue keeps today's behavior.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func channelAdmission(t *testing.T, installationID string, title string) engine.ChannelIssueAdmission {
	t.Helper()
	return engine.ChannelIssueAdmission{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: parseUUID(installationID),
		ChannelType:    "slack",
		OriginType:     "slack_chat",
		OriginID:       dbid.NewV7(),
		CreatorUserID:  parseUUID(testUserID),
		Title:          title,
		Description:    "reported in #support",
	}
}

func newChannelInstallationID(t *testing.T) string {
	t.Helper()
	id := dbid.NewV7()
	installationID := uuidToString(id)
	cleanupTriageSourceKind(t, triage.SourceChannel, installationID)
	return installationID
}

func TestChannelIssueIsAdmittedAndMeasuredByDefault(t *testing.T) {
	installationID := newChannelInstallationID(t)

	got := testHandler.AdmitChannelIssue(context.Background(), channelAdmission(t, installationID, "Login is broken"))
	if got != engine.TriageAdmit {
		t.Fatalf("decision = %q, want admit — an unconfigured channel must keep creating issues", got)
	}
	items := triageItemsForSource(t, triage.SourceChannel, installationID)
	if len(items) != 1 || !items[0].Shadow {
		t.Fatalf("items = %+v, want one shadow measurement row", items)
	}
}

func TestChannelIssueIsHeldWhenTheSourceIsGated(t *testing.T) {
	installationID := newChannelInstallationID(t)
	// The source is created by the first admission, then gated.
	testHandler.AdmitChannelIssue(context.Background(), channelAdmission(t, installationID, "seed"))
	setTriageSourceModeForKind(t, triage.SourceChannel, installationID, string(triage.ModeGate))

	got := testHandler.AdmitChannelIssue(context.Background(), channelAdmission(t, installationID, "Login is broken"))
	if got != engine.TriageHeld {
		t.Fatalf("decision = %q, want held", got)
	}
	items := triageItemsForSource(t, triage.SourceChannel, installationID)
	var pending int
	for _, item := range items {
		if item.State == triage.StatePending && !item.Shadow {
			pending++
			if item.Title != "Login is broken" {
				t.Fatalf("queued title = %q, want the command title", item.Title)
			}
		}
	}
	if pending != 1 {
		t.Fatalf("pending queue rows = %d, want 1 (items: %+v)", pending, items)
	}
}

func TestChannelIssueIsRefusedWhenTheSourceIsBlocked(t *testing.T) {
	installationID := newChannelInstallationID(t)
	testHandler.AdmitChannelIssue(context.Background(), channelAdmission(t, installationID, "seed"))
	setTriageSourceModeForKind(t, triage.SourceChannel, installationID, string(triage.ModeBlocked))

	got := testHandler.AdmitChannelIssue(context.Background(), channelAdmission(t, installationID, "Login is broken"))
	if got != engine.TriageRefused {
		t.Fatalf("decision = %q, want refused", got)
	}
	var dropped int
	for _, item := range triageItemsForSource(t, triage.SourceChannel, installationID) {
		if item.State == triage.StateDropped {
			dropped++
		}
	}
	if dropped != 1 {
		t.Fatalf("dropped audit rows = %d, want 1", dropped)
	}
}

func TestChannelAdmissionWithoutAnInstallationAdmits(t *testing.T) {
	in := channelAdmission(t, uuidToString(dbid.NewV7()), "no installation")
	in.InstallationID = pgtype.UUID{}
	if got := testHandler.AdmitChannelIssue(context.Background(), in); got != engine.TriageAdmit {
		t.Fatalf("decision = %q, want admit — an unkeyable source must never hold work", got)
	}
}
