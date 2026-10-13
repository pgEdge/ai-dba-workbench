/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// Result shapes query_datastore can return.
const (
	datastoreShapeRows      = "rows"
	datastoreShapeLatest    = "latest"
	datastoreShapeAggregate = "aggregate"
)

// Request limits.
const (
	datastoreDefaultPageSize = 100
	datastoreMaxPageSize     = 1000
	// datastoreMaxPage bounds page so (page-1)*page_size cannot
	// overflow and an absurd offset is refused before it reaches SQL.
	datastoreMaxPage     = 1000000
	datastoreMaxBuckets  = 500
	datastoreMaxInValues = 1000
	// datastoreBucketAlias names the time-bucket column of an
	// aggregate query.
	datastoreBucketAlias = "bucket_time"
)

// datastoreOperators maps each accepted filter operator to its SQL.
var datastoreOperators = map[string]string{
	"=":           "=",
	"!=":          "<>",
	"<":           "<",
	"<=":          "<=",
	">":           ">",
	">=":          ">=",
	"in":          "IN",
	"not in":      "NOT IN",
	"like":        "LIKE",
	"is null":     "IS NULL",
	"is not null": "IS NOT NULL",
}

// datastoreOperatorList is the operator list error messages give.
const datastoreOperatorList = "=, !=, <, <=, >, >=, in, not in, like, is null, is not null"

// datastoreAggregations lists the accepted aggregate functions.
var datastoreAggregations = map[string]bool{
	"avg": true, "sum": true, "min": true, "max": true, "count": true,
}

// datastoreFilter is one filter condition. Value is the caller's raw
// JSON value; it is only ever bound as a parameter.
type datastoreFilter struct {
	Column string
	Op     string
	Value  any
	// HasValue records whether the caller supplied a value at all, so a
	// missing value and an explicit null get distinct errors.
	HasValue bool
}

// datastoreOrder is one ordering term; Direction is "asc" or "desc".
type datastoreOrder struct {
	Column    string
	Direction string
}

// datastoreRequest is a parsed query_datastore request. Its fields
// mirror the tool's input schema; column names are checked against the
// source later, by buildDatastoreQuery.
type datastoreRequest struct {
	Source        string
	Columns       []string
	ConnectionIDs []int
	TimeStart     string
	TimeEnd       string
	Filters       []datastoreFilter
	Shape         string
	GroupBy       []string
	Aggregation   string
	// Buckets is zero when the caller asked for no time buckets.
	Buckets  int
	OrderBy  []datastoreOrder
	Page     int
	PageSize int
	Explain  bool
}

// visibleSet is the set of connections a caller may read, as
// RBACChecker.VisibleConnectionIDs reports it.
type visibleSet struct {
	// All is true when the caller may read every connection.
	All bool
	// IDs lists the readable connections when All is false.
	IDs []int
	// SystemAlerts is true when the caller may see system alerts
	// (RBACChecker.CanSeeSystemAlerts). The zero value hides them.
	SystemAlerts bool
}

// datastoreRequestKeys lists the parameters query_datastore accepts.
var datastoreRequestKeys = map[string]bool{
	"source": true, "columns": true, "connection_ids": true,
	"time_start": true, "time_end": true, "filters": true, "shape": true,
	"group_by": true, "aggregation": true, "buckets": true,
	"order_by": true, "page": true, "page_size": true, "explain": true,
}

