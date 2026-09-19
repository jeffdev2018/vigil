package insight

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/util"
)

// This file is the canonical matrix for the DSL vocabulary: one case per
// rejected enum, and one injection attempt per free-text field. The handler
// suite does not replay it — it asserts the wiring (status codes, workspace
// scoping, logging) and points here for the grammar.

func mustCompile(t *testing.T, q *Query) Compiled {
	t.Helper()
	ws, err := util.ParseUUID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("parse workspace uuid: %v", err)
	}
	compiled, verr := Compile(q, ws)
	if verr != nil {
		t.Fatalf("Compile(%+v) = %v, want success", q, verr)
	}
	return compiled
}

func TestValidateRejectsUnknownEnums(t *testing.T) {
	// One row per enum the DSL closes. Each carries a value that is plausible
	// but absent from the vocabulary: these are the documents a model actually
	// produces when it guesses, not adversarial noise (that is the next test).
	cases := []struct {
		name  string
		query Query
		field string
	}{
		{
			name:  "unknown entity",
			query: Query{Entity: "user", Metric: MetricCount},
			field: "entity",
		},
		{
			name:  "entity naming a real table outside the vocabulary",
			query: Query{Entity: "workspace_model_key", Metric: MetricCount},
			field: "entity",
		},
		{
			name:  "unknown metric",
			query: Query{Entity: EntityIssue, Metric: "median"},
			field: "metric",
		},
		{
			name:  "sum over a column that is not in the numeric allowlist",
			query: Query{Entity: EntityIssue, Metric: "sum:title"},
			field: "metric",
		},
		{
			name:  "sum over a numeric column of another entity",
			query: Query{Entity: EntityIssue, Metric: "sum:attempt"},
			field: "metric",
		},
		{
			name:  "p50_cycle_time on an entity with no completion",
			query: Query{Entity: EntityComment, Metric: MetricP50CycleTime},
			field: "metric",
		},
		{
			name:  "unknown dimension",
			query: Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{"creator"}},
			field: "group_by",
		},
		{
			name:  "dimension belonging to another entity",
			query: Query{Entity: EntityComment, Metric: MetricCount, GroupBy: []string{"priority"}},
			field: "group_by",
		},
		{
			name:  "duplicate dimension",
			query: Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{"status", "status"}},
			field: "group_by",
		},
		{
			name:  "too many dimensions",
			query: Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{"status", "priority", "project"}},
			field: "group_by",
		},
		{
			name:  "unknown granularity",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Granularity: "hour"},
			field: "granularity",
		},
		{
			name: "unknown filter field",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "title", Op: OpIs, Values: []string{"x"}},
			}},
			field: "filters[0].field",
		},
		{
			name: "filter field belonging to another entity",
			query: Query{Entity: EntityTask, Metric: MetricCount, Filters: []Filter{
				{Field: "priority", Op: OpIs, Values: []string{"high"}},
			}},
			field: "filters[0].field",
		},
		{
			name: "unknown operator",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "status", Op: "matches", Values: []string{"x"}},
			}},
			field: "filters[0].op",
		},
		{
			name: "range operator on a set field",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "status", Op: OpGt, Values: []string{"5"}},
			}},
			field: "filters[0].op",
		},
		{
			name: "set operator on a numeric field",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "age_days", Op: OpIs, Values: []string{"5"}},
			}},
			field: "filters[0].op",
		},
		{
			name: "presence operator on a numeric field",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "age_days", Op: OpIsEmpty},
			}},
			field: "filters[0].op",
		},
		{
			name: "range operator with a non-numeric value",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "age_days", Op: OpGt, Values: []string{"five"}},
			}},
			field: "filters[0].values",
		},
		{
			name: "set operator with no value",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "status", Op: OpIn},
			}},
			field: "filters[0].values",
		},
		{
			name: "presence operator carrying a value",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "status", Op: OpIsEmpty, Values: []string{"x"}},
			}},
			field: "filters[0].values",
		},
		{
			name: "property on an entity that has none",
			query: Query{Entity: EntityComment, Metric: MetricCount, GroupBy: []string{
				"property:22222222-2222-4222-8222-222222222222",
			}},
			field: "group_by",
		},
		{
			name: "unknown time field",
			query: Query{Entity: EntityIssue, Metric: MetricCount, TimeRange: &TimeRange{
				Field: "closed_at", LastDays: 7,
			}},
			field: "time_range.field",
		},
		{
			name: "time field belonging to another entity",
			query: Query{Entity: EntityComment, Metric: MetricCount, TimeRange: &TimeRange{
				Field: "completed_at", LastDays: 7,
			}},
			field: "time_range.field",
		},
		{
			name: "empty time range",
			query: Query{Entity: EntityIssue, Metric: MetricCount, TimeRange: &TimeRange{
				Field: "created_at",
			}},
			field: "time_range",
		},
		{
			name: "both relative and absolute time range",
			query: Query{Entity: EntityIssue, Metric: MetricCount, TimeRange: &TimeRange{
				LastDays: 7, Start: "2026-01-01T00:00:00Z",
			}},
			field: "time_range",
		},
		{
			name: "inverted absolute range",
			query: Query{Entity: EntityIssue, Metric: MetricCount, TimeRange: &TimeRange{
				Start: "2026-02-01T00:00:00Z", End: "2026-01-01T00:00:00Z",
			}},
			field: "time_range",
		},
		{
			name: "last_days beyond the ceiling",
			query: Query{Entity: EntityIssue, Metric: MetricCount, TimeRange: &TimeRange{
				LastDays: MaxLastDays + 1,
			}},
			field: "time_range.last_days",
		},
		{
			name:  "limit beyond the ceiling",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Limit: MaxLimit + 1},
			field: "limit",
		},
		{
			name:  "negative limit",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Limit: -1},
			field: "limit",
		},
	}

	ws, err := util.ParseUUID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("parse workspace uuid: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verr := tc.query.Validate()
			if verr == nil {
				t.Fatalf("Validate() = nil, want a rejection on %q", tc.field)
			}
			if verr.Field != tc.field {
				t.Errorf("Validate().Field = %q, want %q (reason: %s)", verr.Field, tc.field, verr.Reason)
			}
			// The compiler must reach the same verdict, because it is the
			// only thing standing between a document and the database.
			if _, cerr := Compile(&tc.query, ws); cerr == nil {
				t.Errorf("Compile() accepted a document Validate rejected")
			}
		})
	}
}

