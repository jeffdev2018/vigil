package handler

// The chat-bot entry point into the capture inbox. `/capture` in a connected
// channel goes through the same code the HTTP endpoint uses — validation,
// audit, realtime event, and the asynchronous suggestion — so a capture typed
// in Lark is indistinguishable from one made in the app apart from its
// origin. The channel engine calls this through its own narrow interface and
// never learns about this package.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var _ engine.CaptureCreator = (*Handler)(nil)

// CreateChannelCapture files a capture from a chat command. creatorUserID is
// the member the channel binding maps the sender to; the engine refuses the
// message before this point when it cannot map one, so an unattributed
// capture is not a case that reaches here.
func (h *Handler) CreateChannelCapture(ctx context.Context, workspaceID, creatorUserID pgtype.UUID, content, rawURL string) (pgtype.UUID, error) {
	content = strings.TrimSpace(util.SanitizeTextForPostgres(content))
	rawURL, ok := validateBrainCaptureURL(rawURL)
	if !ok {
		return pgtype.UUID{}, errors.New("url must be an http(s) URL")
	}
	if content == "" && rawURL == "" {
		return pgtype.UUID{}, errors.New("content or url is required")
	}
	if utf8.RuneCountInString(content) > brainCaptureMaxContentRunes {
		return pgtype.UUID{}, fmt.Errorf("content is limited to %d characters", brainCaptureMaxContentRunes)
	}
	if !creatorUserID.Valid {
		return pgtype.UUID{}, errors.New("capture has no author")
	}
	capture, err := h.Queries.CreateBrainCapture(ctx, db.CreateBrainCaptureParams{
		ID:                  dbid.NewV7(),
		WorkspaceID:         workspaceID,
		Kind:                inferBrainCaptureKind("", content, rawURL),
		Content:             content,
		Url:                 rawURL,
		Origin:              "channel",
		TranscriptionStatus: "none",
		CreatedByType:       "member",
		CreatedByID:         creatorUserID,
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create capture: %w", err)
	}
	h.afterBrainCapture(ctx, capture, "member", uuidToString(creatorUserID))
	return capture.ID, nil
}