// parseDatastoreRequest validates the shape of query_datastore's
// arguments. Keys starting "__" are injected by the registry and are
// ignored; any other unknown key is an error, so a caller cannot believe
// a parameter took effect when it did not.
func parseDatastoreRequest(args map[string]any) (datastoreRequest, error) {
	req := datastoreRequest{
		Shape:    datastoreShapeRows,
		Page:     1,
		PageSize: datastoreDefaultPageSize,
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if strings.HasPrefix(key, "__") || datastoreRequestKeys[key] {
			continue
		}
		if key == "query" || key == "sql" {
			return req, errors.New("query_datastore no longer accepts SQL; describe the query with source, columns, filters and the other parameters, and call describe_datastore to list the sources")
		}
		return req, fmt.Errorf("unknown parameter %q", key)
	}

	source, ok := args["source"]
	if !ok {
		return req, errors.New("source is required; call describe_datastore to list the sources")
	}
	name, ok := source.(string)
	if !ok {
		return req, errors.New("source must be a string")
	}
	req.Source = strings.TrimSpace(name)
	if req.Source == "" {
		return req, errors.New("source is required; call describe_datastore to list the sources")
	}

	var err error
	if req.Columns, err = parseStringList(args, "columns"); err != nil {
		return req, err
	}
	if req.GroupBy, err = parseStringList(args, "group_by"); err != nil {
		return req, err
	}
	if req.ConnectionIDs, err = parseConnectionIDs(args); err != nil {
		return req, err
	}
	if req.TimeStart, err = parseOptionalString(args, "time_start"); err != nil {
		return req, err
	}
	if req.TimeEnd, err = parseOptionalString(args, "time_end"); err != nil {
		return req, err
	}
	if req.Filters, err = parseDatastoreFilters(args); err != nil {
		return req, err
	}
	if req.OrderBy, err = parseDatastoreOrder(args); err != nil {
		return req, err
	}

	if v, ok := args["shape"]; ok {
		shape := stringValue(v)
		shape = strings.ToLower(strings.TrimSpace(shape))
		if shape != datastoreShapeRows && shape != datastoreShapeLatest && shape != datastoreShapeAggregate {
			return req, errors.New("shape must be one of rows, latest or aggregate")
		}
		req.Shape = shape
	}
	if v, ok := args["aggregation"]; ok {
		agg := stringValue(v)
		agg = strings.ToLower(strings.TrimSpace(agg))
		if !datastoreAggregations[agg] {
			return req, errors.New("aggregation must be one of avg, sum, min, max or count")
		}
		req.Aggregation = agg
	}
	if v, ok := args["buckets"]; ok {
		n, ok := integerValue(v)
		if !ok || n < 1 || n > datastoreMaxBuckets {
			return req, fmt.Errorf("buckets must be an integer between 1 and %d", datastoreMaxBuckets)
		}
		req.Buckets = int(n)
	}
	if v, ok := args["page"]; ok {
		n, ok := integerValue(v)
		if !ok || n < 1 || n > datastoreMaxPage {
			return req, fmt.Errorf("page must be an integer between 1 and %d", datastoreMaxPage)
		}
		req.Page = int(n)
	}
	if v, ok := args["page_size"]; ok {
		n, ok := integerValue(v)
		if !ok || n < 1 || n > datastoreMaxPageSize {
			return req, fmt.Errorf("page_size must be an integer between 1 and %d", datastoreMaxPageSize)
		}
		req.PageSize = int(n)
	}
	if v, ok := args["explain"]; ok {
		b, ok := v.(bool)
		if !ok {
			return req, errors.New("explain must be a boolean")
		}
		req.Explain = b
	}
	return req, nil
}

// integerValue converts a JSON number to an integer, refusing anything
// fractional or outside the int64 range.
func integerValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		// float64(math.MaxInt64) rounds up to 2^63, which int64 cannot
		// hold, so the upper bound is exclusive; -2^63 is exact.
		if n != math.Trunc(n) || n < math.MinInt64 || n >= math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

// stringValue returns v when it is a string, and "" otherwise, so the
// caller's own validation reports the bad value.
func stringValue(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func parseOptionalString(args map[string]any, name string) (string, error) {
	v, ok := args[name]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return strings.TrimSpace(s), nil
}

func parseStringList(args map[string]any, name string) ([]string, error) {
	v, ok := args[name]
	if !ok {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be an array of strings", name)
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out, nil
}

func parseConnectionIDs(args map[string]any) ([]int, error) {
	v, ok := args["connection_ids"]
	if !ok {
		return nil, nil
	}
	const msg = "connection_ids must be an array of positive integers"
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New(msg)
	}
	ids := make([]int, 0, len(list))
	for _, item := range list {
		n, ok := integerValue(item)
		if !ok || n < 1 || n > math.MaxInt32 {
			return nil, errors.New(msg)
		}
		ids = append(ids, int(n))
	}
	return ids, nil
}

// checkObjectKeys rejects any key of obj not in allowed.
func checkObjectKeys(obj map[string]any, where string, allowed ...string) error {
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("%s: unknown key %q", where, key)
		}
	}
	return nil
}

