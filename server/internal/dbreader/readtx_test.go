package dbreader

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeTx satisfies pgx.Tx for the two methods ReadTx itself calls. The
// embedded nil interface panics on anything else, which is the point: a test
// that starts depending on a third method should say so.
type fakeTx struct {
	pgx.Tx
	committed  bool
	rolledBack bool
}

func (f *fakeTx) Commit(context.Context) error   { f.committed = true; return nil }
func (f *fakeTx) Rollback(context.Context) error { f.rolledBack = true; return nil }

type fakeTxStarter struct {
	label  string
	begins int
	txs    []*fakeTx
}

func (s *fakeTxStarter) Begin(context.Context) (pgx.Tx, error) {
	s.begins++
	tx := &fakeTx{}
	s.txs = append(s.txs, tx)
	return tx, nil
}

func txSelector(replicaAvailable bool) (*Selector, *fakeTxStarter, *fakeTxStarter, *recorderStub) {
	recorder := &recorderStub{}
	selector := newSelector(
		&db.Queries{},
		&db.Queries{},
		recorder,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	primary := &fakeTxStarter{label: "primary"}
	var replica *fakeTxStarter
	if replicaAvailable {
		replica = &fakeTxStarter{label: "replica"}
		selector.SetTxStarters(primary, replica)
	} else {
		selector.SetTxStarters(primary, nil)
	}
	return selector, primary, replica, recorder
}

// The deployment shape almost every self-hosted install has: one database. An
// eventual-consistency read must still work, on primary, without the caller
// knowing whether a replica exists.
func TestReadTxWithoutAReplicaUsesPrimary(t *testing.T) {
	selector, primary, _, recorder := txSelector(false)

	if err := ReadTx(context.Background(), selector, BusinessInsight, EventualConsistency,
		func(context.Context, pgx.Tx) error { return nil }); err != nil {
		t.Fatalf("ReadTx: %v", err)
	}
	if primary.begins != 1 {
		t.Fatalf("primary begins = %d, want 1", primary.begins)
	}
	if len(recorder.routes) != 1 || recorder.routes[0].role != RolePrimary {
		t.Fatalf("routes = %#v, want one primary route", recorder.routes)
	}
	if recorder.routes[0].reason != ReasonReplicaDisabled {
		t.Errorf("reason = %q, want %q", recorder.routes[0].reason, ReasonReplicaDisabled)
	}
	if !primary.txs[0].committed {
		t.Error("the transaction was never committed")
	}
}

func TestReadTxUsesTheReplicaWhenConfigured(t *testing.T) {
	selector, primary, replica, _ := txSelector(true)

	if err := ReadTx(context.Background(), selector, BusinessInsight, EventualConsistency,
		func(context.Context, pgx.Tx) error { return nil }); err != nil {
		t.Fatalf("ReadTx: %v", err)
	}
	if replica.begins != 1 || primary.begins != 0 {
		t.Fatalf("begins: primary=%d replica=%d, want 0/1", primary.begins, replica.begins)
	}
}

// The fallback the feature depends on: a replica that cannot answer must not
// turn into a failed question. The same transaction body runs again on primary.
func TestReadTxFallsBackToPrimaryOnReplicaConnectionFailure(t *testing.T) {
	selector, primary, replica, recorder := txSelector(true)

	calls := 0
	err := ReadTx(context.Background(), selector, BusinessInsight, EventualConsistency,
		func(context.Context, pgx.Tx) error {
			calls++
			if calls == 1 {
				return &pgconn.PgError{Code: "57P01", Message: "admin shutdown"}
			}
			return nil
		})
	if err != nil {
		t.Fatalf("ReadTx: %v", err)
	}
	if calls != 2 {
		t.Fatalf("callback ran %d times, want 2 (replica then primary)", calls)
	}
	if replica.begins != 1 || primary.begins != 1 {
		t.Fatalf("begins: primary=%d replica=%d, want 1/1", primary.begins, replica.begins)
	}
	if !replica.txs[0].rolledBack {
		t.Error("the failed replica transaction was not rolled back")
	}
	if len(recorder.routes) != 2 || recorder.routes[1].role != RolePrimary {
		t.Fatalf("routes = %#v, want a replica route then a primary one", recorder.routes)
	}
}

// An application error is the caller's, not the replica's: it must surface
// unchanged rather than being retried on primary.
func TestReadTxDoesNotRetryApplicationErrors(t *testing.T) {
	selector, primary, replica, _ := txSelector(true)

	boom := errors.New("scan failed")
	calls := 0
	err := ReadTx(context.Background(), selector, BusinessInsight, EventualConsistency,
		func(context.Context, pgx.Tx) error { calls++; return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the application error", err)
	}
	if calls != 1 || primary.begins != 0 || replica.begins != 1 {
		t.Fatalf("calls=%d primary=%d replica=%d, want one replica attempt", calls, primary.begins, replica.begins)
	}
}

func TestReadTxWithoutStartersIsAWiringError(t *testing.T) {
	selector := NewPrimaryOnly(&db.Queries{})
	err := ReadTx(context.Background(), selector, BusinessInsight, EventualConsistency,
		func(context.Context, pgx.Tx) error { return nil })
	if !errors.Is(err, ErrNoTxStarter) {
		t.Fatalf("err = %v, want ErrNoTxStarter", err)
	}
}
