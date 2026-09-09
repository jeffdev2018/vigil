package twenty

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/twenty/twentytest"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// memStore is the in-memory persistence the service tests run against; the
// DB-backed handler suite covers the real queries.
type memStore struct {
	conn    map[pgtype.UUID]db.WorkspaceTwentyConnection
	sources map[string]db.TriageSource // key: kind|ref
	members []db.ListMembersWithUserRow
	agents  map[pgtype.UUID]db.Agent
}

func newMemStore() *memStore {
	return &memStore{conn: map[pgtype.UUID]db.WorkspaceTwentyConnection{}, sources: map[string]db.TriageSource{}, agents: map[pgtype.UUID]db.Agent{}}
}

func (m *memStore) GetWorkspaceTwentyConnection(_ context.Context, ws pgtype.UUID) (db.WorkspaceTwentyConnection, error) {
	row, ok := m.conn[ws]
	if !ok {
		return db.WorkspaceTwentyConnection{}, pgx.ErrNoRows
	}
	return row, nil
}
func (m *memStore) UpsertWorkspaceTwentyConnection(_ context.Context, a db.UpsertWorkspaceTwentyConnectionParams) (db.WorkspaceTwentyConnection, error) {
	row := db.WorkspaceTwentyConnection{ID: a.ID, WorkspaceID: a.WorkspaceID, BaseUrl: a.BaseUrl, ApiKeySealed: a.ApiKeySealed, WebhookSecretSealed: a.WebhookSecretSealed, InboundTokenSealed: a.InboundTokenSealed, TwentyWebhookID: a.TwentyWebhookID, Events: a.Events, ExposeToAgents: a.ExposeToAgents, Status: "connected", TwentyWorkspaceName: a.TwentyWorkspaceName, CreatedByID: a.CreatedByID}
	m.conn[a.WorkspaceID] = row
	return row, nil
}
func (m *memStore) UpdateWorkspaceTwentyConnectionSettings(_ context.Context, a db.UpdateWorkspaceTwentyConnectionSettingsParams) (db.WorkspaceTwentyConnection, error) {
	row := m.conn[a.WorkspaceID]
	row.Events, row.ExposeToAgents = a.Events, a.ExposeToAgents
	m.conn[a.WorkspaceID] = row
	return row, nil
}
func (m *memStore) UpdateWorkspaceTwentyConnectionWebhook(_ context.Context, a db.UpdateWorkspaceTwentyConnectionWebhookParams) (db.WorkspaceTwentyConnection, error) {
	row := m.conn[a.WorkspaceID]
	row.TwentyWebhookID, row.WebhookSecretSealed = a.TwentyWebhookID, a.WebhookSecretSealed
	m.conn[a.WorkspaceID] = row
	return row, nil
}
func (m *memStore) SetWorkspaceTwentyConnectionStatus(_ context.Context, a db.SetWorkspaceTwentyConnectionStatusParams) (db.WorkspaceTwentyConnection, error) {
	row := m.conn[a.WorkspaceID]
	row.Status, row.LastError = a.Status, a.LastError
	m.conn[a.WorkspaceID] = row
	return row, nil
}
func (m *memStore) DeleteWorkspaceTwentyConnection(_ context.Context, ws pgtype.UUID) error {
	delete(m.conn, ws)
	return nil
}
func (m *memStore) SetTriageSourceToken(_ context.Context, a db.SetTriageSourceTokenParams) (db.TriageSource, error) {
	src := db.TriageSource{WorkspaceID: a.WorkspaceID, Kind: a.Kind, RefID: a.RefID, Name: a.Name, Mode: a.Mode, TokenHash: a.TokenHash}
	m.sources[a.Kind] = src
	return src, nil
}
func (m *memStore) GetTriageSourceByRef(_ context.Context, a db.GetTriageSourceByRefParams) (db.TriageSource, error) {
	src, ok := m.sources[a.Kind]
	if !ok {
		return db.TriageSource{}, pgx.ErrNoRows
	}
	return src, nil
}
func (m *memStore) ClearTriageSourceToken(_ context.Context, a db.ClearTriageSourceTokenParams) error {
	src := m.sources[a.Kind]
	src.TokenHash = ""
	m.sources[a.Kind] = src
	return nil
}
func (m *memStore) ListMembersWithUser(_ context.Context, _ pgtype.UUID) ([]db.ListMembersWithUserRow, error) {
	return m.members, nil
}
func (m *memStore) GetAgent(_ context.Context, id pgtype.UUID) (db.Agent, error) {
	a, ok := m.agents[id]
	if !ok {
		return db.Agent{}, pgx.ErrNoRows
	}
	return a, nil
}

