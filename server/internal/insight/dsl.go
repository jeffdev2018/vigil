// Package insight compiles a closed question-answering DSL into read-only SQL.
//
// The whole point of the package is that no SQL string ever crosses the model.
// A natural-language question is translated (translate.go) into a Query — a
// small JSON document whose every field is drawn from a fixed vocabulary — and
// the compiler (compile.go) turns that document into SQL. Free text from the
// document never reaches the statement: it becomes a $n placeholder through
// the addArg closure, exactly like (*Handler).compileIssueTableQuery does for
// the issue table.
//
// The vocabulary is deliberately narrow. Widening it is a product decision,
// not a bug fix: insight_query_log records what people actually asked and
// whether the translation compiled, which is the evidence that decision needs.
package insight

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/util"
)

// Entities. Each maps to exactly one table plus its workspace predicate; there
// is no join vocabulary, so a document cannot reach a table this list omits.
const (
	EntityIssue   = "issue"
	EntityTask    = "task"
	EntityComment = "comment"
)

// Metrics.
const (
	MetricCount        = "count"
	MetricAvgAgeDays   = "avg_age_days"
	MetricP50CycleTime = "p50_cycle_time"
	// MetricSumPrefix is followed by a numeric field from the entity's
	// numericFields allowlist, e.g. "sum:reopen_count".
	MetricSumPrefix = "sum:"
)

// Granularities for the `time` dimension.
const (
	GranularityDay   = "day"
	GranularityWeek  = "week"
	GranularityMonth = "month"
)

// Filter operators. Set operators take Values; the range operators take
// exactly one value; the presence operators take none.
const (
	OpIs         = "is"
	OpIsNot      = "is_not"
	OpIn         = "in"
	OpNotIn      = "not_in"
	OpGt         = "gt"
	OpGte        = "gte"
	OpLt         = "lt"
	OpLte        = "lte"
	OpIsEmpty    = "is_empty"
	OpIsNotEmpty = "is_not_empty"
)

// PropertyPrefix marks a custom issue property, addressed by its definition
// UUID: "property:5c9f…". The UUID is validated here and passed as an argument,
// never interpolated.
const PropertyPrefix = "property:"

// DimTime is the one dimension that is not a column: it buckets the entity's
// creation timestamp at Granularity.
const DimTime = "time"

// Bounds. limit caps the returned group count; the compiler also applies
// MaxLimit when the document omits one. lastDays is bounded so a document
// cannot ask the database to scan an unbounded history.
const (
	DefaultLimit = 20
	MaxLimit     = 200
	MaxLastDays  = 730
	MaxFilters   = 12
	MaxGroupBy   = 2
	MaxValues    = 50
	MaxValueLen  = 128
)

// Query is the whole DSL. It is the only thing the model is allowed to emit
// and the only thing the compiler will read. workspace_id is deliberately
// absent: the compiler injects it from the authenticated request, so a
// document has no field through which it could name another workspace.
type Query struct {
	Entity      string     `json:"entity"`
	Metric      string     `json:"metric"`
	GroupBy     []string   `json:"group_by,omitempty"`
	Filters     []Filter   `json:"filters,omitempty"`
	TimeRange   *TimeRange `json:"time_range,omitempty"`
	Granularity string     `json:"granularity,omitempty"`
	Limit       int        `json:"limit,omitempty"`
}

// Filter is one predicate. Field names a dimension from the entity's
// vocabulary (or a numeric pseudo-field like age_days); Values carry the
// user's data and always become placeholders.
type Filter struct {
	Field  string   `json:"field"`
	Op     string   `json:"op"`
	Values []string `json:"values,omitempty"`
}

// TimeRange bounds the scan. LastDays is the relative form the model is asked
// to prefer; Start/End are the absolute form, both RFC3339.
type TimeRange struct {
	Field    string `json:"field,omitempty"`
	LastDays int    `json:"last_days,omitempty"`
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"`
}

// Error is the typed rejection every validation failure produces. The handler
// turns it into a 400 with Field/Reason, and does so BEFORE any execution:
// nothing in this package opens a transaction until Validate has returned nil.
type Error struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (e *Error) Error() string { return e.Field + ": " + e.Reason }

func invalid(field, reason string) *Error { return &Error{Field: field, Reason: reason} }