// TestInjectionAttemptsNeverReachSQL walks every field the model can fill with
// free text and proves the payload cannot appear in the compiled statement.
// Either the document is rejected, or the value became a placeholder.
func TestInjectionAttemptsNeverReachSQL(t *testing.T) {
	const payload = "x'; DROP TABLE issue; --"

	cases := []struct {
		name  string
		query Query
	}{
		{
			name:  "entity name",
			query: Query{Entity: payload, Metric: MetricCount},
		},
		{
			name:  "metric name",
			query: Query{Entity: EntityIssue, Metric: payload},
		},
		{
			name:  "sum field name",
			query: Query{Entity: EntityIssue, Metric: MetricSumPrefix + payload},
		},
		{
			name:  "group_by dimension name",
			query: Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{payload}},
		},
		{
			name: "property id in group_by",
			query: Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{
				PropertyPrefix + payload,
			}},
		},
		{
			name: "property id in a filter field",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: PropertyPrefix + payload, Op: OpIs, Values: []string{"a"}},
			}},
		},
		{
			name: "filter field name",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: payload, Op: OpIs, Values: []string{"a"}},
			}},
		},
		{
			name: "filter operator",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "status", Op: payload, Values: []string{"a"}},
			}},
		},
		{
			name: "filter value",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "status", Op: OpIs, Values: []string{payload}},
			}},
		},
		{
			name: "numeric filter value",
			query: Query{Entity: EntityIssue, Metric: MetricCount, Filters: []Filter{
				{Field: "age_days", Op: OpGt, Values: []string{"5" + payload}},
			}},
		},
		{
			name:  "granularity",
			query: Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{DimTime}, Granularity: payload},
		},
		{
			name: "time range field",
			query: Query{Entity: EntityIssue, Metric: MetricCount, TimeRange: &TimeRange{
				Field: payload, LastDays: 7,
			}},
		},
		{
			name: "time range start",
			query: Query{Entity: EntityIssue, Metric: MetricCount, TimeRange: &TimeRange{
				Start: payload, End: "2026-01-01T00:00:00Z",
			}},
		},
		{
			name: "time range end",
			query: Query{Entity: EntityIssue, Metric: MetricCount, TimeRange: &TimeRange{
				Start: "2026-01-01T00:00:00Z", End: payload,
			}},
		},
	}

	ws, err := util.ParseUUID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("parse workspace uuid: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, verr := Compile(&tc.query, ws)
			if verr != nil {
				return // rejected before execution, which is the contract
			}
			if strings.Contains(compiled.SQL, "DROP") || strings.Contains(compiled.SQL, payload) {
				t.Fatalf("payload reached the statement: %s", compiled.SQL)
			}
		})
	}
}