func parseDatastoreFilters(args map[string]any) ([]datastoreFilter, error) {
	v, ok := args["filters"]
	if !ok {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("filters must be an array of objects")
	}
	filters := make([]datastoreFilter, 0, len(list))
	for i, item := range list {
		where := fmt.Sprintf("filters[%d]", i)
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", where)
		}
		if err := checkObjectKeys(obj, where, "column", "op", "value"); err != nil {
			return nil, err
		}
		column := stringValue(obj["column"])
		column = strings.TrimSpace(column)
		if column == "" {
			return nil, fmt.Errorf("%s.column must be a non-empty string", where)
		}
		op, ok := obj["op"].(string)
		if !ok {
			return nil, fmt.Errorf("%s.op must be a string", where)
		}
		op = strings.Join(strings.Fields(strings.ToLower(op)), " ")
		if _, ok := datastoreOperators[op]; !ok {
			return nil, fmt.Errorf("%s.op: unsupported operator %q; use one of %s", where, op, datastoreOperatorList)
		}
		value, hasValue := obj["value"]
		filters = append(filters, datastoreFilter{Column: column, Op: op, Value: value, HasValue: hasValue})
	}
	return filters, nil
}

func parseDatastoreOrder(args map[string]any) ([]datastoreOrder, error) {
	v, ok := args["order_by"]
	if !ok {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("order_by must be an array of objects")
	}
	order := make([]datastoreOrder, 0, len(list))
	for i, item := range list {
		where := fmt.Sprintf("order_by[%d]", i)
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", where)
		}
		if err := checkObjectKeys(obj, where, "column", "direction"); err != nil {
			return nil, err
		}
		column := stringValue(obj["column"])
		column = strings.TrimSpace(column)
		if column == "" {
			return nil, fmt.Errorf("%s.column must be a non-empty string", where)
		}
		direction := "asc"
		if d, ok := obj["direction"]; ok {
			s := stringValue(d)
			direction = strings.ToLower(strings.TrimSpace(s))
			if direction != "asc" && direction != "desc" {
				return nil, fmt.Errorf("%s.direction must be asc or desc", where)
			}
		}
		order = append(order, datastoreOrder{Column: column, Direction: direction})
	}
	return order, nil
}

// errConnectionNotAccessible reports a named connection the caller may
// not read. The handler turns it into the same response every other tool
// gives for a missing or hidden connection.
var errConnectionNotAccessible = errors.New(connectionNotAccessibleMsg)

// datastoreQuery accumulates bind parameters while a statement is built.
type datastoreQuery struct {
	args []any
}

// bind adds a parameter and returns its placeholder.
func (q *datastoreQuery) bind(v any) string {
	q.args = append(q.args, v)
	return "$" + strconv.Itoa(len(q.args))
}

// datastoreTableAlias is the alias every generated statement gives the
// source table.
const datastoreTableAlias = "t"

// qualified returns alias."column" with the column quoted.
func qualified(alias, column string) string {
	return alias + "." + metrics.QuoteIdentifier(column)
}