// entitySpec is the closed description of one queryable entity: its table, the
// alias every expression uses, the workspace predicate, and the columns the
// DSL may name. Nothing outside these tables is reachable.
type entitySpec struct {
	// from is the FROM clause fragment. It always aliases the entity `e`.
	from string
	// workspacePredicate always compares against $1 and nothing else.
	workspacePredicate string
	// createdAt / closedAt are the two timestamps the age metrics use.
	// closedAt is empty when the entity has no completion concept.
	createdAt string
	closedAt  string
	// idleAt is "when this row last moved". agent_task_queue has no
	// updated_at, so each entity names its own best available answer.
	idleAt string
	// dims maps a dimension name to its SQL group expression. Values are
	// package constants — no caller input reaches them.
	dims map[string]string
	// timeColumns are the columns a time_range may bound.
	timeColumns map[string]string
	// numericFields are the columns sum:<field> may total.
	numericFields map[string]string
	// enumFilters are the dimensions a set filter may compare, mapped to the
	// text expression compared against a placeholder.
	enumFilters map[string]string
	// supportsProperties says whether property:<uuid> is addressable.
	supportsProperties bool
	// labelJoin is emitted once when a label dimension or filter is used.
	labelJoin string
}

var entities = map[string]entitySpec{
	EntityIssue: {
		from:               "issue e",
		workspacePredicate: "e.workspace_id = $1",
		createdAt:          "e.created_at",
		closedAt:           "e.completed_at",
		idleAt:             "e.updated_at",
		dims: map[string]string{
			"status":     "e.status",
			"priority":   "e.priority",
			"assignee":   "CASE WHEN e.assignee_id IS NULL THEN 'unassigned' ELSE COALESCE(e.assignee_type, '') || ':' || e.assignee_id::text END",
			"project":    "COALESCE(e.project_id::text, 'none')",
			"issue_type": "COALESCE(e.issue_type, 'none')",
			"label":      "COALESCE(il.label_id::text, 'none')",
		},
		timeColumns: map[string]string{
			"created_at":   "e.created_at",
			"updated_at":   "e.updated_at",
			"completed_at": "e.completed_at",
		},
		numericFields: map[string]string{
			"reopen_count": "e.reopen_count",
			"stage":        "e.stage",
		},
		enumFilters: map[string]string{
			"status":     "e.status",
			"priority":   "e.priority",
			"issue_type": "COALESCE(e.issue_type, '')",
			"project":    "COALESCE(e.project_id::text, '')",
			"assignee":   "COALESCE(e.assignee_id::text, '')",
			"label":      "COALESCE(il.label_id::text, '')",
		},
		supportsProperties: true,
		labelJoin:          "LEFT JOIN issue_to_label il ON il.issue_id = e.id",
	},
	EntityTask: {
		// agent_task_queue carries no workspace_id, so the workspace comes
		// from the agent that owns the run. The join is INNER on purpose: a
		// task whose agent belongs to another workspace cannot survive it,
		// which is what makes cross-workspace reads impossible by
		// construction rather than by a predicate someone could forget.
		from:               "agent_task_queue e JOIN agent a ON a.id = e.agent_id",
		workspacePredicate: "a.workspace_id = $1",
		createdAt:          "e.created_at",
		closedAt:           "e.completed_at",
		idleAt:             "COALESCE(e.completed_at, e.started_at, e.dispatched_at, e.created_at)",
		dims: map[string]string{
			"status": "e.status",
			"agent":  "e.agent_id::text",
		},
		timeColumns: map[string]string{
			"created_at":   "e.created_at",
			"completed_at": "e.completed_at",
			"started_at":   "e.started_at",
		},
		numericFields: map[string]string{
			"attempt": "e.attempt",
		},
		enumFilters: map[string]string{
			"status": "e.status",
			"agent":  "COALESCE(e.agent_id::text, '')",
		},
	},
	EntityComment: {
		from:               "comment e",
		workspacePredicate: "e.workspace_id = $1",
		createdAt:          "e.created_at",
		// A comment has no completion; p50_cycle_time is rejected for it
		// rather than silently answered with updated_at.
		closedAt: "",
		idleAt:   "e.updated_at",
		dims: map[string]string{
			"author":      "e.author_type || ':' || e.author_id::text",
			"author_type": "e.author_type",
			"type":        "e.type",
		},
		timeColumns: map[string]string{
			"created_at": "e.created_at",
			"updated_at": "e.updated_at",
		},
		numericFields: map[string]string{},
		enumFilters: map[string]string{
			"author_type": "e.author_type",
			"type":        "e.type",
			"author":      "COALESCE(e.author_id::text, '')",
		},
	},
}

// numericPseudoFields are filterable ages derived from timestamps rather than
// stored. They are the v1 stand-in for a status-transition history the schema
// does not have: idle_days is "unchanged for N days", NOT "in this status for
// N days". See the reference shipped with the platform skill.
var numericPseudoFields = map[string]bool{
	"age_days":  true,
	"idle_days": true,
}