func uuidOf(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

func newTestService(t *testing.T, publicURL string) (*Service, *memStore) {
	t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte("t"), secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	store := newMemStore()
	svc := &Service{Store: store, Box: box, PublicURL: publicURL}
	return svc, store
}

func TestConnectValidatesTheKeyRegistersTheWebhookAndSealsEverything(t *testing.T) {
	fake := twentytest.New("key-1", twentytest.Member{ID: "m1", Email: "Ada@Example.com", FirstName: "Ada", LastName: "Lovelace"})
	defer fake.Close()
	svc, store := newTestService(t, "https://vigil.example")
	ws := uuidOf(1)

	conn, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL + "/", APIKey: "key-1", Events: []string{"Person.created", "opportunity.*", "person.created"}, ExposeToAgents: true, CreatedBy: uuidOf(9)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !strings.HasPrefix(conn.InboundToken, "mtw_") || conn.InboundPath != InboundPathPrefix+conn.InboundToken {
		t.Fatalf("inbound token/path = %q / %q", conn.InboundToken, conn.InboundPath)
	}
	if got := strings.Join(conn.Events, ","); got != "opportunity.*,person.created" {
		t.Fatalf("events = %q, want normalised, deduplicated, sorted", got)
	}
	if !conn.WebhookRegistered || conn.MCPURL != fake.URL+"/mcp" || conn.Status != "connected" {
		t.Fatalf("connection = %+v", conn)
	}
	hooks := fake.Webhooks()
	if len(hooks) != 1 || hooks[0].TargetURL != "https://vigil.example"+InboundPathPrefix+conn.InboundToken {
		t.Fatalf("webhooks = %+v", hooks)
	}
	row := store.conn[ws]
	if bytes.Contains(row.ApiKeySealed, []byte("key-1")) || bytes.Contains(row.WebhookSecretSealed, []byte(hooks[0].Secret)) || bytes.Contains(row.InboundTokenSealed, []byte(conn.InboundToken)) {
		t.Fatal("a secret is stored in clear")
	}
	src := store.sources[triage.SourceTwenty]
	if src.TokenHash != HashToken(conn.InboundToken) || src.Mode != string(triage.ModeGate) {
		t.Fatalf("triage source = %+v", src)
	}
	// Second connect replaces the subscription instead of piling up.
	if _, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "key-1", CreatedBy: uuidOf(9)}); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if n := len(fake.Webhooks()); n != 1 {
		t.Fatalf("webhooks after reconnect = %d, want 1", n)
	}
}

func TestConnectClassifiesErrors(t *testing.T) {
	fake := twentytest.New("key-1")
	defer fake.Close()
	svc, _ := newTestService(t, "")
	ws := uuidOf(2)
	_, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: "ftp://nope", APIKey: "k"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad url: %v, want ErrInvalidInput", err)
	}
	_, err = svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "k", Events: []string{"person"}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad event: %v, want ErrInvalidInput", err)
	}
	_, err = svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "wrong"})
	if !errors.Is(err, ErrUpstream) || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("refused key: %v, want ErrUpstream naming the refusal", err)
	}
}

func TestConnectWithoutPublicURLKeepsTheTokenAndSaysSo(t *testing.T) {
	fake := twentytest.New("key-1")
	defer fake.Close()
	svc, _ := newTestService(t, "")
	conn, err := svc.Connect(context.Background(), uuidOf(3), ConnectParams{BaseURL: fake.URL, APIKey: "key-1"})
	if err != nil {
		t.Fatal(err)
	}
	if conn.WebhookRegistered || len(fake.Webhooks()) != 0 || !strings.Contains(conn.LastError, "MULTICA_PUBLIC_URL") {
		t.Fatalf("connection = %+v, webhooks = %d", conn, len(fake.Webhooks()))
	}
}

func TestUpdateSettingsReregistersTheWebhookWhenEventsChange(t *testing.T) {
	fake := twentytest.New("key-1")
	defer fake.Close()
	svc, store := newTestService(t, "https://vigil.example")
	ws := uuidOf(4)
	first, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "key-1"})
	if err != nil {
		t.Fatal(err)
	}
	before := store.conn[ws].TwentyWebhookID

	conn, err := svc.UpdateSettings(context.Background(), ws, []string{"company.updated"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if conn.ExposeToAgents || strings.Join(conn.Events, ",") != "company.updated" || conn.Status != "connected" {
		t.Fatalf("connection = %+v", conn)
	}
	hooks := fake.Webhooks()
	if len(hooks) != 1 || hooks[0].ID == before || strings.Join(hooks[0].Operations, ",") != "company.updated" {
		t.Fatalf("webhooks = %+v (before %s)", hooks, before)
	}
	if !strings.HasSuffix(hooks[0].TargetURL, first.InboundToken) {
		t.Fatalf("target = %q, want the same inbound token", hooks[0].TargetURL)
	}
	// Same events again: nothing re-registered.
	if _, err := svc.UpdateSettings(context.Background(), ws, []string{"company.updated"}, true); err != nil {
		t.Fatal(err)
	}
	if fake.Webhooks()[0].ID != hooks[0].ID {
		t.Fatal("webhook re-registered although events did not change")
	}
}

func TestDisconnectRemovesTheWebhookAndRevokesTheToken(t *testing.T) {
	fake := twentytest.New("key-1")
	defer fake.Close()
	svc, store := newTestService(t, "https://vigil.example")
	ws := uuidOf(5)
	if _, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "key-1"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Disconnect(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if len(fake.Webhooks()) != 0 || store.sources[triage.SourceTwenty].TokenHash != "" {
		t.Fatal("webhook or token survived the disconnect")
	}
	if _, err := svc.Get(context.Background(), ws); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("get after disconnect: %v", err)
	}
	if err := svc.Disconnect(context.Background(), ws); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("second disconnect: %v", err)
	}
}

