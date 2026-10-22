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
	// datastoreMaxRows bounds page * page_size, so a caller cannot page
	// deep into a large source and make the server sort and skip an
	// unbounded number of rows. It also bounds page by itself.
	datastoreMaxRows     = 100000
	datastoreMaxPage     = datastoreMaxRows
	datastoreMaxBuckets  = 500
	datastoreMaxInValues = 1000
	// datastoreMaxListItems bounds filters, order_by and group_by. With
	// datastoreMaxInValues it keeps a statement's bind parameters well
	// under PostgreSQL's limit of 65535.
	datastoreMaxListItems = 20
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
// JSON value for a scalar operator, and Values the list for in and not
// in; both are only ever bound as parameters. The input schema declares
// them as a string and an array of strings, because some model
// providers refuse a property without a type, but JSON numbers and
// booleans are still accepted.
type datastoreFilter struct {
	Column string
	Op     string
	Value  any
	// HasValue records whether the caller supplied a value at all, so a
	// missing value and an explicit null get distinct errors.
	HasValue bool
	Values   any
	// HasValues records whether the caller supplied values at all.
	HasValues bool
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

// datastoreParameterHints suggests the parameter a caller probably meant
// when it passes one query_datastore does not accept.
var datastoreParameterHints = map[string]string{
	"connection_id": "did you mean connection_ids (an array)?",
	"limit":         "use page_size and page",
	"offset":        "use page_size and page",
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
	steps := []func(map[string]any) error{
		checkDatastoreRequestKeys,
		req.parseSource,
		req.parseLists,
		req.parseShapeOptions,
		req.parsePaging,
		req.parseExplain,
	}
	for _, step := range steps {
		if err := step(args); err != nil {
			return req, err
		}
	}
	return req, nil
}

// checkDatastoreRequestKeys refuses any parameter query_datastore does
// not accept, other than the registry's "__" keys.
func checkDatastoreRequestKeys(args map[string]any) error {
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if strings.HasPrefix(key, "__") || datastoreRequestKeys[key] {
			continue
		}
		// A null connection_id names nothing, so it is ignored as it
		// is by the other tools rather than refused as unknown.
		if key == "connection_id" && args[key] == nil {
			continue
		}
		if key == "query" || key == "sql" {
			return errors.New("query_datastore no longer accepts SQL; describe the query with source, columns, filters and the other parameters, and call describe_datastore to list the sources")
		}
		if hint, ok := datastoreParameterHints[key]; ok {
			return fmt.Errorf("unknown parameter %q; %s", key, hint)
		}
		return fmt.Errorf("unknown parameter %q", key)
	}
	return nil
}

// parseSource reads the required source parameter.
func (req *datastoreRequest) parseSource(args map[string]any) error {
	source, ok := args["source"]
	if !ok {
		return errors.New("source is required; call describe_datastore to list the sources")
	}
	name, ok := source.(string)
	if !ok {
		return errors.New("source must be a string")
	}
	req.Source = strings.TrimSpace(name)
	if req.Source == "" {
		return errors.New("source is required; call describe_datastore to list the sources")
	}
	return nil
}

// parseLists reads the list and time range parameters, in the order
// their errors are reported.
func (req *datastoreRequest) parseLists(args map[string]any) error {
	var err error
	if req.Columns, err = parseStringList(args, "columns"); err != nil {
		return err
	}
	if req.GroupBy, err = parseStringList(args, "group_by"); err != nil {
		return err
	}
	if len(req.GroupBy) > datastoreMaxListItems {
		return fmt.Errorf("group_by takes at most %d columns", datastoreMaxListItems)
	}
	if req.ConnectionIDs, err = parseConnectionIDs(args); err != nil {
		return err
	}
	if req.TimeStart, err = parseOptionalString(args, "time_start"); err != nil {
		return err
	}
	if req.TimeEnd, err = parseOptionalString(args, "time_end"); err != nil {
		return err
	}
	return req.parseClauses(args)
}

// parseClauses reads the filters and order_by parameters.
func (req *datastoreRequest) parseClauses(args map[string]any) error {
	var err error
	if req.Filters, err = parseDatastoreFilters(args); err != nil {
		return err
	}
	if len(req.Filters) > datastoreMaxListItems {
		return fmt.Errorf("filters takes at most %d conditions", datastoreMaxListItems)
	}
	if req.OrderBy, err = parseDatastoreOrder(args); err != nil {
		return err
	}
	if len(req.OrderBy) > datastoreMaxListItems {
		return fmt.Errorf("order_by takes at most %d terms", datastoreMaxListItems)
	}
	return nil
}

