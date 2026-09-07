package insight

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Compiled is a ready-to-execute read-only statement plus the argument list it
// was built with. Args[0] is always the workspace UUID and $1 is the only
// place it appears, so every compiled statement is workspace-scoped whatever
// the document said.
type Compiled struct {
	SQL  string
	Args []any
	// Columns names the SELECT list in order: one column per group_by
	// dimension, then "value". The runner uses it to build row maps without
	// asking the driver for column metadata.
	Columns []string
	// Warnings carry the honest caveats about what the numbers mean — a label
	// grouping double-counts multi-labelled issues, cycle time is not
	// time-in-status. They travel to the UI with the rows.
	Warnings []string
}

// Shapes the API may return. The frontend derives the same value from metric
// and group_by (packages/core/insights/shape.ts) and treats an unknown value
// through a default branch, so adding one here cannot break an installed
// client.
const (
	ShapeNumber = "number"
	ShapeBar    = "bar"
	ShapeLine   = "line"
	ShapeDonut  = "donut"
)

// Shape derives the chart form from the query alone. No group_by is a single
// figure; a time grouping is a series; a small categorical grouping reads best
// as a donut when the metric is a count of parts of a whole, otherwise bars.
func (q *Query) Shape() string {
	switch len(q.GroupBy) {
	case 0:
		return ShapeNumber
	case 1:
		if q.GroupBy[0] == DimTime {
			return ShapeLine
		}
		if q.Metric == MetricCount {
			return ShapeDonut
		}
		return ShapeBar
	default:
		return ShapeBar
	}
}

// Compile turns a validated document into SQL. It panics on nothing and
// assumes Validate has already run: every map lookup below is on a key
// Validate proved present.
//
// The security property is structural, not textual. `addArg` is the ONLY way a
// value from the document reaches the statement, and it can produce nothing
// but "$n"; every other fragment is a constant from entitySpec. workspaceID is
// bound as $1 before any document field is read, so there is no ordering in
// which a document value could displace it.
func Compile(q *Query, workspaceID pgtype.UUID) (Compiled, *Error) {
	if err := q.Validate(); err != nil {
		return Compiled{}, err
	}
	spec := entities[q.Entity]

	args := []any{workspaceID}
	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	where := []string{spec.workspacePredicate}
	needsLabelJoin := false
	var warnings []string

	granularity := q.Granularity
	if granularity == "" {
		granularity = GranularityDay
	}

	selects := make([]string, 0, len(q.GroupBy)+1)
	columns := make([]string, 0, len(q.GroupBy)+1)
	groupRefs := make([]string, 0, len(q.GroupBy))
	timeIndex := -1
	for i, dim := range q.GroupBy {
		expr, usesLabel := dimensionExpr(spec, dim, granularity, addArg)
		if usesLabel {
			needsLabelJoin = true
			warnings = append(warnings, "an issue with several labels is counted once per label")
		}
		alias := "g" + strconv.Itoa(i)
		selects = append(selects, expr+" AS "+alias)
		columns = append(columns, dim)
		groupRefs = append(groupRefs, strconv.Itoa(i+1))
		if dim == DimTime {
			timeIndex = i
		}
	}
	metricExpr, metricWarning := metricExpr(spec, q.Metric)
	if metricWarning != "" {
		warnings = append(warnings, metricWarning)
	}
	selects = append(selects, metricExpr+" AS value")
	columns = append(columns, "value")

	for _, f := range q.Filters {
		predicate, usesLabel := filterPredicate(spec, f, addArg)
		if predicate == "" {
			continue
		}
		if usesLabel {
			needsLabelJoin = true
		}
		where = append(where, predicate)
	}

	if q.TimeRange != nil {
		where = append(where, timeRangePredicate(spec, q.TimeRange, addArg)...)
	}

	from := spec.from
	if needsLabelJoin {
		if spec.labelJoin == "" {
			// Unreachable: only the issue entity exposes a label dimension,
			// and only the issue entity carries a join. Kept as a guard so a
			// future entity cannot silently compile a join-less label filter.
			return Compiled{}, invalid("group_by", "this entity has no labels")
		}
		from += " " + spec.labelJoin
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(selects, ", "))
	b.WriteString(" FROM ")
	b.WriteString(from)
	b.WriteString(" WHERE ")
	b.WriteString(strings.Join(where, " AND "))
	if len(groupRefs) > 0 {
		b.WriteString(" GROUP BY ")
		b.WriteString(strings.Join(groupRefs, ", "))
		if timeIndex >= 0 {
			// A series is read left to right; sorting it by magnitude would
			// scramble the very axis the chart exists to show.
			b.WriteString(" ORDER BY " + strconv.Itoa(timeIndex+1) + " ASC")
		} else {
			b.WriteString(" ORDER BY value DESC NULLS LAST, 1 ASC")
		}
		limit := q.Limit
		if limit == 0 {
			limit = DefaultLimit
		}
		b.WriteString(" LIMIT " + addArg(int32(limit)))
	}

	return Compiled{
		SQL:      b.String(),
		Args:     args,
		Columns:  columns,
		Warnings: warnings,
	}, nil
}