// TestLimitIsAPlaceholderNotAnInteger guards the one numeric field it would be
// tempting to interpolate.
func TestLimitIsAPlaceholderNotAnInteger(t *testing.T) {
	compiled := mustCompile(t, &Query{
		Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{"status"}, Limit: 7,
	})
	if !strings.Contains(compiled.SQL, "LIMIT $") {
		t.Errorf("LIMIT is not parameterized: %s", compiled.SQL)
	}
	if got := compiled.Args[len(compiled.Args)-1]; got != int32(7) {
		t.Errorf("limit argument = %v, want int32(7)", got)
	}
}

// TestWorkspaceIsAlwaysTheFirstArgument is the scoping invariant. $1 is bound
// before any document field is read, and no document field can name it.
func TestWorkspaceIsAlwaysTheFirstArgument(t *testing.T) {
	ws, err := util.ParseUUID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("parse workspace uuid: %v", err)
	}
	for _, entity := range Entities() {
		t.Run(entity, func(t *testing.T) {
			compiled, verr := Compile(&Query{Entity: entity, Metric: MetricCount}, ws)
			if verr != nil {
				t.Fatalf("Compile: %v", verr)
			}
			if len(compiled.Args) == 0 || compiled.Args[0] != ws {
				t.Fatalf("Args[0] = %v, want the workspace id", compiled.Args)
			}
			if !strings.Contains(compiled.SQL, "workspace_id = $1") {
				t.Fatalf("statement is not workspace-scoped: %s", compiled.SQL)
			}
			// $1 must appear exactly once, so no later fragment can rebind it.
			if n := strings.Count(compiled.SQL, "$1"); n != 1 {
				t.Fatalf("$1 appears %d times, want 1: %s", n, compiled.SQL)
			}
		})
	}
}

// TestTaskScopeGoesThroughTheAgentJoin pins the one entity that carries no
// workspace_id of its own. If that join ever becomes a LEFT JOIN, tasks
// belonging to other workspaces start appearing in the answer.
func TestTaskScopeGoesThroughTheAgentJoin(t *testing.T) {
	compiled := mustCompile(t, &Query{Entity: EntityTask, Metric: MetricCount})
	if !strings.Contains(compiled.SQL, "JOIN agent a ON a.id = e.agent_id") {
		t.Fatalf("task scope lost its agent join: %s", compiled.SQL)
	}
	if strings.Contains(compiled.SQL, "LEFT JOIN agent") {
		t.Fatalf("task scope uses an outer join, which would admit unscoped tasks: %s", compiled.SQL)
	}
}

func TestCompileShapes(t *testing.T) {
	cases := []struct {
		name  string
		query Query
		want  string
	}{
		{"no grouping is one figure", Query{Entity: EntityIssue, Metric: MetricCount}, ShapeNumber},
		{"time grouping is a series", Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{DimTime}}, ShapeLine},
		{"counted parts of a whole", Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{"status"}}, ShapeDonut},
		{"a non-count over a category", Query{Entity: EntityIssue, Metric: MetricAvgAgeDays, GroupBy: []string{"status"}}, ShapeBar},
		{"two dimensions", Query{Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{"status", "priority"}}, ShapeBar},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.query.Shape(); got != tc.want {
				t.Errorf("Shape() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLabelGroupingWarnsAboutDoubleCounting(t *testing.T) {
	compiled := mustCompile(t, &Query{
		Entity: EntityIssue, Metric: MetricCount, GroupBy: []string{"label"},
	})
	if !strings.Contains(compiled.SQL, "LEFT JOIN issue_to_label") {
		t.Errorf("label grouping did not join the label table: %s", compiled.SQL)
	}
	if len(compiled.Warnings) == 0 {
		t.Error("a label grouping counts a multi-labelled issue twice and said nothing about it")
	}
}

func TestCycleTimeWarnsThatItIsNotTimeInStatus(t *testing.T) {
	compiled := mustCompile(t, &Query{Entity: EntityIssue, Metric: MetricP50CycleTime})
	joined := strings.Join(compiled.Warnings, " ")
	if !strings.Contains(joined, "status-transition history") {
		t.Errorf("p50_cycle_time shipped without its v1 caveat: %v", compiled.Warnings)
	}
}

// quoteForReason renders a rejected value in a browser-facing message — a
// CJK value whose 40-byte cut lands mid-rune must not come back as invalid
// UTF-8 once unquoted.
func TestQuoteForReasonIsUTF8Safe(t *testing.T) {
	long := strings.Repeat("中文混合内容ab测试", 20)
	got := quoteForReason(long)
	unquoted, err := strconv.Unquote(got)
	if err != nil {
		t.Fatalf("quoteForReason produced an unparseable quoted string: %v (%q)", err, got)
	}
	if !utf8.ValidString(unquoted) {
		t.Fatalf("quoteForReason produced invalid UTF-8: %q", unquoted)
	}
}