// parseShapeOptions reads the shape, aggregation and buckets
// parameters.
func (req *datastoreRequest) parseShapeOptions(args map[string]any) error {
	if v, ok := args["shape"]; ok {
		shape := stringValue(v)
		shape = strings.ToLower(strings.TrimSpace(shape))
		if shape != datastoreShapeRows && shape != datastoreShapeLatest && shape != datastoreShapeAggregate {
			return errors.New("shape must be one of rows, latest or aggregate")
		}
		req.Shape = shape
	}
	if v, ok := args["aggregation"]; ok {
		agg := stringValue(v)
		agg = strings.ToLower(strings.TrimSpace(agg))
		if !datastoreAggregations[agg] {
			return errors.New("aggregation must be one of avg, sum, min, max or count")
		}
		req.Aggregation = agg
	}
	return parseBoundedInteger(args, "buckets", datastoreMaxBuckets, &req.Buckets)
}

// parsePaging reads the page and page_size parameters and checks that
// together they stay within datastoreMaxRows.
func (req *datastoreRequest) parsePaging(args map[string]any) error {
	if err := parseBoundedInteger(args, "page", datastoreMaxPage, &req.Page); err != nil {
		return err
	}
	if err := parseBoundedInteger(args, "page_size", datastoreMaxPageSize, &req.PageSize); err != nil {
		return err
	}
	return checkDatastorePaging(req.Page, req.PageSize)
}

// parseExplain reads the explain parameter.
func (req *datastoreRequest) parseExplain(args map[string]any) error {
	v, ok := args["explain"]
	if !ok {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return errors.New("explain must be a boolean")
	}
	req.Explain = b
	return nil
}

// parseBoundedInteger stores the integer parameter name in dst when the
// caller gives it, refusing a value outside 1 to limit. dst keeps its
// default when the parameter is absent.
func parseBoundedInteger(args map[string]any, name string, limit int64, dst *int) error {
	v, ok := args[name]
	if !ok {
		return nil
	}
	n, ok := integerValue(v)
	if !ok || n < 1 || n > limit {
		return fmt.Errorf("%s must be an integer between 1 and %d", name, limit)
	}
	*dst = int(n)
	return nil
}

