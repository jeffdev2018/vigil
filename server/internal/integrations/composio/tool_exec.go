package composio

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
)

// ToolExecutor is the deterministic tool path of the SDK (ExecuteTool). It is
// optional on the SDK interface so the fakes that only drive the MCP session
// path keep compiling; *sdk.Client implements it.
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, toolSlug string, req sdk.ExecuteToolRequest) (*sdk.ExecuteToolResponse, error)
}

// ErrNoConnection: the user has no active connection for the toolkit.
var ErrNoConnection = errors.New("composio: no active connection for this toolkit")

// ExecuteToolForUser runs one Composio tool through the user's own active
// connection for the toolkit — the server-side path for fixed flows
// (calendar import/export), never for an agent's free tool use.
func (s *Service) ExecuteToolForUser(ctx context.Context, userID pgtype.UUID, toolkitSlug, toolSlug string, args map[string]any) (map[string]any, error) {
	exec, ok := s.sdk.(ToolExecutor)
	if !ok {
		return nil, errors.New("composio: this SDK cannot execute tools")
	}
	rows, err := s.store.ListActiveUserComposioConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	account := ""
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.ToolkitSlug), toolkitSlug) {
			account = row.ConnectedAccountID
			break
		}
	}
	if account == "" {
		return nil, ErrNoConnection
	}
	resp, err := exec.ExecuteTool(ctx, toolSlug, sdk.ExecuteToolRequest{Arguments: args, ConnectedAccountID: account})
	if err != nil {
		return nil, err
	}
	if !resp.Successful {
		msg := resp.Error
		if msg == "" {
			msg = "the tool reported a failure"
		}
		return nil, fmt.Errorf("composio: %s: %s", toolSlug, msg)
	}
	return resp.Data, nil
}

// HasConnection reports whether the user holds an active connection for the toolkit.
func (s *Service) HasConnection(ctx context.Context, userID pgtype.UUID, toolkitSlug string) bool {
	rows, err := s.store.ListActiveUserComposioConnections(ctx, userID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.ToolkitSlug), toolkitSlug) {
			return true
		}
	}
	return false
}
