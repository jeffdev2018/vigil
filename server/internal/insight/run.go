package insight

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dbreader"
)

// StatementTimeout bounds every insight read at the Postgres level. An insight
// is an interactive aggregation over one workspace: the user is watching a
// spinner, so a query that has not answered in this budget is not going to be
// useful when it does. Enforced with SET LOCAL inside the read transaction,
// exactly as /search does, so nothing leaks to the pooled connection.
const StatementTimeout = 8 * time.Second

// Outcomes recorded in insight_query_log. They are the evidence for whether
// the DSL is wide enough, so `invalid` (the model produced a document the
// compiler rejected) is deliberately distinct from `refused` (no model, or the
// model gave up).
const (
	OutcomeOK      = "ok"
	OutcomeInvalid = "invalid"
	OutcomeTimeout = "timeout"
	OutcomeRefused = "refused"
)

// ErrTimeout is returned when the database cancelled the statement at the
// transaction-local timeout. The handler maps it to 503 — the question may
// well be answerable, just not this cheaply — and logs a `timeout` row.
var ErrTimeout = errors.New("insight query timed out")

// Value is one cell. The API contract is deliberately narrow: a string label,
// a number, or null. Anything else would force the frontend schema to widen.
type Value any

// Row is one result row keyed by Compiled.Columns.
type Row map[string]Value

// Result is what both /api/insights/ask and /api/insights/run return.
type Result struct {
	Rows       []Row    `json:"rows"`
	Shape      string   `json:"shape"`
	Warnings   []string `json:"warnings"`
	DurationMS int      `json:"duration_ms"`
}

// Run compiles and executes one document. It is the only execution path: a
// document that fails Compile never reaches the database, because Compile
// returns before a transaction is opened.
//
// The read is routed through dbreader with EventualConsistency, so it uses a
// replica when one is configured and healthy and falls back to primary
// otherwise. An insight is a rollup of the recent past; a few seconds of
// replication lag cannot change what it says, which is exactly the property
// that makes it safe to move off the primary.
func Run(
	ctx context.Context,
	selector *dbreader.Selector,
	q *Query,
	workspaceID pgtype.UUID,
) (Result, *Error, error) {
	compiled, verr := Compile(q, workspaceID)
	if verr != nil {
		return Result{}, verr, nil
	}

	started := time.Now()
	var rows []Row
	err := dbreader.ReadTx(
		ctx,
		selector,
		dbreader.BusinessInsight,
		dbreader.EventualConsistency,
		func(ctx context.Context, tx pgx.Tx) error {
			var scanErr error
			rows, scanErr = execCompiled(ctx, tx, compiled)
			return scanErr
		},
	)
	if err != nil {
		if isStatementTimeout(err) {
			return Result{}, nil, ErrTimeout
		}
		return Result{}, nil, err
	}

	return Result{
		Rows:       rows,
		Shape:      q.Shape(),
		Warnings:   compiled.Warnings,
		DurationMS: int(time.Since(started).Milliseconds()),
	}, nil, nil
}

// execCompiled applies the transaction-local guards and scans the result.
// Both settings are SET LOCAL, so pgxpool hands the connection back with the
// database defaults intact.
func execCompiled(ctx context.Context, tx pgx.Tx, compiled Compiled) ([]Row, error) {
	timeoutMs := int(StatementTimeout / time.Millisecond)
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return nil, fmt.Errorf("set insight statement_timeout: %w", err)
	}
	// Belt and braces: the compiler emits SELECT and nothing else, but a
	// read-only transaction means a future compiler bug cannot write.
	if _, err := tx.Exec(ctx, "SET LOCAL transaction_read_only = on"); err != nil {
		return nil, fmt.Errorf("set insight transaction_read_only: %w", err)
	}

	pgRows, err := tx.Query(ctx, compiled.SQL, compiled.Args...)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()

	out := make([]Row, 0, DefaultLimit)
	for pgRows.Next() {
		values, err := pgRows.Values()
		if err != nil {
			return nil, err
		}
		row := make(Row, len(compiled.Columns))
		for i, name := range compiled.Columns {
			if i < len(values) {
				row[name] = normalize(values[i])
			}
		}
		out = append(out, row)
	}
	if err := pgRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// normalize flattens a driver value into the string | number | null contract.
// Anything the driver hands back that is not already one of those becomes its
// text form, so a future column type cannot break the response schema.
func normalize(value any) Value {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return v
	case float64:
		return v
	case float32:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case pgtype.Numeric:
		f, err := v.Float64Value()
		if err != nil || !f.Valid {
			return nil
		}
		return f.Float64
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprint(v)
	}
}

func isStatementTimeout(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "57014"
	}
	return false
}