// checkDatastorePaging refuses a page that reaches beyond the first
// datastoreMaxRows rows of a result.
func checkDatastorePaging(page, pageSize int) error {
	if int64(page)*int64(pageSize) > datastoreMaxRows {
		return fmt.Errorf("page * page_size must be at most %d; narrow the query with filters or a shorter time range instead of paging further", datastoreMaxRows)
	}
	return nil
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
	// A null counts as absent, as several LLM providers send null for
	// an optional parameter they mean to omit.
	v := args["connection_ids"]
	if v == nil {
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
		if err := checkObjectKeys(obj, where, "column", "op", "value", "values"); err != nil {
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
		values, hasValues := obj["values"]
		filters = append(filters, datastoreFilter{
			Column: column, Op: op,
			Value: value, HasValue: hasValue,
			Values: values, HasValues: hasValues,
		})
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
	if err := checkDatastoreRequest(src, req); err != nil {
		return "", nil, err
	}
	columns, err := datastoreColumns(src, req)
	if err != nil {
		return "", nil, err
	}

	q := &datastoreQuery{}
	// inner holds the predicates the server builds (the caller's scope and
	// the time window), and conditions the caller's own filters.
	inner, window, err := serverConditions(q, src, req, vis, now)
	if err != nil {
		return "", nil, err
	}
	conditions, err := filterConditions(q, src, req.Filters)
	if err != nil {
		return "", nil, err
	}

	from, err := scopedFrom(src, inner)
	if err != nil {
		return "", nil, err
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	sql, err := shapeStatement(q, src, req, columns, window, from, where)
	if err != nil {
		return "", nil, err
	}

	limit := q.bind(int64(req.PageSize) + 1)
	offset := q.bind(int64(req.Page-1) * int64(req.PageSize))
	return sql + " LIMIT " + limit + " OFFSET " + offset, q.args, nil
}

// checkDatastoreRequest repeats parseDatastoreRequest's limits, so a
// request built any other way is held to them too, and checks that the
// request suits src.
func checkDatastoreRequest(src datastoreSource, req datastoreRequest) error {
	if req.Source != src.Name {
		return fmt.Errorf("request source %q does not match %q", req.Source, src.Name)
	}
	if req.PageSize < 1 || req.PageSize > datastoreMaxPageSize {
		return fmt.Errorf("page_size must be an integer between 1 and %d", datastoreMaxPageSize)
	}
	if req.Page < 1 || req.Page > datastoreMaxPage {
		return fmt.Errorf("page must be an integer between 1 and %d", datastoreMaxPage)
	}
	if err := checkDatastorePaging(req.Page, req.PageSize); err != nil {
		return err
	}
	if len(req.Filters) > datastoreMaxListItems || len(req.OrderBy) > datastoreMaxListItems || len(req.GroupBy) > datastoreMaxListItems {
		return fmt.Errorf("filters, order_by and group_by take at most %d entries each", datastoreMaxListItems)
	}
	return checkShape(src, req)
}

// datastoreColumns returns the columns the statement selects: the
// caller's list, or every column of src for a shape other than
// aggregate. It also checks the group_by columns.
func datastoreColumns(src datastoreSource, req datastoreRequest) ([]string, error) {
	columns := req.Columns
	if len(columns) == 0 && req.Shape != datastoreShapeAggregate {
		columns = src.columnNames()
	}
	if err := checkColumnList(src, columns, "columns"); err != nil {
		return nil, err
	}
	if err := checkColumnList(src, req.GroupBy, "group_by"); err != nil {
		return nil, err
	}
	return columns, nil
}

// serverConditions returns the predicates the server applies itself:
// the caller's scope, then the time window. It binds them in that order.
func serverConditions(q *datastoreQuery, src datastoreSource, req datastoreRequest, vis visibleSet, now time.Time) ([]string, datastoreWindow, error) {
	var inner []string
	scope, err := scopeCondition(q, src, req, vis)
	if err != nil {
		return nil, datastoreWindow{}, err
	}
	if scope != "" {
		inner = append(inner, scope)
	}
	if src.LiveColumn != "" {
		inner = append(inner, qualified(datastoreTableAlias, src.LiveColumn)+" IS NULL")
	} else if cond := liveConnectionCondition(src); cond != "" {
		inner = append(inner, cond)
	}

	window, err := resolveDatastoreWindow(src, req, now)
	if err != nil {
		return nil, datastoreWindow{}, err
	}
	timeCol := ""
	if src.TimeColumn != "" {
		timeCol = qualified(datastoreTableAlias, src.TimeColumn)
	}
	if window.start != nil {
		window.startParam = q.bind(*window.start)
		inner = append(inner, timeCol+" >= "+window.startParam)
	}
	if window.end != nil {
		inner = append(inner, timeCol+" <= "+q.bind(*window.end))
	}
	return inner, window, nil
}

// liveConnectionCondition returns the predicate that keeps a
// connection-scoped source to rows whose connection is live, or "" for
// a source with no connection column. The visible set already leaves
// out a connection marked deleted, but a caller who may read every
// connection is not scoped by it at all, and a connection marked after
// the set was read would otherwise slip through; this holds for every
// caller (issue #669). It also hides rows a metrics table without a
// foreign key still holds for a connection already purged. It binds
// nothing, so it leaves the parameter numbering alone.
func liveConnectionCondition(src datastoreSource) string {
	if src.Scope != scopeConnection || src.ConnectionColumn == "" {
		return ""
	}
	return "EXISTS (SELECT 1 FROM public.connections lc WHERE lc.id = " +
		qualified(datastoreTableAlias, src.ConnectionColumn) +
		" AND lc.deleted_at IS NULL)"
}

// filterConditions renders the caller's filters, binding their values
// in order.
func filterConditions(q *datastoreQuery, src datastoreSource, filters []datastoreFilter) ([]string, error) {
	var conditions []string
	for i, f := range filters {
		cond, err := filterCondition(q, src, f, i)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, cond)
	}
	return conditions, nil
}

// shapeStatement builds the statement for req.Shape, without its LIMIT
// and OFFSET.
func shapeStatement(q *datastoreQuery, src datastoreSource, req datastoreRequest, columns []string, window datastoreWindow, from, where string) (string, error) {
	switch req.Shape {
	case datastoreShapeLatest:
		return latestStatement(src, req, columns, from, where)
	case datastoreShapeAggregate:
		return aggregateStatement(q, src, req, window, from, where)
	default:
		return rowsStatement(src, req, columns, from, where)
	}
}

// errDatastoreExplainRestricted refuses explain to a caller who cannot
// access every connection.
var errDatastoreExplainRestricted = errors.New("explain is available only to callers who can access every connection")

// checkDatastoreExplain refuses explain unless vis covers every
// connection. The planner's row estimates come from statistics over the
// whole table, hidden connections' rows included, so a plan for a
// filter such as datname LIKE 'secr%' tells the caller how many such
// rows exist anywhere, whatever the scope predicate says.
func checkDatastoreExplain(req datastoreRequest, vis visibleSet) error {
	if req.Explain && !vis.All {
		return errDatastoreExplainRestricted
	}
	return nil
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

// fromClause names the source table, schema-qualified: metric sources
// live in the metrics schema and configuration sources in public, where
// the collector's migrations create them. Qualifying the name keeps a
// table of the same name earlier on the search_path from being read.
func fromClause(src datastoreSource) (string, error) {
	switch src.Kind {
	case datastoreKindMetric:
		return "metrics." + metrics.QuoteIdentifier(src.Table) + " " + datastoreTableAlias, nil
	case datastoreKindConfiguration:
		return "public." + metrics.QuoteIdentifier(src.Table) + " " + datastoreTableAlias, nil
	default:
		return "", fmt.Errorf("source %q has an unknown kind", src.Name)
	}
}

// scopedFrom returns the FROM item every shape reads. When the server
// has predicates of its own (the caller's scope and the time window) it
// applies them in a subquery with OFFSET 0, which PostgreSQL neither
// flattens into the outer query nor pushes outer predicates into. The
// caller's filters therefore only ever see rows the caller may read: a
// filter that would fail on some value (a malformed LIKE pattern, say)
// cannot fail on a hidden row, and so cannot reveal that the row exists.
// The time window sits inside too, because a predicate outside the
// subquery could not prune metric partitions or use the
// (connection_id, collected_at) index.
//
// The subquery selects the source's permitted columns under the same
// alias, so the outer statement refers to them exactly as it would to
// the table.
//
// A column marked for redaction is masked here too, so the caller's
// filters, grouping and ordering only ever see the masked value and
// cannot be used to guess the real one.
func scopedFrom(src datastoreSource, inner []string) (string, error) {
	from, err := fromClause(src)
	if err != nil {
		return "", err
	}
	redacts := slices.ContainsFunc(src.Columns, func(c datastoreColumn) bool {
		return c.RedactBy != "" || c.MaskOptions || c.MaskWhole || c.RedactText
	})
	if len(inner) == 0 && !redacts {
		return from, nil
	}
	where := ""
	if len(inner) > 0 {
		where = " WHERE " + strings.Join(inner, " AND ")
	}
	return "(SELECT " + innerSelectList(src) + " FROM " + from + where + " OFFSET 0) " +
		datastoreTableAlias, nil
}

// innerSelectList renders scopedFrom's select list: each permitted
// column, with a redacted column replaced by metrics.RedactedValue on rows that
// name a credential-bearing setting, and the credential-bearing options
// in an options column masked, and known password forms in free text
// masked. An empty or NULL value is kept, since it shows only that the
// setting is unset. Each statement-text column follows, as stored, under
// metrics.RawStatementAlias.
func innerSelectList(src datastoreSource) string {
	parts := make([]string, len(src.Columns))
	for i, c := range src.Columns {
		value := qualified(datastoreTableAlias, c.Name)
		if c.MaskOptions {
			parts[i] = metrics.MaskedOptions(value) + " AS " + metrics.QuoteIdentifier(c.Name)
			continue
		}
		if c.MaskWhole {
			parts[i] = metrics.MaskedWhole(value) + " AS " + metrics.QuoteIdentifier(c.Name)
			continue
		}
		if c.RedactText {
			parts[i] = metrics.RedactedText(value) + " AS " + metrics.QuoteIdentifier(c.Name)
			continue
		}
		if c.RedactBy == "" {
			parts[i] = value
			continue
		}
		parts[i] = metrics.MaskedSetting(value, qualified(datastoreTableAlias, c.RedactBy)) + " AS " +
			metrics.QuoteIdentifier(c.Name)
	}
	// Each statement-text column is also selected as stored, for
	// redactDatastoreStatements to mask in Go. No caller-supplied name
	// can refer to it, since checkColumn accepts only source columns.
	for _, c := range src.Columns {
		if c.RedactStatement {
			parts = append(parts, qualified(datastoreTableAlias, c.Name)+" AS "+
				metrics.QuoteIdentifier(metrics.RawStatementAlias(c.Name)))
		}
	}
	return strings.Join(parts, ", ")
}

// scopeCondition returns the predicate that limits src to the rows vis
// may read, or "" when no limit applies. It fails closed: a scope it
// does not know is an error rather than an unscoped query.
func scopeCondition(q *datastoreQuery, src datastoreSource, req datastoreRequest, vis visibleSet) (string, error) {
	if err := checkScopeRule(src, req); err != nil {
		return "", err
	}
	ids, restricted, err := scopeIDs(req, vis)
	if err != nil {
		return "", err
	}
	visible := func() string { return q.bind(ids) + "::int[]" }

	switch src.Scope {
	case scopeAlert:
		return alertCondition(q, datastoreTableAlias, req, vis, restricted, visible), nil
	case scopeViaAlert:
		return viaAlertCondition(q, req, vis, restricted, visible), nil
	}
	if !restricted {
		return "", nil
	}
	return restrictedScopeCondition(q, src, visible), nil
}

// checkScopeRule fails closed on a source with no known scoping rule,
// and refuses connection_ids for a source whose rule does not scope by
// one connection column.
func checkScopeRule(src datastoreSource, req datastoreRequest) error {
	switch src.Scope {
	case scopeConnection, scopeAlert, scopeViaAlert:
		return nil
	case scopeHierarchy, scopeCluster, scopeGroup, scopeGlobal:
		if len(req.ConnectionIDs) > 0 {
			return fmt.Errorf("connection_ids does not apply to source %q; filter on its columns instead", src.Name)
		}
		return nil
	default:
		return fmt.Errorf("source %q has no scoping rule", src.Name)
	}
}

// scopeIDs returns the set of connections to scope to, and whether to
// scope at all. Named connections must all be visible; the denial is the
// same whether a connection is missing or merely hidden.
func scopeIDs(req datastoreRequest, vis visibleSet) ([]int, bool, error) {
	if len(req.ConnectionIDs) > 0 {
		for _, id := range req.ConnectionIDs {
			if !vis.All && !slices.Contains(vis.IDs, id) {
				return nil, false, errConnectionNotAccessible
			}
		}
		return slices.Clone(req.ConnectionIDs), true, nil
	}
	if vis.All {
		return nil, false, nil
	}
	ids := make([]int, 0, len(vis.IDs))
	ids = append(ids, vis.IDs...)
	return ids, true, nil
}

// viaAlertCondition admits a row whose alert passes alertCondition.
func viaAlertCondition(q *datastoreQuery, req datastoreRequest, vis visibleSet, restricted bool, visible func() string) string {
	cond := alertCondition(q, "a", req, vis, restricted, visible)
	if cond == "" {
		return ""
	}
	return "EXISTS (SELECT 1 FROM public.alerts a WHERE a.id = " +
		qualified(datastoreTableAlias, "alert_id") + " AND " + cond + ")"
}

// restrictedScopeCondition returns the predicate of a connection,
// hierarchy, cluster or group scope for a caller limited to the visible
// set. Each case binds the visible set itself, so a scope that does not
// use it binds nothing.
func restrictedScopeCondition(q *datastoreQuery, src datastoreSource, visible func() string) string {
	switch src.Scope {
	case scopeConnection:
		return qualified(datastoreTableAlias, src.ConnectionColumn) + " = ANY(" + visible() + ")"
	case scopeHierarchy:
		return hierarchyCondition(q, visible())
	case scopeCluster:
		return "EXISTS (SELECT 1 FROM public.connections c WHERE c.cluster_id = " +
			qualified(datastoreTableAlias, "id") + " AND c.id = ANY(" + visible() + "))"
	case scopeGroup:
		return "EXISTS (SELECT 1 FROM public.connections c JOIN public.clusters cl ON cl.id = c.cluster_id WHERE cl.group_id = " +
			qualified(datastoreTableAlias, "id") + " AND c.id = ANY(" + visible() + "))"
	default:
		return ""
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
		" OR (" + conn + " IS NULL AND " + cluster + " IS NOT NULL AND EXISTS (SELECT 1 FROM public.connections c WHERE c.cluster_id = " +
		cluster + " AND c.id = ANY(" + visible + ")))" +
		" OR (" + conn + " IS NULL AND " + cluster + " IS NULL AND " + group + " IS NOT NULL AND EXISTS (SELECT 1 FROM public.connections c JOIN public.clusters cl ON cl.id = c.cluster_id WHERE cl.group_id = " +
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

	start, end, err := parseDatastoreBounds(req, now)
	if err != nil {
		return datastoreWindow{}, err
	}

	if src.Kind != datastoreKindMetric {
		if start != nil && end != nil && !end.After(*start) {
			return datastoreWindow{}, errors.New("invalid time range: time_end must be after time_start")
		}
		return datastoreWindow{start: start, end: end}, nil
	}
	return metricDatastoreWindow(start, end, now)
}

// parseDatastoreBounds reads time_end, then time_start, which counts back
// from time_end when it is relative. An absent bound is nil.
func parseDatastoreBounds(req datastoreRequest, now time.Time) (*time.Time, *time.Time, error) {
	var start, end *time.Time
	base := now
	if req.TimeEnd != "" {
		t, err := parseDatastoreTime(req.TimeEnd, "time_end", now, now)
		if err != nil {
			return nil, nil, err
		}
		end = &t
		base = t
	}
	if req.TimeStart != "" {
		t, err := parseDatastoreTime(req.TimeStart, "time_start", now, base)
		if err != nil {
			return nil, nil, err
		}
		start = &t
	}
	return start, end, nil
}

// metricDatastoreWindow bounds a metric source's window: end defaults to
// now and start to the hour before end, and the result follows
// metrics.ResolveCustomWindow's rules.
func metricDatastoreWindow(start, end *time.Time, now time.Time) (datastoreWindow, error) {
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
		if f.HasValue || f.HasValues {
			return "", fmt.Errorf("%s: operator %s takes no value or values", where, f.Op)
		}
		return column + " " + op, nil
	case "in", "not in":
		return listFilterCondition(q, c, f, column+" "+op, where)
	default:
		return scalarFilterCondition(q, c, f, column+" "+op, where)
	}
}

// listFilterCondition renders an in or not in filter, binding each of
// its values. lhs is the rendered column and operator.
func listFilterCondition(q *datastoreQuery, c datastoreColumn, f datastoreFilter, lhs, where string) (string, error) {
	if f.HasValue {
		return "", fmt.Errorf("%s: operator %s takes values (an array), not value", where, f.Op)
	}
	list, ok := f.Values.([]any)
	if !ok || len(list) == 0 {
		return "", fmt.Errorf("%s: operator %s needs values, a non-empty array", where, f.Op)
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
	return lhs + " (" + strings.Join(placeholders, ", ") + ")", nil
}

// scalarFilterCondition renders a filter that compares the column with
// one value, binding it. lhs is the rendered column and operator.
func scalarFilterCondition(q *datastoreQuery, c datastoreColumn, f datastoreFilter, lhs, where string) (string, error) {
	if f.HasValues {
		return "", fmt.Errorf("%s: operator %s takes value, not values (values is only for in and not in)", where, f.Op)
	}
	if !f.HasValue {
		return "", fmt.Errorf("%s: operator %s needs a value", where, f.Op)
	}
	if f.Value == nil {
		return "", fmt.Errorf("%s: to match a missing value use the is null operator", where)
	}
	if f.Op == "like" {
		if err := checkLikeFilter(c, f.Value, where); err != nil {
			return "", err
		}
	}
	v, err := coerceFilterValue(c, f.Value, where)
	if err != nil {
		return "", err
	}
	return lhs + " " + q.bind(v), nil
}

// checkLikeFilter refuses a like filter on a column that is not text, or
// whose pattern is not a string or ends with a lone escape character.
func checkLikeFilter(c datastoreColumn, value any, where string) error {
	if !slices.Contains(datastoreTextTypes, c.DataType) {
		return fmt.Errorf("%s: operator like needs a text column, and %q is %s", where, c.Name, c.DataType)
	}
	pattern, ok := value.(string)
	if !ok {
		return fmt.Errorf("%s: operator like needs a string value", where)
	}
	if endsWithEscape(pattern) {
		return fmt.Errorf(`%s: a like pattern must not end with a single backslash; write \\ to match a literal backslash`, where)
	}
	return nil
}

// endsWithEscape reports whether pattern ends in an odd-length run of
// backslashes, which leaves LIKE's escape character with nothing to
// escape. PostgreSQL rejects such a pattern only when it compares it with
// a row, so the error would otherwise depend on the data.
func endsWithEscape(pattern string) bool {
	run := len(pattern) - len(strings.TrimRight(pattern, `\`))
	return run%2 == 1
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
		return coerceIntegerValue(c, v, where)
	case slices.Contains(datastoreFloatTypes, c.DataType):
		return coerceFloatValue(c, v, where)
	case c.DataType == typeBoolean:
		return coerceBooleanValue(c, v, where)
	default:
		return coerceTextValue(v), nil
	}
}

// coerceIntegerValue converts a filter value for an integer column.
func coerceIntegerValue(c datastoreColumn, v any, where string) (any, error) {
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
}

// coerceFloatValue converts a filter value for a floating-point or
// numeric column.
func coerceFloatValue(c datastoreColumn, v any, where string) (any, error) {
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
}

// coerceBooleanValue converts a filter value for a boolean column.
func coerceBooleanValue(c datastoreColumn, v any, where string) (any, error) {
	switch b := v.(type) {
	case bool:
		return b, nil
	case string:
		if parsed, err := strconv.ParseBool(strings.TrimSpace(b)); err == nil {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("%s: column %q needs a boolean value", where, c.Name)
}

// coerceTextValue renders a filter value as text. Text columns, and
// every other type (times, intervals, LSNs, addresses, JSON), take the
// value as text; PostgreSQL parses it as the column's type, so pgx never
// has to encode a number into a type it has no plan for.
func coerceTextValue(v any) any {
	switch s := v.(type) {
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case int:
		return strconv.Itoa(s)
	case int64:
		return strconv.FormatInt(s, 10)
	case json.Number:
		return s.String()
	case bool:
		return strconv.FormatBool(s)
	}
	return v
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
func orderTerms(order []datastoreOrder, alias string, check func(string) error) ([]string, error) {
	parts := make([]string, len(order))
	for i, o := range order {
		if err := check(o.Column); err != nil {
			return nil, err
		}
		name := metrics.QuoteIdentifier(o.Column)
		if alias != "" {
			name = qualified(alias, o.Column)
		}
		parts[i] = name + " " + strings.ToUpper(o.Direction)
	}
	return parts, nil
}

// tieBreakTerms appends to terms an ascending term for each column in
// keys that terms does not already order by, so a result has the same
// order every time and paging neither repeats nor skips rows. terms holds
// rendered "name DIRECTION" strings; name renders a column as they do.
func tieBreakTerms(terms []string, keys []string, name func(string) string) []string {
	for _, k := range keys {
		rendered := name(k)
		if slices.ContainsFunc(terms, func(t string) bool { return strings.HasPrefix(t, rendered+" ") }) {
			continue
		}
		terms = append(terms, rendered+" ASC")
	}
	return terms
}

// datastoreRowKey lists the columns that identify a row of src: the
// connection, time and entity key columns of a metric source, and id
// (or, failing that, the first column) of a configuration source.
func datastoreRowKey(src datastoreSource) []string {
	if src.Kind == datastoreKindMetric {
		return append([]string{src.ConnectionColumn, src.TimeColumn}, datastoreEntityKeys(src)...)
	}
	if _, ok := src.column("id"); ok {
		return []string{"id"}
	}
	return []string{src.Columns[0].Name}
}

// datastoreEntityKeys returns src's entity key columns, as
// metrics.EntityKeyColumns identifies them.
func datastoreEntityKeys(src datastoreSource) []string {
	types := make(map[string]string, len(src.Columns))
	for _, c := range src.Columns {
		types[c.Name] = c.keyType()
	}
	return metrics.EntityKeyColumns(src.columnNames(), types)
}

func sourceColumnCheck(src datastoreSource) func(string) error {
	return func(name string) error {
		_, err := checkColumn(src, name)
		return err
	}
}

// rowsStatement selects filtered rows, newest first by default, and
// breaks ties by the source's row key.
func rowsStatement(src datastoreSource, req datastoreRequest, columns []string, from, where string) (string, error) {
	var order []string
	if len(req.OrderBy) > 0 {
		terms, err := orderTerms(req.OrderBy, datastoreTableAlias, sourceColumnCheck(src))
		if err != nil {
			return "", err
		}
		order = terms
	} else if src.TimeColumn != "" {
		order = []string{qualified(datastoreTableAlias, src.TimeColumn) + " DESC"}
	}
	name := func(c string) string { return qualified(datastoreTableAlias, c) }
	order = tieBreakTerms(order, datastoreRowKey(src), name)
	selected := append(slices.Clone(columns), rawStatementNames(src, columns)...)
	return "SELECT " + selectList(datastoreTableAlias, selected) + " FROM " + from + where +
		" ORDER BY " + strings.Join(order, ", "), nil
}

// latestStatement selects the newest row per connection and entity key,
// using the same DISTINCT ON approach and metrics.EntityKeyColumns as
// the metrics package's latest-row queries. The window and filters apply
// inside, before the newest row is picked. The caller's ordering, if
// any, is followed by the distinct keys, which identify a row of the
// result.
func latestStatement(src datastoreSource, req datastoreRequest, columns []string, from, where string) (string, error) {
	all := src.columnNames()
	all = append(all, rawStatementNames(src, all)...)
	distinct := append([]string{src.ConnectionColumn}, datastoreEntityKeys(src)...)

	inner := "SELECT DISTINCT ON (" + selectList(datastoreTableAlias, distinct) + ") " +
		selectList(datastoreTableAlias, all) + " FROM " + from + where +
		" ORDER BY " + selectList(datastoreTableAlias, distinct) + ", " +
		qualified(datastoreTableAlias, src.TimeColumn) + " DESC"

	order, err := orderTerms(req.OrderBy, "", sourceColumnCheck(src))
	if err != nil {
		return "", err
	}
	order = tieBreakTerms(order, distinct, metrics.QuoteIdentifier)
	selected := append(slices.Clone(columns), rawStatementNames(src, columns)...)
	return "SELECT " + selectList("", selected) + " FROM (" + inner + ") latest ORDER BY " + strings.Join(order, ", "), nil
}

// aggregateRawStatements selects, after the aggregate's outputs, the
// stored text for each output holding statement text
// (statementOutput), under metrics.RawStatementAlias of the
// output's name: the least stored text of the group for a group_by
// column, and the least or greatest for min or max. The output itself
// is computed from the masked text, so the stored text chosen is a
// representative of the group, masked in Go, and need not be from the
// row the output came from.
func aggregateRawStatements(src datastoreSource, req datastoreRequest) []string {
	var selects []string
	add := func(fn, column, output string) {
		selects = append(selects, fn+"("+qualified(datastoreTableAlias, metrics.RawStatementAlias(column))+") AS "+
			metrics.QuoteIdentifier(metrics.RawStatementAlias(output)))
	}
	for _, g := range req.GroupBy {
		if statementColumn(src, g) {
			add("min", g, g)
		}
	}
	if req.Aggregation == "min" || req.Aggregation == "max" {
		for _, name := range req.Columns {
			if statementColumn(src, name) {
				add(req.Aggregation, name, req.Aggregation+"_"+name)
			}
		}
	}
	return selects
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
	selects = append(selects, aggregateRawStatements(src, req)...)

	sql := "SELECT " + strings.Join(selects, ", ") + " FROM " + from + where
	if groupCount > 0 {
		positions := make([]string, groupCount)
		for i := range positions {
			positions[i] = strconv.Itoa(i + 1)
		}
		sql += " GROUP BY " + strings.Join(positions, ", ")
	}

	// The caller's ordering is followed by the group columns, which
	// identify a row of the result.
	order, err := orderTerms(req.OrderBy, "", func(name string) error {
		if !slices.Contains(outputs, name) {
			return fmt.Errorf("order_by column %q is not an output of this aggregate; use one of %s", name, strings.Join(outputs, ", "))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	order = tieBreakTerms(order, outputs[:groupCount], metrics.QuoteIdentifier)
	if len(order) > 0 {
		sql += " ORDER BY " + strings.Join(order, ", ")
	}
	return sql, nil
}