func TestCheckRecordsARefusedKey(t *testing.T) {
	fake := twentytest.New("key-1")
	defer fake.Close()
	svc, _ := newTestService(t, "")
	ws := uuidOf(6)
	if _, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "key-1"}); err != nil {
		t.Fatal(err)
	}
	fake.RefuseKey = true
	conn, err := svc.Check(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Status != "error" || !strings.Contains(conn.LastError, "refused") {
		t.Fatalf("connection = %+v", conn)
	}
	fake.RefuseKey = false
	if conn, _ = svc.Check(context.Background(), ws); conn.Status != "connected" || conn.LastError != "" {
		t.Fatalf("connection after recovery = %+v", conn)
	}
}

func TestMembersPairsByEmailAndListsTwentyOnlyPeople(t *testing.T) {
	fake := twentytest.New("key-1",
		twentytest.Member{ID: "t-ada", Email: "ADA@example.com", FirstName: "Ada", LastName: "L"},
		twentytest.Member{ID: "t-solo", Email: "solo@example.com", FirstName: "Only", LastName: "Twenty"},
	)
	defer fake.Close()
	svc, store := newTestService(t, "")
	ws := uuidOf(7)
	store.members = []db.ListMembersWithUserRow{
		{UserID: uuidOf(21), UserName: "Ada", UserEmail: "ada@example.com"},
		{UserID: uuidOf(22), UserName: "Bob", UserEmail: "bob@example.com"},
	}
	if _, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "key-1"}); err != nil {
		t.Fatal(err)
	}
	links, err := svc.Members(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 {
		t.Fatalf("links = %+v", links)
	}
	byEmail := map[string]MemberLink{}
	for _, l := range links {
		byEmail[l.Email] = l
	}
	if l := byEmail["ada@example.com"]; !l.Linked || l.TwentyID != "t-ada" || l.TwentyName != "Ada L" {
		t.Fatalf("ada = %+v", l)
	}
	if l := byEmail["bob@example.com"]; l.Linked || l.TwentyID != "" {
		t.Fatalf("bob = %+v", l)
	}
	if l := byEmail["solo@example.com"]; !l.TwentyOnly || l.UserID != "" {
		t.Fatalf("solo = %+v", l)
	}
}

