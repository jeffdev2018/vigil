package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// A /capture typed in a connected chat lands in the same inbox as one made in
// the app, stamped with the channel as its origin and the mapped member as
// its author — the whole point of routing the chat command through the API's
// own path instead of a second INSERT.
func TestCreateChannelCaptureFilesARawCapture(t *testing.T) {
	ctx := context.Background()
	workspaceID := brainWorkspace(t)
	wsUUID := parseUUID(workspaceID)

	id, err := testHandler.CreateChannelCapture(ctx, wsUUID, parseUUID(testUserID), "pgbouncer listens on 6432", "")
	if err != nil {
		t.Fatalf("CreateChannelCapture: %v", err)
	}
	dbfx.Cleanup(t, `DELETE FROM brain_capture WHERE id = $1`, util.UUIDToString(id))

	items, rawCount := listCaptures(t, workspaceID, "raw")
	if rawCount != 1 || len(items) != 1 {
		t.Fatalf("inbox has %d raw capture(s) (%d listed), want 1", rawCount, len(items))
	}
	got := items[0]
	if got.Origin != "channel" {
		t.Errorf("origin = %q, want channel", got.Origin)
	}
	if got.Status != "raw" {
		t.Errorf("status = %q, want raw: a chat capture files nothing", got.Status)
	}
	if got.CreatedByType != "member" || got.CreatedByID == nil || *got.CreatedByID != testUserID {
		t.Errorf("author = (%q, %v), want the mapped member", got.CreatedByType, got.CreatedByID)
	}
	// A bare line of prose is text; the kind decides how the capture renders.
	if got.Kind != "text" {
		t.Errorf("kind = %q, want text", got.Kind)
	}
}

func TestCreateChannelCaptureInfersALinkAndRefusesJunk(t *testing.T) {
	ctx := context.Background()
	workspaceID := brainWorkspace(t)
	wsUUID := parseUUID(workspaceID)
	userUUID := parseUUID(testUserID)

	id, err := testHandler.CreateChannelCapture(ctx, wsUUID, userUUID, "", "https://example.com/locking")
	if err != nil {
		t.Fatalf("CreateChannelCapture: %v", err)
	}
	dbfx.Cleanup(t, `DELETE FROM brain_capture WHERE id = $1`, util.UUIDToString(id))
	items, _ := listCaptures(t, workspaceID, "raw")
	if len(items) != 1 || items[0].Kind != "link" {
		t.Fatalf("kind = %v, want a link for a bare url", items)
	}

	// The chat is an untrusted boundary like any other: the same validation
	// the HTTP endpoint applies runs here, so a bad command cannot write a
	// row the app would refuse.
	for _, tc := range []struct {
		name, content, url string
	}{
		{name: "nothing at all"},
		{name: "non-http url", url: "file:///etc/passwd"},
		{name: "oversized content", content: strings.Repeat("a", brainCaptureMaxContentRunes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := testHandler.CreateChannelCapture(ctx, wsUUID, userUUID, tc.content, tc.url); err == nil {
				t.Fatal("CreateChannelCapture accepted it")
			}
		})
	}

	// An unattributed capture would leave the inbox with a row nobody can
	// ask about. The engine maps the sender before it gets here, so this is
	// a wiring mistake — it fails loudly instead of writing the row.
	if _, err := testHandler.CreateChannelCapture(ctx, wsUUID, pgtype.UUID{}, "x", ""); err == nil {
		t.Error("CreateChannelCapture accepted a capture with no author")
	}
}