// buildDatastoreQuery turns a validated request into a single SELECT
// over src, limited to the rows vis may read, and returns the statement
// and its bind parameters. Every identifier comes from src's allow-list
// and is quoted; every caller-supplied value is a parameter. It does not
// touch the database, and now stands in for the current time so the
// result is deterministic.
//
// explain is the caller's concern: the statement is the same either way.
func buildDatastoreQuery(src datastoreSource, req datastoreRequest, vis visibleSet, now time.Time) (string, []any, error) {
	if req.Source != src.Name {
		return "", nil, fmt.Errorf("request source %q does not match %q", req.Source, src.Name)
	}
	if req.PageSize < 1 || req.PageSize > datastoreMaxPageSize {
		return "", nil, fmt.Errorf("page_size must be an integer between 1 and %d", datastoreMaxPageSize)
	}
	if req.Page < 1 || req.Page > datastoreMaxPage {
		return "", nil, fmt.Errorf("page must be an integer between 1 and %d", datastoreMaxPage)
	}
	if err := checkShape(src, req); err != nil {
		return "", nil, err
	}

	columns := req.Columns
	if len(columns) == 0 && req.Shape != datastoreShapeAggregate {
		columns = src.columnNames()
	}
	if err := checkColumnList(src, columns, "columns"); err != nil {
		return "", nil, err
	}
	if err := checkColumnList(src, req.GroupBy, "group_by"); err != nil {
		return "", nil, err
	}

	q := &datastoreQuery{}
	var conditions []string

	scope, err := scopeCondition(q, src, req, vis)
	if err != nil {
		return "", nil, err
	}
	if scope != "" {
		conditions = append(conditions, scope)
	}

	window, err := resolveDatastoreWindow(src, req, now)
	if err != nil {
		return "", nil, err
	}
	timeCol := ""
	if src.TimeColumn != "" {
		timeCol = qualified(datastoreTableAlias, src.TimeColumn)
	}
	if window.start != nil {
		window.startParam = q.bind(*window.start)
		conditions = append(conditions, timeCol+" >= "+window.startParam)
	}
	if window.end != nil {
		conditions = append(conditions, timeCol+" <= "+q.bind(*window.end))
	}

	for i, f := range req.Filters {
		cond, err := filterCondition(q, src, f, i)
		if err != nil {
			return "", nil, err
		}
		conditions = append(conditions, cond)
	}

	from, err := fromClause(src)
	if err != nil {
		return "", nil, err
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var sql string
	switch req.Shape {
	case datastoreShapeLatest:
		sql, err = latestStatement(src, req, columns, from, where)
	case datastoreShapeAggregate:
		sql, err = aggregateStatement(q, src, req, window, from, where)
	default:
		sql, err = rowsStatement(src, req, columns, from, where)
	}
	if err != nil {
		return "", nil, err
	}

	limit := q.bind(int64(req.PageSize) + 1)
	offset := q.bind(int64(req.Page-1) * int64(req.PageSize))
	return sql + " LIMIT " + limit + " OFFSET " + offset, q.args, nil
}

// checkShape rejects parameters that do not belong to the chosen shape.
func checkShape(src datastoreSource, req datastoreRequest) error {
	if req.Shape != datastoreShapeAggregate {
		if len(req.GroupBy) > 0 || req.Aggregation != "" || req.Buckets > 0 {
			return errors.New("group_by, aggregation and buckets apply only with shape aggregate")
		}
	}
	switch req.Shape {
	case datastoreShapeLatest:
		if src.Kind != datastoreKindMetric {
			return fmt.Errorf("shape latest applies only to metric sources, and %q is a configuration source", src.Name)
		}
	case datastoreShapeAggregate:
		if req.Aggregation == "" {
			return errors.New("aggregation is required with shape aggregate")
		}
		if req.Buckets > 0 && src.TimeColumn == "" {
			return fmt.Errorf("buckets needs a time column, and source %q has no time column", src.Name)
		}
	}
	return nil
}

// checkColumn returns the permitted column called name. A column the
// source does not list, including every credential column, is reported
// as unknown.
func checkColumn(src datastoreSource, name string) (datastoreColumn, error) {
	c, ok := src.column(name)
	if !ok || !metrics.IsValidIdentifier(name) {
		return datastoreColumn{}, fmt.Errorf("unknown column %q for source %q; call describe_datastore with this source to list its columns", name, src.Name)
	}
	return c, nil
}

func checkColumnList(src datastoreSource, names []string, param string) error {
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if _, err := checkColumn(src, name); err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("%s names column %q more than once", param, name)
		}
		seen[name] = true
	}
	return nil
}

// fromClause names the source table: schema-qualified for a metric
// source, and in the default schema for a configuration source.
func fromClause(src datastoreSource) (string, error) {
	switch src.Kind {
	case datastoreKindMetric:
		return "metrics." + metrics.QuoteIdentifier(src.Table) + " " + datastoreTableAlias, nil
	case datastoreKindConfiguration:
		return metrics.QuoteIdentifier(src.Table) + " " + datastoreTableAlias, nil
	default:
		return "", fmt.Errorf("source %q has an unknown kind", src.Name)
	}
}