var granularities = map[string]string{
	GranularityDay:   "day",
	GranularityWeek:  "week",
	GranularityMonth: "month",
}

var setOps = map[string]bool{OpIs: true, OpIsNot: true, OpIn: true, OpNotIn: true}
var rangeOps = map[string]string{OpGt: ">", OpGte: ">=", OpLt: "<", OpLte: "<="}
var presenceOps = map[string]bool{OpIsEmpty: true, OpIsNotEmpty: true}

// Entities lists the supported entities, for the prompt and the reference.
func Entities() []string { return []string{EntityIssue, EntityTask, EntityComment} }

// DimensionsFor lists the group-by vocabulary of one entity, sorted, with
// `time` appended. Used by the prompt builder so the model is told exactly
// what it may emit rather than guessing.
func DimensionsFor(entity string) []string {
	spec, ok := entities[entity]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(spec.dims)+1)
	for name := range spec.dims {
		out = append(out, name)
	}
	sortStrings(out)
	return append(out, DimTime)
}

// FilterFieldsFor lists the filterable fields of one entity, sorted.
func FilterFieldsFor(entity string) []string {
	spec, ok := entities[entity]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(spec.enumFilters)+len(numericPseudoFields))
	for name := range spec.enumFilters {
		out = append(out, name)
	}
	for name := range numericPseudoFields {
		out = append(out, name)
	}
	sortStrings(out)
	return out
}

// MetricsFor lists the metrics one entity accepts, sorted, with the concrete
// sum:<field> forms expanded.
func MetricsFor(entity string) []string {
	spec, ok := entities[entity]
	if !ok {
		return nil
	}
	out := []string{MetricCount, MetricAvgAgeDays}
	if spec.closedAt != "" {
		out = append(out, MetricP50CycleTime)
	}
	for name := range spec.numericFields {
		out = append(out, MetricSumPrefix+name)
	}
	sortStrings(out)
	return out
}

// Validate checks the whole document against the vocabulary and returns the
// first violation. A nil return is the compiler's precondition: Compile
// assumes every enum it reads is already known.
func (q *Query) Validate() *Error {
	spec, ok := entities[q.Entity]
	if !ok {
		return invalid("entity", "unknown entity "+quoteForReason(q.Entity))
	}
	if err := validateMetric(spec, q.Metric); err != nil {
		return err
	}
	if len(q.GroupBy) > MaxGroupBy {
		return invalid("group_by", fmt.Sprintf("at most %d dimensions", MaxGroupBy))
	}
	seenDim := make(map[string]bool, len(q.GroupBy))
	for _, dim := range q.GroupBy {
		if err := validateDimension(spec, dim); err != nil {
			return err
		}
		if seenDim[dim] {
			return invalid("group_by", "duplicate dimension "+quoteForReason(dim))
		}
		seenDim[dim] = true
	}
	if q.Granularity != "" {
		if _, ok := granularities[q.Granularity]; !ok {
			return invalid("granularity", "unknown granularity "+quoteForReason(q.Granularity))
		}
	}
	if len(q.Filters) > MaxFilters {
		return invalid("filters", fmt.Sprintf("at most %d filters", MaxFilters))
	}
	for i, f := range q.Filters {
		if err := validateFilter(spec, i, f); err != nil {
			return err
		}
	}
	if err := validateTimeRange(spec, q.TimeRange); err != nil {
		return err
	}
	if q.Limit < 0 || q.Limit > MaxLimit {
		return invalid("limit", fmt.Sprintf("limit must be between 0 and %d", MaxLimit))
	}
	return nil
}

func validateMetric(spec entitySpec, metric string) *Error {
	switch {
	case metric == MetricCount:
		return nil
	case metric == MetricAvgAgeDays:
		return nil
	case metric == MetricP50CycleTime:
		if spec.closedAt == "" {
			return invalid("metric", "p50_cycle_time needs a completion timestamp this entity does not have")
		}
		return nil
	case strings.HasPrefix(metric, MetricSumPrefix):
		field := strings.TrimPrefix(metric, MetricSumPrefix)
		if _, ok := spec.numericFields[field]; !ok {
			return invalid("metric", "unknown numeric field "+quoteForReason(field))
		}
		return nil
	default:
		return invalid("metric", "unknown metric "+quoteForReason(metric))
	}
}

func validateDimension(spec entitySpec, dim string) *Error {
	if dim == DimTime {
		return nil
	}
	if strings.HasPrefix(dim, PropertyPrefix) {
		return validatePropertyRef(spec, "group_by", dim)
	}
	if _, ok := spec.dims[dim]; !ok {
		return invalid("group_by", "unknown dimension "+quoteForReason(dim))
	}
	return nil
}