// dimensionExpr returns the GROUP BY expression for one dimension and whether
// it needs the label join.
func dimensionExpr(spec entitySpec, dim, granularity string, addArg func(any) string) (string, bool) {
	if dim == DimTime {
		// granularity is a map key from `granularities`, so the only strings
		// that can land here are "day", "week" and "month".
		return fmt.Sprintf("to_char(date_trunc('%s', %s), 'YYYY-MM-DD')", granularities[granularity], spec.createdAt), false
	}
	if strings.HasPrefix(dim, PropertyPrefix) {
		key := strings.TrimPrefix(dim, PropertyPrefix)
		return fmt.Sprintf("COALESCE(e.properties ->> %s, 'none')", addArg(key)), false
	}
	return spec.dims[dim], dim == "label"
}

// metricExpr returns the aggregate and, when the number carries a caveat, the
// sentence the UI must show next to it.
func metricExpr(spec entitySpec, metric string) (string, string) {
	switch {
	case metric == MetricCount:
		return "count(*)::double precision", ""
	case metric == MetricAvgAgeDays:
		end := "now()"
		if spec.closedAt != "" {
			end = "COALESCE(" + spec.closedAt + ", now())"
		}
		return fmt.Sprintf("AVG(EXTRACT(EPOCH FROM (%s - %s)) / 86400.0)::double precision", end, spec.createdAt),
			"age is measured from creation, not from the last status change"
	case metric == MetricP50CycleTime:
		return fmt.Sprintf(
				"percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (%s - %s)) / 86400.0)::double precision",
				spec.closedAt, spec.createdAt),
			"cycle time is creation to completion; the schema keeps no status-transition history, so time spent in a given status cannot be measured"
	default:
		field := strings.TrimPrefix(metric, MetricSumPrefix)
		return fmt.Sprintf("COALESCE(SUM(%s), 0)::double precision", spec.numericFields[field]), ""
	}
}

// filterPredicate compiles one filter. Every value goes through addArg; the
// only literals are operators and column expressions from entitySpec.
func filterPredicate(spec entitySpec, f Filter, addArg func(any) string) (string, bool) {
	if numericPseudoFields[f.Field] {
		column := spec.createdAt
		if f.Field == "idle_days" {
			column = spec.idleAt
		}
		// Validate proved the value parses as a float.
		value, _ := strconv.ParseFloat(f.Values[0], 64)
		return fmt.Sprintf(
			"EXTRACT(EPOCH FROM (now() - %s)) / 86400.0 %s %s::double precision",
			column, rangeOps[f.Op], addArg(value),
		), false
	}

	var expr string
	usesLabel := false
	if strings.HasPrefix(f.Field, PropertyPrefix) {
		key := strings.TrimPrefix(f.Field, PropertyPrefix)
		expr = fmt.Sprintf("COALESCE(e.properties ->> %s, '')", addArg(key))
	} else {
		expr = spec.enumFilters[f.Field]
		usesLabel = f.Field == "label"
	}

	switch f.Op {
	case OpIsEmpty:
		return expr + " = ''", usesLabel
	case OpIsNotEmpty:
		return expr + " <> ''", usesLabel
	case OpIs, OpIn:
		return fmt.Sprintf("%s = ANY(%s::text[])", expr, addArg(f.Values)), usesLabel
	case OpIsNot, OpNotIn:
		return fmt.Sprintf("NOT (%s = ANY(%s::text[]))", expr, addArg(f.Values)), usesLabel
	}
	return "", usesLabel
}

func timeRangePredicate(spec entitySpec, tr *TimeRange, addArg func(any) string) []string {
	column := spec.createdAt
	if tr.Field != "" {
		column = spec.timeColumns[tr.Field]
	}
	if tr.LastDays > 0 {
		return []string{fmt.Sprintf(
			"%s >= now() - (%s::int * INTERVAL '1 day')",
			column, addArg(int32(tr.LastDays)),
		)}
	}
	out := make([]string, 0, 2)
	if tr.Start != "" {
		start, _ := parseRFC3339(tr.Start)
		out = append(out, fmt.Sprintf("%s >= %s", column, addArg(start)))
	}
	if tr.End != "" {
		end, _ := parseRFC3339(tr.End)
		out = append(out, fmt.Sprintf("%s < %s", column, addArg(end)))
	}
	return out
}

func parseRFC3339(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}