// scopeCondition returns the predicate that limits src to the rows vis
// may read, or "" when no limit applies. It fails closed: a scope it
// does not know is an error rather than an unscoped query.
func scopeCondition(q *datastoreQuery, src datastoreSource, req datastoreRequest, vis visibleSet) (string, error) {
	switch src.Scope {
	case scopeConnection, scopeAlert, scopeViaAlert:
	case scopeHierarchy, scopeCluster, scopeGroup, scopeGlobal:
		if len(req.ConnectionIDs) > 0 {
			return "", fmt.Errorf("connection_ids does not apply to source %q; filter on its columns instead", src.Name)
		}
	default:
		return "", fmt.Errorf("source %q has no scoping rule", src.Name)
	}

	// ids is the set to scope to, and restricted says whether to scope
	// at all. Named connections must all be visible; the denial is the
	// same whether a connection is missing or merely hidden.
	var ids []int
	restricted := !vis.All
	if len(req.ConnectionIDs) > 0 {
		for _, id := range req.ConnectionIDs {
			if !vis.All && !slices.Contains(vis.IDs, id) {
				return "", errConnectionNotAccessible
			}
		}
		ids = slices.Clone(req.ConnectionIDs)
		restricted = true
	} else if restricted {
		ids = make([]int, 0, len(vis.IDs))
		ids = append(ids, vis.IDs...)
	}

	visible := func() string { return q.bind(ids) + "::int[]" }

	switch src.Scope {
	case scopeConnection:
		if !restricted {
			return "", nil
		}
		return qualified(datastoreTableAlias, src.ConnectionColumn) + " = ANY(" + visible() + ")", nil

	case scopeAlert:
		return alertCondition(q, datastoreTableAlias, req, vis, restricted, visible), nil

	case scopeViaAlert:
		cond := alertCondition(q, "a", req, vis, restricted, visible)
		if cond == "" {
			return "", nil
		}
		return "EXISTS (SELECT 1 FROM alerts a WHERE a.id = " +
			qualified(datastoreTableAlias, "alert_id") + " AND " + cond + ")", nil

	case scopeHierarchy:
		if !restricted {
			return "", nil
		}
		return hierarchyCondition(q, visible()), nil

	case scopeCluster:
		if !restricted {
			return "", nil
		}
		return "EXISTS (SELECT 1 FROM connections c WHERE c.cluster_id = " +
			qualified(datastoreTableAlias, "id") + " AND c.id = ANY(" + visible() + "))", nil

	case scopeGroup:
		if !restricted {
			return "", nil
		}
		return "EXISTS (SELECT 1 FROM connections c JOIN clusters cl ON cl.id = c.cluster_id WHERE cl.group_id = " +
			qualified(datastoreTableAlias, "id") + " AND c.id = ANY(" + visible() + "))", nil

	default:
		return "", nil
	}
}

// alertCondition mirrors the alert visibility of handleAlerts and
// get_alert_history (database.alertConnectionCondition): connection
// alerts follow the visible set, and system alerts, which have no
// connection, are shown only to a caller who may see them and who has
// not named particular connections.
func alertCondition(q *datastoreQuery, alias string, req datastoreRequest, vis visibleSet, restricted bool, visible func() string) string {
	includeSystem := vis.SystemAlerts && len(req.ConnectionIDs) == 0
	connCol := qualified(alias, "connection_id")
	if !restricted {
		if includeSystem {
			return ""
		}
		return connCol + " IS NOT NULL"
	}
	cond := connCol + " = ANY(" + visible() + ")"
	if includeSystem {
		cond = "(" + cond + " OR " + qualified(alias, "alert_type") + " = " + q.bind(database.AlertTypeSystem) + ")"
	}
	return cond
}

// hierarchyCondition admits a row of a scope-bearing table (blackouts,
// blackout_schedules, alert_thresholds, probe_configs) by the most
// specific reference it carries, as blackoutVisible and
// scopeVisibleToCaller do: a connection, a cluster with a visible
// member, a group with a cluster holding one, or nothing at all for an
// estate-wide row. A row carrying no reference whose scope claims to be
// narrower than the estate is malformed, and is hidden rather than
// treated as estate-wide.
func hierarchyCondition(q *datastoreQuery, visible string) string {
	conn := qualified(datastoreTableAlias, "connection_id")
	cluster := qualified(datastoreTableAlias, "cluster_id")
	group := qualified(datastoreTableAlias, "group_id")
	scope := qualified(datastoreTableAlias, "scope")
	estate := q.bind("estate")
	global := q.bind("global")

	return "((" + conn + " IS NULL AND " + cluster + " IS NULL AND " + group + " IS NULL AND " +
		scope + " IN (" + estate + ", " + global + "))" +
		" OR (" + conn + " IS NOT NULL AND " + conn + " = ANY(" + visible + "))" +
		" OR (" + conn + " IS NULL AND " + cluster + " IS NOT NULL AND EXISTS (SELECT 1 FROM connections c WHERE c.cluster_id = " +
		cluster + " AND c.id = ANY(" + visible + ")))" +
		" OR (" + conn + " IS NULL AND " + cluster + " IS NULL AND " + group + " IS NOT NULL AND EXISTS (SELECT 1 FROM connections c JOIN clusters cl ON cl.id = c.cluster_id WHERE cl.group_id = " +
		group + " AND c.id = ANY(" + visible + "))))"
}

// datastoreWindow is a resolved time range; a nil bound is open.
type datastoreWindow struct {
	start *time.Time
	end   *time.Time
	// startParam is the placeholder start was bound to, once bound.
	startParam string
}

