package twenty

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util/netguard"
)

// The base URL is typed by a workspace admin and fetched from inside the
// deployment: the production client must refuse a non-public address (the
// cloud metadata endpoint, loopback admin ports, the private network) at
// connect time, and Connect must answer that as the caller's mistake.
func TestTheProductionClientRefusesANonPublicInstance(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Write([]byte(`{"data":{"workspaceMembers":{"edges":[]}}}`))
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListWorkspaceMembers(context.Background()); !errors.Is(err, netguard.ErrAddrBlocked) || reached {
		t.Fatalf("loopback instance was fetched: err=%v reached=%v", err, reached)
	}
	svc, _ := newTestService(t, "")
	svc.HTTP = nil
	if _, err := svc.Connect(context.Background(), uuidOf(1), ConnectParams{BaseURL: "http://169.254.169.254/latest", APIKey: "key"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("metadata base_url: err = %v, want ErrInvalidInput", err)
	}
}

// An instance's error body is not echoed into the error: it reaches the
// requester and the connection's last_error, and the body of whatever the
// URL points at is exactly what an SSRF wants to read back.
func TestAnUpstreamErrorDoesNotCarryTheResponseBody(t *testing.T) {
	const secret = "AKIAINTERNALSECRET"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, secret, http.StatusInternalServerError)
	}))
	defer srv.Close()
	client, err := NewClient(srv.URL, "key", &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListWorkspaceMembers(context.Background())
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("upstream body echoed: %v", err)
	}
}