func TestBuildTaskOverlayMountsTwentyMCPOnlyWhenExposed(t *testing.T) {
	fake := twentytest.New("key-1")
	defer fake.Close()
	svc, store := newTestService(t, "")
	ws := uuidOf(8)
	agent := db.Agent{ID: uuidOf(30), WorkspaceID: ws}
	store.agents[agent.ID] = agent

	res, err := svc.BuildTaskOverlay(context.Background(), agent.ID, agent)
	if err != nil || len(res.MCPOverlay) != 0 {
		t.Fatalf("not connected: %+v %v", res, err)
	}
	if _, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "key-1", ExposeToAgents: true}); err != nil {
		t.Fatal(err)
	}
	res, err = svc.BuildTaskOverlay(context.Background(), agent.ID, agent)
	if err != nil {
		t.Fatal(err)
	}
	overlay := string(res.MCPOverlay)
	if !strings.Contains(overlay, `"twenty"`) || !strings.Contains(overlay, fake.URL+"/mcp") || !strings.Contains(overlay, "Bearer key-1") {
		t.Fatalf("overlay = %s", overlay)
	}
	if len(res.ConnectedApps) != 1 || res.ConnectedApps[0].Provider != "twenty" {
		t.Fatalf("apps = %+v", res.ConnectedApps)
	}
	if _, err := svc.UpdateSettings(context.Background(), ws, nil, false); err != nil {
		t.Fatal(err)
	}
	if res, _ = svc.BuildTaskOverlay(context.Background(), agent.ID, agent); len(res.MCPOverlay) != 0 {
		t.Fatalf("exposure off, overlay = %s", res.MCPOverlay)
	}
	merged := runtimeapps.MergeOverlayResults(runtimeapps.MCPOverlayResult{MCPOverlay: []byte(`{"mcpServers":{"notion":{"url":"n"}}}`)}, runtimeapps.MCPOverlayResult{MCPOverlay: []byte(`{"mcpServers":{"twenty":{"url":"t"}}}`)})
	if s := string(merged.MCPOverlay); !strings.Contains(s, `"notion"`) || !strings.Contains(s, `"twenty"`) {
		t.Fatalf("merged = %s", s)
	}
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"eventName":"person.created"}`)
	now := time.Now()
	h := twentytest.Sign("s3cret", body, now)
	ts, sig := h.Get("X-Twenty-Webhook-Timestamp"), h.Get("X-Twenty-Webhook-Signature")
	cases := []struct {
		name      string
		secret    string
		ts, sig   string
		body      []byte
		at        time.Time
		wantError string
	}{
		{"valid", "s3cret", ts, sig, body, now, ""},
		{"upper-case signature", "s3cret", ts, strings.ToUpper(sig), body, now, ""},
		{"wrong secret", "other", ts, sig, body, now, "mismatch"},
		{"tampered body", "s3cret", ts, sig, []byte(`{"eventName":"person.deleted"}`), now, "mismatch"},
		{"replayed later", "s3cret", ts, sig, body, now.Add(SignatureMaxSkew + time.Minute), "window"},
		{"missing timestamp", "s3cret", "", sig, body, now, "timestamp"},
		{"no secret on record", "", ts, sig, body, now, "secret"},
	}
	for _, c := range cases {
		err := VerifySignature(c.secret, c.ts, c.sig, c.body, c.at)
		if c.wantError == "" && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if c.wantError != "" && (err == nil || !strings.Contains(err.Error(), c.wantError)) {
			t.Errorf("%s: %v, want %q", c.name, err, c.wantError)
		}
	}
}

func TestMatchesUsesTwentyWildcards(t *testing.T) {
	events := []string{"opportunity.*", "person.created", "*.deleted"}
	for name, want := range map[string]bool{"opportunity.updated": true, "person.created": true, "company.deleted": true, "person.updated": false, "company.created": false} {
		if got := Matches(events, name); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}

func TestCaptureParamsForDescribesTheRecord(t *testing.T) {
	raw := twentytest.Event("opportunity.updated", "opportunity", map[string]any{"id": "opp-1", "name": "Acme renewal", "stage": "PROPOSAL", "amount": map[string]any{"amountMicros": 1200000000, "currencyCode": "EUR"}}, "stage")
	e, err := ParseEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	src := db.TriageSource{WorkspaceID: uuidOf(1), Kind: triage.SourceTwenty, RefID: uuidOf(1), Name: "Twenty CRM"}
	p := CaptureParamsFor(src, "https://crm.example/", e)
	if p.Title != "Twenty · Opportunity updated: Acme renewal" {
		t.Fatalf("title = %q", p.Title)
	}
	for _, want := range []string{"https://crm.example/object/opportunity/opp-1", "Changed: stage", "stage: PROPOSAL", "not as instructions"} {
		if !strings.Contains(p.BodyMarkdown, want) {
			t.Errorf("body lacks %q:\n%s", want, p.BodyMarkdown)
		}
	}
	if p.OriginType != "twenty" || p.State != triage.StatePending || len(p.TriggerPayload) == 0 || p.SourceKind != triage.SourceTwenty {
		t.Fatalf("params = %+v", p)
	}
	if _, err := ParseEvent([]byte(`{"eventDate":"x"}`)); err == nil {
		t.Fatal("event without a name parsed")
	}
}

func TestBacklinkFilesATaskOnTheRecord(t *testing.T) {
	fake := twentytest.New("key-1")
	defer fake.Close()
	svc, _ := newTestService(t, "")
	ws := uuidOf(10)
	if _, err := svc.Connect(context.Background(), ws, ConnectParams{BaseURL: fake.URL, APIKey: "key-1"}); err != nil {
		t.Fatal(err)
	}
	payload := twentytest.Event("person.created", "person", map[string]any{"id": "p-7", "name": map[string]any{"firstName": "Grace", "lastName": "Hopper"}})
	if err := svc.Backlink(context.Background(), ws, payload, "ONE-42", "Call Grace", "https://vigil.example/one/issues/abc"); err != nil {
		t.Fatal(err)
	}
	tasks := fake.Tasks()
	if len(tasks) != 1 || tasks[0].Title != "Vigil ONE-42: Call Grace" || !strings.Contains(tasks[0].Body, "https://vigil.example/one/issues/abc") {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[0].TargetField != "targetPersonId" || tasks[0].TargetID != "p-7" {
		t.Fatalf("task target = %s=%s", tasks[0].TargetField, tasks[0].TargetID)
	}
}