func validateFilter(spec entitySpec, index int, f Filter) *Error {
	field := fmt.Sprintf("filters[%d]", index)
	isProperty := strings.HasPrefix(f.Field, PropertyPrefix)
	if isProperty {
		if err := validatePropertyRef(spec, field+".field", f.Field); err != nil {
			return err
		}
	} else if _, ok := spec.enumFilters[f.Field]; !ok {
		if _, numeric := numericPseudoFields[f.Field]; !numeric {
			return invalid(field+".field", "unknown filter field "+quoteForReason(f.Field))
		}
	}
	_, isNumeric := numericPseudoFields[f.Field]

	switch {
	case presenceOps[f.Op]:
		if len(f.Values) != 0 {
			return invalid(field+".values", "presence operators take no value")
		}
		if isNumeric {
			return invalid(field+".op", "presence operators do not apply to a numeric field")
		}
		return nil
	case setOps[f.Op]:
		if isNumeric {
			return invalid(field+".op", "set operators do not apply to a numeric field")
		}
		return validateValues(field, f.Values)
	case rangeOps[f.Op] != "":
		if !isNumeric {
			return invalid(field+".op", "range operators apply to age_days and idle_days only")
		}
		if len(f.Values) != 1 {
			return invalid(field+".values", "range operators take exactly one value")
		}
		if _, err := strconv.ParseFloat(f.Values[0], 64); err != nil {
			return invalid(field+".values", "range value must be a number")
		}
		return nil
	default:
		return invalid(field+".op", "unknown operator "+quoteForReason(f.Op))
	}
}

func validateValues(field string, values []string) *Error {
	if len(values) == 0 {
		return invalid(field+".values", "at least one value is required")
	}
	if len(values) > MaxValues {
		return invalid(field+".values", fmt.Sprintf("at most %d values", MaxValues))
	}
	for _, v := range values {
		if len(v) > MaxValueLen {
			return invalid(field+".values", fmt.Sprintf("value longer than %d characters", MaxValueLen))
		}
	}
	return nil
}

// validatePropertyRef checks the `property:<uuid>` form. The UUID is parsed,
// not pattern-matched, so anything that is not a UUID — including a quote, a
// comment marker, or a nested SELECT — is rejected here and never reaches the
// compiler.
func validatePropertyRef(spec entitySpec, field, ref string) *Error {
	if !spec.supportsProperties {
		return invalid(field, "this entity has no custom properties")
	}
	raw := strings.TrimPrefix(ref, PropertyPrefix)
	if _, err := uuid.Parse(raw); err != nil {
		return invalid(field, "property reference must be property:<uuid>")
	}
	return nil
}

func validateTimeRange(spec entitySpec, tr *TimeRange) *Error {
	if tr == nil {
		return nil
	}
	if tr.Field != "" {
		if _, ok := spec.timeColumns[tr.Field]; !ok {
			return invalid("time_range.field", "unknown time field "+quoteForReason(tr.Field))
		}
	}
	if tr.LastDays != 0 {
		if tr.LastDays < 1 || tr.LastDays > MaxLastDays {
			return invalid("time_range.last_days", fmt.Sprintf("last_days must be between 1 and %d", MaxLastDays))
		}
		if tr.Start != "" || tr.End != "" {
			return invalid("time_range", "use last_days or start/end, not both")
		}
		return nil
	}
	if tr.Start == "" && tr.End == "" {
		return invalid("time_range", "either last_days or start/end is required")
	}
	if _, err := parseRFC3339(tr.Start); tr.Start != "" && err != nil {
		return invalid("time_range.start", "start must be an RFC3339 timestamp")
	}
	if _, err := parseRFC3339(tr.End); tr.End != "" && err != nil {
		return invalid("time_range.end", "end must be an RFC3339 timestamp")
	}
	if tr.Start != "" && tr.End != "" {
		start, _ := parseRFC3339(tr.Start)
		end, _ := parseRFC3339(tr.End)
		if !start.Before(end) {
			return invalid("time_range", "start must be before end")
		}
	}
	return nil
}

// quoteForReason keeps a rejected value readable in the 400 body without
// letting an arbitrary blob through: it is truncated and stripped of control
// characters. The value never reaches SQL either way; this is about the error
// message, which is rendered in a browser.
func quoteForReason(value string) string {
	const max = 40
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(cleaned) > max {
		cleaned = util.TruncateUTF8Bytes(cleaned, max) + "…"
	}
	return strconv.Quote(cleaned)
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