// parseDatastoreTime reads a time_start or time_end value: "now", a
// relative duration before base ("1h", "7d") or an absolute timestamp.
func parseDatastoreTime(s, param string, now, base time.Time) (time.Time, error) {
	if strings.EqualFold(s, "now") {
		return now, nil
	}
	if d, err := parseRelativeDuration(s); err == nil {
		if d <= 0 {
			return time.Time{}, fmt.Errorf("invalid %s: a relative duration must be positive", param)
		}
		return base.Add(-d), nil
	}
	t, err := parseTimeArg(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: use a relative duration such as 1h or 7d, now, or an RFC 3339 timestamp", param)
	}
	return t, nil
}

// resolveDatastoreWindow works out the time range. A metric source is
// always bounded: it defaults to the hour before time_end (itself
// defaulting to now), and follows metrics.ResolveCustomWindow's rules.
// A configuration source is bounded only on the sides the caller gives,
// so future blackouts and the like stay reachable.
func resolveDatastoreWindow(src datastoreSource, req datastoreRequest, now time.Time) (datastoreWindow, error) {
	now = now.UTC()
	if src.TimeColumn == "" {
		if req.TimeStart != "" || req.TimeEnd != "" {
			return datastoreWindow{}, fmt.Errorf("source %q has no time column, so time_start and time_end do not apply", src.Name)
		}
		return datastoreWindow{}, nil
	}

	var start, end *time.Time
	base := now
	if req.TimeEnd != "" {
		t, err := parseDatastoreTime(req.TimeEnd, "time_end", now, now)
		if err != nil {
			return datastoreWindow{}, err
		}
		end = &t
		base = t
	}
	if req.TimeStart != "" {
		t, err := parseDatastoreTime(req.TimeStart, "time_start", now, base)
		if err != nil {
			return datastoreWindow{}, err
		}
		start = &t
	}

	if src.Kind != datastoreKindMetric {
		if start != nil && end != nil && !end.After(*start) {
			return datastoreWindow{}, errors.New("invalid time range: time_end must be after time_start")
		}
		return datastoreWindow{start: start, end: end}, nil
	}

	if end == nil {
		end = &now
	}
	if start == nil {
		s := end.Add(-time.Hour)
		start = &s
	}
	w, err := metrics.ResolveCustomWindowAt(*start, *end, now, "time_start", "time_end")
	if err != nil {
		return datastoreWindow{}, err
	}
	return datastoreWindow{start: &w.Start, end: &w.End}, nil
}

// Column type families, as information_schema.columns names them.
var (
	datastoreTextTypes    = []string{typeText, typeVarchar, "character", "name"}
	datastoreIntegerTypes = []string{"smallint", typeInteger, typeBigint}
	datastoreFloatTypes   = []string{typeReal, "double precision", "numeric"}
)

// filterCondition renders one filter, binding its value.
func filterCondition(q *datastoreQuery, src datastoreSource, f datastoreFilter, i int) (string, error) {
	where := fmt.Sprintf("filters[%d]", i)
	c, err := checkColumn(src, f.Column)
	if err != nil {
		return "", err
	}
	column := qualified(datastoreTableAlias, c.Name)
	op := datastoreOperators[f.Op]

	switch f.Op {
	case "is null", "is not null":
		if f.HasValue {
			return "", fmt.Errorf("%s: operator %s takes no value", where, f.Op)
		}
		return column + " " + op, nil

	case "in", "not in":
		list, ok := f.Value.([]any)
		if !ok || len(list) == 0 {
			return "", fmt.Errorf("%s: operator %s needs a non-empty array value", where, f.Op)
		}
		if len(list) > datastoreMaxInValues {
			return "", fmt.Errorf("%s: operator %s takes at most %d values", where, f.Op, datastoreMaxInValues)
		}
		placeholders := make([]string, len(list))
		for j, item := range list {
			v, err := coerceFilterValue(c, item, where)
			if err != nil {
				return "", err
			}
			placeholders[j] = q.bind(v)
		}
		return column + " " + op + " (" + strings.Join(placeholders, ", ") + ")", nil
	}

	if !f.HasValue {
		return "", fmt.Errorf("%s: operator %s needs a value", where, f.Op)
	}
	if f.Value == nil {
		return "", fmt.Errorf("%s: to match a missing value use the is null operator", where)
	}
	if f.Op == "like" {
		if !slices.Contains(datastoreTextTypes, c.DataType) {
			return "", fmt.Errorf("%s: operator like needs a text column, and %q is %s", where, c.Name, c.DataType)
		}
		if _, ok := f.Value.(string); !ok {
			return "", fmt.Errorf("%s: operator like needs a string value", where)
		}
	}
	v, err := coerceFilterValue(c, f.Value, where)
	if err != nil {
		return "", err
	}
	return column + " " + op + " " + q.bind(v), nil
}

// coerceFilterValue converts a JSON scalar to a Go value of the column's
// type, so the parameter encodes cleanly.
func coerceFilterValue(c datastoreColumn, v any, where string) (any, error) {
	switch v.(type) {
	case string, bool, float64, int, int64, json.Number:
	default:
		return nil, fmt.Errorf("%s: a filter value must be a scalar (string, number or boolean)", where)
	}

	switch {
	case slices.Contains(datastoreIntegerTypes, c.DataType):
		if s, ok := v.(string); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: column %q needs an integer value", where, c.Name)
			}
			return n, nil
		}
		if _, ok := v.(bool); ok {
			return nil, fmt.Errorf("%s: column %q needs an integer value", where, c.Name)
		}
		n, ok := integerValue(v)
		if !ok {
			return nil, fmt.Errorf("%s: column %q needs an integer value", where, c.Name)
		}
		return n, nil

	case slices.Contains(datastoreFloatTypes, c.DataType):
		switch n := v.(type) {
		case float64:
			return n, nil
		case int:
			return float64(n), nil
		case int64:
			return float64(n), nil
		case json.Number:
			if f, err := n.Float64(); err == nil {
				return f, nil
			}
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return f, nil
			}
		}
		return nil, fmt.Errorf("%s: column %q needs a number value", where, c.Name)

	case c.DataType == typeBoolean:
		switch b := v.(type) {
		case bool:
			return b, nil
		case string:
			if parsed, err := strconv.ParseBool(strings.TrimSpace(b)); err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("%s: column %q needs a boolean value", where, c.Name)

	default:
		// Text columns, and every other type (times, intervals, LSNs,
		// addresses, JSON), take the value as text; PostgreSQL parses it
		// as the column's type, so pgx never has to encode a number into
		// a type it has no plan for.
		switch s := v.(type) {
		case float64:
			return strconv.FormatFloat(s, 'f', -1, 64), nil
		case int:
			return strconv.Itoa(s), nil
		case int64:
			return strconv.FormatInt(s, 10), nil
		case json.Number:
			return s.String(), nil
		case bool:
			return strconv.FormatBool(s), nil
		}
		return v, nil
	}
}

// selectList renders columns qualified with alias, or bare when alias
// is empty.
func selectList(alias string, columns []string) string {
	parts := make([]string, len(columns))
	for i, c := range columns {
		if alias == "" {
			parts[i] = metrics.QuoteIdentifier(c)
		} else {
			parts[i] = qualified(alias, c)
		}
	}
	return strings.Join(parts, ", ")
}

// orderTerms renders the caller's ordering; each column must pass check.
func orderTerms(order []datastoreOrder, alias string, check func(string) error) (string, error) {
	parts := make([]string, len(order))
	for i, o := range order {
		if err := check(o.Column); err != nil {
			return "", err
		}
		name := metrics.QuoteIdentifier(o.Column)
		if alias != "" {
			name = qualified(alias, o.Column)
		}
		parts[i] = name + " " + strings.ToUpper(o.Direction)
	}
	return strings.Join(parts, ", "), nil
}

func sourceColumnCheck(src datastoreSource) func(string) error {
	return func(name string) error {
		_, err := checkColumn(src, name)
		return err
	}
}

// rowsStatement selects filtered rows, newest first by default.
func rowsStatement(src datastoreSource, req datastoreRequest, columns []string, from, where string) (string, error) {
	order := ""
	if len(req.OrderBy) > 0 {
		terms, err := orderTerms(req.OrderBy, datastoreTableAlias, sourceColumnCheck(src))
		if err != nil {
			return "", err
		}
		order = terms
	} else if src.TimeColumn != "" {
		order = qualified(datastoreTableAlias, src.TimeColumn) + " DESC"
	} else {
		order = qualified(datastoreTableAlias, src.Columns[0].Name) + " ASC"
	}
	return "SELECT " + selectList(datastoreTableAlias, columns) + " FROM " + from + where + " ORDER BY " + order, nil
}

// latestStatement selects the newest row per connection and entity key,
// using the same DISTINCT ON approach and metrics.EntityKeyColumns as
// the metrics package's latest-row queries. The window and filters apply
// inside, before the newest row is picked.
func latestStatement(src datastoreSource, req datastoreRequest, columns []string, from, where string) (string, error) {
	all := src.columnNames()
	types := make(map[string]string, len(src.Columns))
	for _, c := range src.Columns {
		types[c.Name] = c.DataType
	}
	distinct := append([]string{src.ConnectionColumn}, metrics.EntityKeyColumns(all, types)...)

	inner := "SELECT DISTINCT ON (" + selectList(datastoreTableAlias, distinct) + ") " +
		selectList(datastoreTableAlias, all) + " FROM " + from + where +
		" ORDER BY " + selectList(datastoreTableAlias, distinct) + ", " +
		qualified(datastoreTableAlias, src.TimeColumn) + " DESC"

	var order string
	if len(req.OrderBy) > 0 {
		terms, err := orderTerms(req.OrderBy, "", sourceColumnCheck(src))
		if err != nil {
			return "", err
		}
		order = terms
	} else {
		parts := make([]string, len(distinct))
		for i, c := range distinct {
			parts[i] = metrics.QuoteIdentifier(c) + " ASC"
		}
		order = strings.Join(parts, ", ")
	}
	return "SELECT " + selectList("", columns) + " FROM (" + inner + ") latest ORDER BY " + order, nil
}

// aggregateStatement groups rows and applies req.Aggregation to the
// requested columns, optionally in time buckets of
// metrics.BucketWidth's width starting at the window's start.
func aggregateStatement(q *datastoreQuery, src datastoreSource, req datastoreRequest, window datastoreWindow, from, where string) (string, error) {
	var outputs, selects []string

	if req.Buckets > 0 {
		if window.start == nil || window.end == nil {
			return "", errors.New("buckets on a configuration source need both time_start and time_end")
		}
		width := metrics.BucketWidth(*window.start, *window.end, req.Buckets)
		widthParam := q.bind(fmt.Sprintf("%d microseconds", width.Microseconds()))
		// The window start's parameter doubles as the bucket origin.
		origin := window.startParam
		selects = append(selects, "date_bin("+widthParam+"::interval, "+
			qualified(datastoreTableAlias, src.TimeColumn)+", "+origin+"::timestamptz) AS "+
			metrics.QuoteIdentifier(datastoreBucketAlias))
		outputs = append(outputs, datastoreBucketAlias)
	}

	for _, g := range req.GroupBy {
		if slices.Contains(req.Columns, g) {
			return "", fmt.Errorf("column %q is in both group_by and columns", g)
		}
		selects = append(selects, qualified(datastoreTableAlias, g))
		outputs = append(outputs, g)
	}
	groupCount := len(selects)

	if len(req.Columns) == 0 {
		if req.Aggregation != "count" {
			return "", fmt.Errorf("columns is required with aggregation %s", req.Aggregation)
		}
		selects = append(selects, "count(*) AS "+metrics.QuoteIdentifier("count"))
		outputs = append(outputs, "count")
	}
	for _, name := range req.Columns {
		c, _ := src.column(name)
		switch req.Aggregation {
		case "avg", "sum":
			if !c.IsMetric {
				return "", fmt.Errorf("aggregation %s needs a numeric metric column, and %q is not one", req.Aggregation, name)
			}
		case "min", "max":
			if c.DataType == typeJSONB || c.DataType == typeBoolean {
				return "", fmt.Errorf("cannot aggregate %s column %q with %s", c.DataType, name, req.Aggregation)
			}
		}
		alias := req.Aggregation + "_" + name
		selects = append(selects, req.Aggregation+"("+qualified(datastoreTableAlias, name)+") AS "+metrics.QuoteIdentifier(alias))
		outputs = append(outputs, alias)
	}

	sql := "SELECT " + strings.Join(selects, ", ") + " FROM " + from + where
	if groupCount > 0 {
		positions := make([]string, groupCount)
		for i := range positions {
			positions[i] = strconv.Itoa(i + 1)
		}
		sql += " GROUP BY " + strings.Join(positions, ", ")
	}

	var order string
	if len(req.OrderBy) > 0 {
		terms, err := orderTerms(req.OrderBy, "", func(name string) error {
			if !slices.Contains(outputs, name) {
				return fmt.Errorf("order_by column %q is not an output of this aggregate; use one of %s", name, strings.Join(outputs, ", "))
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		order = terms
	} else if groupCount > 0 {
		parts := make([]string, groupCount)
		for i := range parts {
			parts[i] = metrics.QuoteIdentifier(outputs[i]) + " ASC"
		}
		order = strings.Join(parts, ", ")
	}
	if order != "" {
		sql += " ORDER BY " + order
	}
	return sql, nil
}
