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
	"math"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testNow is the fixed clock every builder test uses.
var testNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// testMetricSource mirrors a discovered pg_stat_database source.
func testMetricSource() datastoreSource {
	return datastoreSource{
		Name:             "pg_stat_database",
		Table:            "pg_stat_database",
		Description:      "Database statistics.",
		TimeColumn:       "collected_at",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindMetric,
		Scope:            scopeConnection,
		Columns: []datastoreColumn{
			col("connection_id", typeInteger),
			col("collected_at", typeTimestamptz),
			col("datname", "name"),
			metricCol("xact_commit", typeBigint),
			metricCol("blks_hit", typeBigint),
			metricCol("cache_ratio", "double precision"),
			col("stats_reset", typeTimestamptz),
		},
	}
}

func configSource(t *testing.T, name string) datastoreSource {
	t.Helper()
	for _, src := range configurationSources() {
		if src.Name == name {
			return src
		}
	}
	t.Fatalf("no configuration source %q", name)
	return datastoreSource{}
}

func mustParse(t *testing.T, args map[string]any) datastoreRequest {
	t.Helper()
	req, err := parseDatastoreRequest(args)
	if err != nil {
		t.Fatalf("parseDatastoreRequest(%v): %v", args, err)
	}
	return req
}

func mustBuild(t *testing.T, src datastoreSource, args map[string]any, vis visibleSet) (string, []any) {
	t.Helper()
	sql, qargs, err := buildDatastoreQuery(src, mustParse(t, args), vis, testNow)
	if err != nil {
		t.Fatalf("buildDatastoreQuery: %v", err)
	}
	return sql, qargs
}

func wantErr(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got none", fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("error %q does not contain %q", err.Error(), fragment)
	}
}

var restricted = visibleSet{IDs: []int{3, 7}}
var everyone = visibleSet{All: true}

// --- parser -------------------------------------------------------------

func TestParseDatastoreRequestDefaults(t *testing.T) {
	req := mustParse(t, map[string]any{"source": "alerts", "__context": "x"})
	if req.Source != "alerts" || req.Shape != datastoreShapeRows ||
		req.Page != 1 || req.PageSize != datastoreDefaultPageSize || req.Explain {
		t.Errorf("unexpected defaults: %+v", req)
	}
}

func TestParseDatastoreRequestFull(t *testing.T) {
	req := mustParse(t, map[string]any{
		"source":         "pg_stat_database",
		"columns":        []any{"datname", "xact_commit"},
		"connection_ids": []any{float64(3), 7, int64(9)},
		"time_start":     "24h",
		"time_end":       "now",
		"filters": []any{
			map[string]any{"column": "datname", "op": "=", "value": "app"},
			map[string]any{"column": "blks_hit", "op": "IS NULL"},
		},
		"shape":       "aggregate",
		"group_by":    []any{"connection_id"},
		"aggregation": "AVG",
		"buckets":     float64(10),
		"order_by":    []any{map[string]any{"column": "connection_id", "direction": "DESC"}, map[string]any{"column": "bucket_time"}},
		"page":        float64(2),
		"page_size":   float64(50),
		"explain":     true,
	})
	if len(req.Columns) != 2 || len(req.ConnectionIDs) != 3 || req.ConnectionIDs[2] != 9 ||
		len(req.Filters) != 2 || req.Filters[1].Op != "is null" || req.Shape != "aggregate" ||
		req.Aggregation != "avg" || req.Buckets != 10 || len(req.OrderBy) != 2 ||
		req.OrderBy[0].Direction != "desc" || req.OrderBy[1].Direction != "asc" ||
		req.Page != 2 || req.PageSize != 50 || !req.Explain || req.TimeStart != "24h" {
		t.Errorf("unexpected request: %+v", req)
	}
}

func TestParseDatastoreRequestRejections(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing source", map[string]any{}, "source is required"},
		{"empty source", map[string]any{"source": " "}, "source is required"},
		{"source not string", map[string]any{"source": 3}, "source must be a string"},
		{"raw sql", map[string]any{"source": "alerts", "query": "SELECT 1"}, "no longer accepts SQL"},
		{"unknown key", map[string]any{"source": "alerts", "bogus": 1}, "unknown parameter"},
		{"raw sql key", map[string]any{"source": "alerts", "sql": "SELECT 1"}, "no longer accepts SQL"},
		{"connection_id hint", map[string]any{"source": "alerts", "connection_id": 3}, `unknown parameter "connection_id"; did you mean connection_ids (an array)?`},
		{"limit hint", map[string]any{"source": "alerts", "limit": 10}, `unknown parameter "limit"; use page_size and page`},
		{"offset hint", map[string]any{"source": "alerts", "offset": 10}, `unknown parameter "offset"; use page_size and page`},
		{"too many filters", map[string]any{"source": "alerts", "filters": repeatAny(21, map[string]any{"column": "id", "op": "is null"})}, "filters takes at most 20"},
		{"too many order_by", map[string]any{"source": "alerts", "order_by": repeatAny(21, map[string]any{"column": "id"})}, "order_by takes at most 20"},
		{"too many group_by", map[string]any{"source": "alerts", "group_by": repeatAny(21, "id")}, "group_by takes at most 20"},
		{"page beyond row cap", map[string]any{"source": "alerts", "page": 101, "page_size": 1000}, "page * page_size must be at most 100000"},
		{"page cap alone", map[string]any{"source": "alerts", "page": 100001, "page_size": 1}, "page must be an integer between 1 and 100000"},
		{"columns not array", map[string]any{"source": "alerts", "columns": "id"}, "columns must be an array"},
		{"column not string", map[string]any{"source": "alerts", "columns": []any{1}}, "columns must be an array of strings"},
		{"connection_ids not array", map[string]any{"source": "alerts", "connection_ids": 3}, "connection_ids must be an array"},
		{"connection id fractional", map[string]any{"source": "alerts", "connection_ids": []any{1.5}}, "connection_ids"},
		{"connection id string", map[string]any{"source": "alerts", "connection_ids": []any{"3"}}, "connection_ids"},
		{"connection id zero", map[string]any{"source": "alerts", "connection_ids": []any{0}}, "connection_ids"},
		{"connection id huge", map[string]any{"source": "alerts", "connection_ids": []any{float64(1 << 40)}}, "connection_ids"},
		{"time_start not string", map[string]any{"source": "alerts", "time_start": 5}, "time_start must be a string"},
		{"time_end not string", map[string]any{"source": "alerts", "time_end": 5}, "time_end must be a string"},
		{"filters not array", map[string]any{"source": "alerts", "filters": "x"}, "filters must be an array"},
		{"filter not object", map[string]any{"source": "alerts", "filters": []any{"x"}}, "filters[0] must be an object"},
		{"filter no column", map[string]any{"source": "alerts", "filters": []any{map[string]any{"op": "="}}}, "filters[0].column"},
		{"filter unknown op", map[string]any{"source": "alerts", "filters": []any{map[string]any{"column": "id", "op": "~", "value": 1}}}, "unsupported operator"},
		{"filter op not string", map[string]any{"source": "alerts", "filters": []any{map[string]any{"column": "id", "op": 1}}}, "filters[0].op"},
		{"filter unknown key", map[string]any{"source": "alerts", "filters": []any{map[string]any{"column": "id", "op": "=", "value": 1, "raw": "x"}}}, "unknown key"},
		{"bad shape", map[string]any{"source": "alerts", "shape": "pivot"}, "shape must be"},
		{"shape not string", map[string]any{"source": "alerts", "shape": 1}, "shape must be"},
		{"group_by not array", map[string]any{"source": "alerts", "group_by": "id"}, "group_by must be an array"},
		{"bad aggregation", map[string]any{"source": "alerts", "aggregation": "median"}, "aggregation must be"},
		{"aggregation not string", map[string]any{"source": "alerts", "aggregation": 1}, "aggregation must be"},
		{"buckets fractional", map[string]any{"source": "alerts", "buckets": 1.5}, "buckets"},
		{"buckets zero", map[string]any{"source": "alerts", "buckets": 0}, "buckets"},
		{"buckets too many", map[string]any{"source": "alerts", "buckets": 501}, "buckets"},
		{"order_by not array", map[string]any{"source": "alerts", "order_by": "id"}, "order_by must be an array"},
		{"order_by not object", map[string]any{"source": "alerts", "order_by": []any{"id"}}, "order_by[0] must be an object"},
		{"order_by no column", map[string]any{"source": "alerts", "order_by": []any{map[string]any{"direction": "asc"}}}, "order_by[0].column"},
		{"bad direction", map[string]any{"source": "alerts", "order_by": []any{map[string]any{"column": "id", "direction": "up"}}}, "direction must be"},
		{"direction not string", map[string]any{"source": "alerts", "order_by": []any{map[string]any{"column": "id", "direction": 1}}}, "direction must be"},
		{"order_by unknown key", map[string]any{"source": "alerts", "order_by": []any{map[string]any{"column": "id", "nulls": "first"}}}, "unknown key"},
		{"page zero", map[string]any{"source": "alerts", "page": 0}, "page"},
		{"page huge", map[string]any{"source": "alerts", "page": float64(2000000)}, "page"},
		{"page_size zero", map[string]any{"source": "alerts", "page_size": 0}, "page_size"},
		{"page_size over cap", map[string]any{"source": "alerts", "page_size": 1001}, "page_size"},
		{"page_size string", map[string]any{"source": "alerts", "page_size": "10"}, "page_size"},
		{"explain not bool", map[string]any{"source": "alerts", "explain": "yes"}, "explain must be a boolean"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDatastoreRequest(tc.args)
			wantErr(t, err, tc.want)
		})
	}
}

// repeatAny returns a list holding n copies of v.
func repeatAny(n int, v any) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// TestParseDatastoreRequestListCaps checks the caps admit exactly their
// limit.
func TestParseDatastoreRequestListCaps(t *testing.T) {
	_, err := parseDatastoreRequest(map[string]any{
		"source":   "alerts",
		"filters":  repeatAny(20, map[string]any{"column": "id", "op": "is null"}),
		"order_by": repeatAny(20, map[string]any{"column": "id"}),
		"group_by": repeatAny(20, "id"),
	})
	if err != nil {
		t.Errorf("20 entries each should be accepted: %v", err)
	}
	if _, err := parseDatastoreRequest(map[string]any{"source": "alerts", "page": 100, "page_size": 1000}); err != nil {
		t.Errorf("page 100 of 1000 rows should be accepted: %v", err)
	}
}

// TestEndsWithEscape covers the like pattern check.
func TestEndsWithEscape(t *testing.T) {
	cases := map[string]bool{
		``:      false,
		`app%`:  false,
		`app\`:  true,
		`app\\`: false,
		`a\\\`:  true,
		`\`:     true,
		`\%`:    false,
	}
	for pattern, want := range cases {
		if got := endsWithEscape(pattern); got != want {
			t.Errorf("endsWithEscape(%q) = %v, want %v", pattern, got, want)
		}
	}
}

// --- builder rejections ---------------------------------------------------

func TestBuildDatastoreQueryRejections(t *testing.T) {
	metric := testMetricSource()
	alerts := configSource(t, "alerts")
	rules := configSource(t, "alert_rules")
	conns := configSource(t, "connections")
	blackouts := configSource(t, "blackouts")

	cases := []struct {
		name string
		src  datastoreSource
		args map[string]any
		vis  visibleSet
		want string
	}{
		{"source mismatch", metric, map[string]any{"source": "alerts"}, everyone, "does not match"},
		{"unknown column", metric, map[string]any{"source": "pg_stat_database", "columns": []any{"nope"}}, everyone, "unknown column"},
		{"invalid identifier column", metric, map[string]any{"source": "pg_stat_database", "columns": []any{`datname"; DROP TABLE x; --`}}, everyone, "unknown column"},
		{"credential column", conns, map[string]any{"source": "connections", "columns": []any{"password_encrypted"}}, everyone, "unknown column"},
		{"credential filter", conns, map[string]any{"source": "connections", "filters": []any{map[string]any{"column": "sslkey", "op": "is not null"}}}, everyone, "unknown column"},
		{"credential order", conns, map[string]any{"source": "connections", "order_by": []any{map[string]any{"column": "sslcert"}}}, everyone, "unknown column"},
		{"mixed-case credential column", conns, map[string]any{"source": "connections", "columns": []any{"Password_Encrypted"}}, everyone, "unknown column"},
		{"mixed-case credential filter", conns, map[string]any{"source": "connections", "filters": []any{map[string]any{"column": "Password_Encrypted", "op": "is not null"}}}, everyone, "unknown column"},
		{"mixed-case credential group_by", conns, map[string]any{"source": "connections", "shape": "aggregate", "aggregation": "count", "group_by": []any{"Password_Encrypted"}}, everyone, "unknown column"},
		{"mixed-case credential order", conns, map[string]any{"source": "connections", "order_by": []any{map[string]any{"column": "Password_Encrypted"}}}, everyone, "unknown column"},
		{"quoted credential column", conns, map[string]any{"source": "connections", "columns": []any{"\"password_encrypted\""}}, everyone, "unknown column"},
		{"quoted credential filter", conns, map[string]any{"source": "connections", "filters": []any{map[string]any{"column": "\"password_encrypted\"", "op": "is not null"}}}, everyone, "unknown column"},
		{"quoted credential group_by", conns, map[string]any{"source": "connections", "shape": "aggregate", "aggregation": "count", "group_by": []any{"\"password_encrypted\""}}, everyone, "unknown column"},
		{"quoted credential order", conns, map[string]any{"source": "connections", "order_by": []any{map[string]any{"column": "\"password_encrypted\""}}}, everyone, "unknown column"},
		{"duplicate column", metric, map[string]any{"source": "pg_stat_database", "columns": []any{"datname", "datname"}}, everyone, "more than once"},
		{"time range on timeless source", rules, map[string]any{"source": "alert_rules", "time_start": "1h"}, everyone, "has no time column"},
		{"time end on timeless source", rules, map[string]any{"source": "alert_rules", "time_end": "now"}, everyone, "has no time column"},
		{"bad time_start", metric, map[string]any{"source": "pg_stat_database", "time_start": "yesterday"}, everyone, "invalid time_start"},
		{"bad time_end", metric, map[string]any{"source": "pg_stat_database", "time_end": "tomorrow"}, everyone, "invalid time_end"},
		{"end before start", metric, map[string]any{"source": "pg_stat_database", "time_start": "2026-10-13T10:00:00Z", "time_end": "2026-10-13T09:00:00Z"}, everyone, "must be after"},
		{"start in future", metric, map[string]any{"source": "pg_stat_database", "time_start": "2026-10-14T10:00:00Z", "time_end": "2026-10-15T09:00:00Z"}, everyone, "future"},
		{"span too long", metric, map[string]any{"source": "pg_stat_database", "time_start": "400d"}, everyone, "366 days"},
		{"config end before start", blackouts, map[string]any{"source": "blackouts", "time_start": "2026-10-13T10:00:00Z", "time_end": "2026-10-13T09:00:00Z"}, everyone, "must be after"},
		{"config bad start", blackouts, map[string]any{"source": "blackouts", "time_start": "soon"}, everyone, "invalid time_start"},
		{"config bad end", blackouts, map[string]any{"source": "blackouts", "time_end": "soon"}, everyone, "invalid time_end"},
		{"latest on configuration source", alerts, map[string]any{"source": "alerts", "shape": "latest"}, everyone, "metric sources"},
		{"latest with aggregation", metric, map[string]any{"source": "pg_stat_database", "shape": "latest", "aggregation": "avg"}, everyone, "only with shape aggregate"},
		{"rows with group_by", metric, map[string]any{"source": "pg_stat_database", "group_by": []any{"datname"}}, everyone, "only with shape aggregate"},
		{"rows with buckets", metric, map[string]any{"source": "pg_stat_database", "buckets": 5}, everyone, "only with shape aggregate"},
		{"aggregate without aggregation", metric, map[string]any{"source": "pg_stat_database", "shape": "aggregate"}, everyone, "aggregation is required"},
		{"avg of dimension", metric, map[string]any{"source": "pg_stat_database", "shape": "aggregate", "aggregation": "avg", "columns": []any{"datname"}}, everyone, "numeric metric column"},
		{"sum without columns", metric, map[string]any{"source": "pg_stat_database", "shape": "aggregate", "aggregation": "sum"}, everyone, "columns is required"},
		{"min of jsonb", alerts, map[string]any{"source": "alerts", "shape": "aggregate", "aggregation": "min", "columns": []any{"anomaly_details"}}, everyone, "cannot aggregate"},
		{"group_by overlaps columns", metric, map[string]any{"source": "pg_stat_database", "shape": "aggregate", "aggregation": "max", "columns": []any{"xact_commit"}, "group_by": []any{"xact_commit"}}, everyone, "both group_by and columns"},
		{"group_by unknown", metric, map[string]any{"source": "pg_stat_database", "shape": "aggregate", "aggregation": "count", "group_by": []any{"nope"}}, everyone, "unknown column"},
		{"buckets on timeless source", rules, map[string]any{"source": "alert_rules", "shape": "aggregate", "aggregation": "count", "buckets": 5}, everyone, "has no time column"},
		{"buckets without window", blackouts, map[string]any{"source": "blackouts", "shape": "aggregate", "aggregation": "count", "buckets": 5}, everyone, "time_start and time_end"},
		{"aggregate order by input column", metric, map[string]any{"source": "pg_stat_database", "shape": "aggregate", "aggregation": "count", "group_by": []any{"datname"}, "order_by": []any{map[string]any{"column": "xact_commit"}}}, everyone, "order_by"},
		{"latest order by unknown", metric, map[string]any{"source": "pg_stat_database", "shape": "latest", "order_by": []any{map[string]any{"column": "nope"}}}, everyone, "unknown column"},
		{"filter needs value", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "="}}}, everyone, "needs a value"},
		{"filter null value", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "=", "value": nil}}}, everyone, "is null"},
		{"is null with value", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "is null", "value": "x"}}}, everyone, "takes no value or values"},
		{"is not null with values", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "is not null", "values": []any{"x"}}}}, everyone, "takes no value or values"},
		{"in with value", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "in", "value": []any{"x"}}}}, everyone, "takes values (an array), not value"},
		{"in without values", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "in"}}}, everyone, "needs values, a non-empty array"},
		{"in not array", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "in", "values": "x"}}}, everyone, "non-empty array"},
		{"in empty", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "in", "values": []any{}}}}, everyone, "non-empty array"},
		{"in nested", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "not in", "values": []any{[]any{"x"}}}}}, everyone, "scalar"},
		{"in too long", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "in", "values": make([]any, 1001)}}}, everyone, "at most 1000"},
		{"equals with values", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "=", "values": []any{"x"}}}}, everyone, "takes value, not values"},
		{"like with values", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "like", "value": "a%", "values": []any{"x"}}}}, everyone, "takes value, not values"},
		{"object value", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "=", "value": map[string]any{"a": 1}}}}, everyone, "scalar"},
		{"like non-text column", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "xact_commit", "op": "like", "value": "1%"}}}, everyone, "text column"},
		{"like non-string", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "like", "value": 3}}}, everyone, "string"},
		{"like trailing backslash", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "datname", "op": "like", "value": `app\`}}}, everyone, "must not end with a single backslash"},
		{"integer column with text", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "connection_id", "op": "=", "value": "abc"}}}, everyone, "integer"},
		{"integer column with fraction", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "connection_id", "op": "=", "value": 1.5}}}, everyone, "integer"},
		{"integer column with bool", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "connection_id", "op": "=", "value": true}}}, everyone, "integer"},
		{"float column with text", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "cache_ratio", "op": ">", "value": "high"}}}, everyone, "number"},
		{"float column with bool", metric, map[string]any{"source": "pg_stat_database", "filters": []any{map[string]any{"column": "cache_ratio", "op": ">", "value": false}}}, everyone, "number"},
		{"boolean column with text", conns, map[string]any{"source": "connections", "filters": []any{map[string]any{"column": "is_shared", "op": "=", "value": "yes"}}}, everyone, "boolean"},
		{"invisible connection", metric, map[string]any{"source": "pg_stat_database", "connection_ids": []any{5}}, restricted, connectionNotAccessibleMsg},
		{"connection_ids on global source", rules, map[string]any{"source": "alert_rules", "connection_ids": []any{3}}, everyone, "does not apply"},
		{"connection_ids on hierarchy source", blackouts, map[string]any{"source": "blackouts", "connection_ids": []any{3}}, restricted, "does not apply"},
		{"unknown scope", datastoreSource{Name: "x", Table: "x", Scope: datastoreScope(99), Columns: []datastoreColumn{col("id", typeInteger)}}, map[string]any{"source": "x"}, everyone, "no scoping rule"},
		{"unknown kind", datastoreSource{Name: "x", Table: "x", Kind: "other", Scope: scopeGlobal, Columns: []datastoreColumn{col("id", typeInteger)}}, map[string]any{"source": "x"}, everyone, "unknown kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := mustParse(t, tc.args)
			_, _, err := buildDatastoreQuery(tc.src, req, tc.vis, testNow)
			wantErr(t, err, tc.want)
		})
	}
}

// TestBuildDatastoreQueryPageSizeCap checks the builder enforces the cap
// itself, so a request built by any other route cannot exceed it.
func TestBuildDatastoreQueryPageSizeCap(t *testing.T) {
	req := mustParse(t, map[string]any{"source": "pg_stat_database"})
	for _, size := range []int{0, datastoreMaxPageSize + 1} {
		req.PageSize = size
		_, _, err := buildDatastoreQuery(testMetricSource(), req, everyone, testNow)
		wantErr(t, err, "page_size")
	}
	req.PageSize = 10
	req.Page = 0
	_, _, err := buildDatastoreQuery(testMetricSource(), req, everyone, testNow)
	wantErr(t, err, "page")

	req.PageSize = 1000
	req.Page = 101
	_, _, err = buildDatastoreQuery(testMetricSource(), req, everyone, testNow)
	wantErr(t, err, "page * page_size")

	req.Page = 1
	req.Filters = make([]datastoreFilter, datastoreMaxListItems+1)
	_, _, err = buildDatastoreQuery(testMetricSource(), req, everyone, testNow)
	wantErr(t, err, "at most 20 entries")
}

// --- scoping ----------------------------------------------------------------

// scopeFragments gives, for each scoping rule, a fragment the SQL must
// contain for a restricted caller.
var scopeFragments = map[datastoreScope]string{
	scopeConnection: "= ANY($",
	scopeAlert:      `"connection_id" = ANY($`,
	scopeViaAlert:   "EXISTS (SELECT 1 FROM public.alerts a WHERE a.id = t.\"alert_id\"",
	scopeHierarchy:  "EXISTS (SELECT 1 FROM public.connections c WHERE c.cluster_id = t.\"cluster_id\"",
	scopeCluster:    "EXISTS (SELECT 1 FROM public.connections c WHERE c.cluster_id = t.\"id\"",
	scopeGroup:      "JOIN public.clusters cl ON cl.id = c.cluster_id WHERE cl.group_id = t.\"id\"",
}

func TestBuildDatastoreQueryScopesEverySource(t *testing.T) {
	sources := append(configurationSources(), testMetricSource())
	const secretValue = "zz-filter-value-zz"
	for _, src := range sources {
		t.Run(src.Name, func(t *testing.T) {
			var textCol string
			for _, c := range src.Columns {
				if c.DataType == typeText || c.DataType == typeVarchar || c.DataType == "name" {
					textCol = c.Name
					break
				}
			}
			args := map[string]any{"source": src.Name}
			if textCol != "" {
				args["filters"] = []any{map[string]any{"column": textCol, "op": "=", "value": secretValue}}
			}
			sql, qargs := mustBuild(t, src, args, restricted)
			if strings.Contains(sql, secretValue) {
				t.Errorf("filter value leaked into SQL: %s", sql)
			}
			if textCol != "" && !containsArg(qargs, secretValue) {
				t.Errorf("filter value not bound as a parameter: %v", qargs)
			}
			if strings.Contains(sql, "3") && strings.Contains(sql, "7") && strings.Contains(sql, "3, 7") {
				t.Errorf("visible IDs interpolated into SQL: %s", sql)
			}
			if src.Scope == scopeGlobal {
				if strings.Contains(sql, "ANY(") {
					t.Errorf("global source should not be scoped: %s", sql)
				}
				return
			}
			frag := scopeFragments[src.Scope]
			if !strings.Contains(sql, frag) {
				t.Errorf("SQL for %s (scope %s) lacks %q:\n%s", src.Name, src.Scope, frag, sql)
			}
			if !containsIntSlice(qargs, []int{3, 7}) {
				t.Errorf("visible set not bound: %v", qargs)
			}
			if !bindPlaceholder.MatchString(sql) {
				t.Errorf("SQL for %s lacks a $n::int[] placeholder:\n%s", src.Name, sql)
			}
			// The text masking patterns contain braces, so look for
			// the opening of an array literal rather than any brace.
			if strings.Contains(sql, "'{") {
				t.Errorf("SQL for %s carries an array literal:\n%s", src.Name, sql)
			}
		})
	}
}

// bindPlaceholder matches the visible-set parameter the builder emits.
var bindPlaceholder = regexp.MustCompile(`\$[0-9]+::int\[\]`)

func containsArg(args []any, want any) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsIntSlice(args []any, want []int) bool {
	for _, a := range args {
		ids, ok := a.([]int)
		if !ok || len(ids) != len(want) {
			continue
		}
		match := true
		for i := range ids {
			if ids[i] != want[i] {
				match = false
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestBuildDatastoreQueryUnrestrictedSkipsScoping(t *testing.T) {
	for _, src := range append(configurationSources(), testMetricSource()) {
		sql, _ := mustBuild(t, src, map[string]any{"source": src.Name}, visibleSet{All: true, SystemAlerts: true})
		if strings.Contains(sql, "ANY(") {
			t.Errorf("%s: unrestricted caller should not be scoped: %s", src.Name, sql)
		}
	}
}

// TestBuildDatastoreQueryKeepsToLiveConnections checks that every
// connection-scoped source keeps to live connections for every caller,
// one who may read every connection included (issue #669), and that a
// source with no connection column, or the connections source, which
// tests its own deleted_at, is not given the predicate.
func TestBuildDatastoreQueryKeepsToLiveConnections(t *testing.T) {
	live := `EXISTS (SELECT 1 FROM public.connections lc WHERE lc.id = t."connection_id" AND lc.deleted_at IS NULL)`
	for _, vis := range []visibleSet{{All: true}, {IDs: []int{3}}} {
		for _, src := range append(configurationSources(), testMetricSource()) {
			sql, _ := mustBuild(t, src, map[string]any{"source": src.Name}, vis)
			want := src.Scope == scopeConnection && src.ConnectionColumn != "" && src.LiveColumn == ""
			if got := strings.Contains(sql, live); got != want {
				t.Errorf("%s (all=%v): live connection predicate present = %v, want %v: %s",
					src.Name, vis.All, got, want, sql)
			}
		}
	}
	if got := liveConnectionCondition(datastoreSource{Scope: scopeConnection}); got != "" {
		t.Errorf("a source with no connection column got %q", got)
	}
}

func TestBuildDatastoreQueryEmptyVisibleSet(t *testing.T) {
	sql, qargs := mustBuild(t, testMetricSource(), map[string]any{"source": "pg_stat_database"}, visibleSet{})
	if !strings.Contains(sql, `t."connection_id" = ANY($1::int[])`) {
		t.Errorf("empty visible set should still scope: %s", sql)
	}
	ids, ok := qargs[0].([]int)
	if !ok || ids == nil || len(ids) != 0 {
		t.Errorf("empty visible set should bind an empty, non-nil array: %#v", qargs[0])
	}
}

func TestBuildDatastoreQueryAlertScoping(t *testing.T) {
	alerts := configSource(t, "alerts")
	acks := configSource(t, "alert_acknowledgments")
	cases := []struct {
		name    string
		src     datastoreSource
		args    map[string]any
		vis     visibleSet
		want    []string
		notWant []string
	}{
		{
			name: "all without system alerts",
			src:  alerts, args: map[string]any{"source": "alerts"},
			vis:     visibleSet{All: true},
			want:    []string{`t."connection_id" IS NOT NULL`},
			notWant: []string{`"alert_type" = $`},
		},
		{
			name: "all with system alerts",
			src:  alerts, args: map[string]any{"source": "alerts"},
			vis:     visibleSet{All: true, SystemAlerts: true},
			notWant: []string{"WHERE t.\"connection_id\""},
		},
		{
			name: "restricted with system alerts",
			src:  alerts, args: map[string]any{"source": "alerts"},
			vis:  visibleSet{IDs: []int{3}, SystemAlerts: true},
			want: []string{`(t."connection_id" = ANY($1::int[]) OR t."alert_type" = $2)`},
		},
		{
			name: "restricted without system alerts",
			src:  alerts, args: map[string]any{"source": "alerts"},
			vis:     visibleSet{IDs: []int{3}},
			want:    []string{`t."connection_id" = ANY($1::int[])`},
			notWant: []string{`"alert_type" = $`},
		},
		{
			name: "explicit connections exclude system alerts",
			src:  alerts, args: map[string]any{"source": "alerts", "connection_ids": []any{3}},
			vis:     visibleSet{All: true, SystemAlerts: true},
			want:    []string{`t."connection_id" = ANY($1::int[])`},
			notWant: []string{`"alert_type" = $`},
		},
		{
			name: "acks unrestricted with system alerts",
			src:  acks, args: map[string]any{"source": "alert_acknowledgments"},
			vis:     visibleSet{All: true, SystemAlerts: true},
			notWant: []string{"EXISTS"},
		},
		{
			name: "acks all without system alerts",
			src:  acks, args: map[string]any{"source": "alert_acknowledgments"},
			vis:  visibleSet{All: true},
			want: []string{`EXISTS (SELECT 1 FROM public.alerts a WHERE a.id = t."alert_id" AND a."connection_id" IS NOT NULL)`},
		},
		{
			name: "acks restricted with system alerts",
			src:  acks, args: map[string]any{"source": "alert_acknowledgments"},
			vis:  visibleSet{IDs: []int{3}, SystemAlerts: true},
			want: []string{`(a."connection_id" = ANY($1::int[]) OR a."alert_type" = $2)`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, qargs := mustBuild(t, tc.src, tc.args, tc.vis)
			for _, w := range tc.want {
				if !strings.Contains(sql, w) {
					t.Errorf("SQL lacks %q:\n%s", w, sql)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(sql, w) {
					t.Errorf("SQL should not contain %q:\n%s", w, sql)
				}
			}
			if strings.Contains(sql, "'system'") {
				t.Errorf("system alert type should be bound: %s", sql)
			}
			for _, w := range tc.want {
				if strings.Contains(w, "alert_type") && !containsArg(qargs, "system") {
					t.Errorf("system alert type not bound: %v", qargs)
				}
			}
		})
	}
}

func TestBuildDatastoreQueryHierarchyPredicate(t *testing.T) {
	sql, qargs := mustBuild(t, configSource(t, "blackouts"), map[string]any{"source": "blackouts"}, restricted)
	for _, w := range []string{
		`t."connection_id" IS NULL AND t."cluster_id" IS NULL AND t."group_id" IS NULL AND t."scope" IN ($2, $3)`,
		`t."connection_id" IS NOT NULL AND t."connection_id" = ANY($1::int[])`,
		`t."connection_id" IS NULL AND t."cluster_id" IS NOT NULL AND EXISTS (SELECT 1 FROM public.connections c WHERE c.cluster_id = t."cluster_id" AND c.id = ANY($1::int[]))`,
		`t."connection_id" IS NULL AND t."cluster_id" IS NULL AND t."group_id" IS NOT NULL AND EXISTS (SELECT 1 FROM public.connections c JOIN public.clusters cl ON cl.id = c.cluster_id WHERE cl.group_id = t."group_id" AND c.id = ANY($1::int[]))`,
	} {
		if !strings.Contains(sql, w) {
			t.Errorf("SQL lacks %q:\n%s", w, sql)
		}
	}
	if !containsArg(qargs, "estate") || !containsArg(qargs, "global") {
		t.Errorf("estate scope names should be bound: %v", qargs)
	}
}

func TestBuildDatastoreQueryConnectionIDs(t *testing.T) {
	sql, qargs := mustBuild(t, testMetricSource(), map[string]any{"source": "pg_stat_database", "connection_ids": []any{7}}, restricted)
	if !strings.Contains(sql, `t."connection_id" = ANY($1::int[])`) || !containsIntSlice(qargs, []int{7}) {
		t.Errorf("connection_ids should narrow the scope: %s %v", sql, qargs)
	}
	sql, qargs = mustBuild(t, configSource(t, "connections"), map[string]any{"source": "connections", "connection_ids": []any{3}}, everyone)
	if !strings.Contains(sql, `t."id" = ANY($1::int[])`) || !containsIntSlice(qargs, []int{3}) {
		t.Errorf("connections should scope on id: %s %v", sql, qargs)
	}
	if !strings.HasPrefix(sql, `SELECT t."id", t."name"`) || !strings.Contains(sql, "FROM public.\"connections\" t WHERE") {
		t.Errorf("connections query shape: %s", sql)
	}
}

// --- shapes -----------------------------------------------------------------

func TestBuildDatastoreQueryMetricDefaultWindow(t *testing.T) {
	sql, qargs := mustBuild(t, testMetricSource(), map[string]any{"source": "pg_stat_database"}, everyone)
	want := `SELECT t."connection_id", t."collected_at", t."datname", t."xact_commit", t."blks_hit", t."cache_ratio", t."stats_reset" FROM (SELECT t."connection_id", t."collected_at", t."datname", t."xact_commit", t."blks_hit", t."cache_ratio", t."stats_reset" FROM metrics."pg_stat_database" t WHERE EXISTS (SELECT 1 FROM public.connections lc WHERE lc.id = t."connection_id" AND lc.deleted_at IS NULL) AND t."collected_at" >= $1 AND t."collected_at" <= $2 OFFSET 0) t ORDER BY t."collected_at" DESC, t."connection_id" ASC, t."datname" ASC LIMIT $3 OFFSET $4`
	if sql != want {
		t.Errorf("SQL =\n%s\nwant\n%s", sql, want)
	}
	if len(qargs) != 4 || qargs[0] != testNow.Add(-time.Hour) || qargs[1] != testNow ||
		qargs[2] != int64(101) || qargs[3] != int64(0) {
		t.Errorf("args = %v", qargs)
	}
}

func TestBuildDatastoreQueryTimeForms(t *testing.T) {
	cases := []struct {
		name       string
		start, end string
		wantStart  time.Time
		wantEnd    time.Time
	}{
		{"relative start", "24h", "", testNow.Add(-24 * time.Hour), testNow},
		{"days", "7d", "now", testNow.Add(-7 * 24 * time.Hour), testNow},
		{"relative end", "2h", "1h", testNow.Add(-3 * time.Hour), testNow.Add(-time.Hour)},
		{"absolute", "2026-10-13T09:00:00Z", "2026-10-13T10:00:00Z", testNow.Add(-3 * time.Hour), testNow.Add(-2 * time.Hour)},
		{"end clamped", "2026-10-13T11:00:00Z", "2026-10-14T00:00:00Z", testNow.Add(-time.Hour), testNow},
		{"default start before end", "", "2026-10-13T10:00:00Z", testNow.Add(-3 * time.Hour), testNow.Add(-2 * time.Hour)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"source": "pg_stat_database"}
			if tc.start != "" {
				args["time_start"] = tc.start
			}
			if tc.end != "" {
				args["time_end"] = tc.end
			}
			_, qargs := mustBuild(t, testMetricSource(), args, everyone)
			if qargs[0] != tc.wantStart || qargs[1] != tc.wantEnd {
				t.Errorf("window = %v .. %v, want %v .. %v", qargs[0], qargs[1], tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestBuildDatastoreQueryConfigTimeRange(t *testing.T) {
	src := configSource(t, "blackouts")
	sql, _ := mustBuild(t, src, map[string]any{"source": "blackouts"}, everyone)
	if strings.Contains(sql, `"start_time" >=`) {
		t.Errorf("config source without range should be unbounded: %s", sql)
	}
	if !strings.Contains(sql, `FROM public."blackouts" t ORDER BY t."start_time" DESC, t."id" ASC LIMIT`) {
		t.Errorf("config source should order by its time column: %s", sql)
	}
	sql, qargs := mustBuild(t, src, map[string]any{"source": "blackouts", "time_start": "7d"}, everyone)
	if !strings.Contains(sql, `t."start_time" >= $1`) || strings.Contains(sql, `"start_time" <=`) ||
		qargs[0] != testNow.Add(-7*24*time.Hour) {
		t.Errorf("start-only range: %s %v", sql, qargs)
	}
	sql, qargs = mustBuild(t, src, map[string]any{"source": "blackouts", "time_end": "2026-12-01T00:00:00Z"}, everyone)
	if !strings.Contains(sql, `t."start_time" <= $1`) || strings.Contains(sql, `"start_time" >=`) ||
		qargs[0] != time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("end-only range (future allowed): %s %v", sql, qargs)
	}
	sql, _ = mustBuild(t, src, map[string]any{"source": "blackouts", "time_start": "1d", "time_end": "now"}, everyone)
	if !strings.Contains(sql, `t."start_time" >= $1 AND t."start_time" <= $2`) {
		t.Errorf("full range: %s", sql)
	}
	sql, _ = mustBuild(t, configSource(t, "alert_rules"), map[string]any{"source": "alert_rules"}, everyone)
	if !strings.Contains(sql, `ORDER BY t."id" ASC LIMIT`) {
		t.Errorf("timeless source should order by id: %s", sql)
	}
}

func TestBuildDatastoreQueryFilters(t *testing.T) {
	src := testMetricSource()
	filters := []any{
		map[string]any{"column": "datname", "op": "=", "value": "app"},
		map[string]any{"column": "datname", "op": "!=", "value": "template0"},
		map[string]any{"column": "xact_commit", "op": "<", "value": float64(10)},
		map[string]any{"column": "xact_commit", "op": "<=", "value": "11"},
		map[string]any{"column": "xact_commit", "op": ">", "value": int64(1)},
		map[string]any{"column": "cache_ratio", "op": ">=", "value": "0.5"},
		map[string]any{"column": "datname", "op": "in", "values": []any{"a", "b"}},
		map[string]any{"column": "connection_id", "op": "not in", "values": []any{float64(4), "5"}},
		map[string]any{"column": "datname", "op": "like", "value": "app%"},
		map[string]any{"column": "stats_reset", "op": "is null"},
		map[string]any{"column": "blks_hit", "op": "is not null"},
		map[string]any{"column": "datname", "op": "=", "value": float64(12)},
		map[string]any{"column": "datname", "op": "=", "value": true},
		map[string]any{"column": "cache_ratio", "op": "<", "value": 2},
		map[string]any{"column": "stats_reset", "op": "<", "value": "2026-10-01T00:00:00Z"},
	}
	sql, qargs := mustBuild(t, src, map[string]any{"source": "pg_stat_database", "filters": filters}, everyone)
	for _, w := range []string{
		`t."datname" = $3`, `t."datname" <> $4`, `t."xact_commit" < $5`,
		`t."xact_commit" <= $6`, `t."xact_commit" > $7`, `t."cache_ratio" >= $8`,
		`t."datname" IN ($9, $10)`, `t."connection_id" NOT IN ($11, $12)`,
		`t."datname" LIKE $13`, `t."stats_reset" IS NULL`, `t."blks_hit" IS NOT NULL`,
		`t."datname" = $14`, `t."datname" = $15`, `t."cache_ratio" < $16`, `t."stats_reset" < $17`,
	} {
		if !strings.Contains(sql, w) {
			t.Errorf("SQL lacks %q:\n%s", w, sql)
		}
	}
	want := []any{"app", "template0", int64(10), int64(11), int64(1), 0.5, "a", "b", int64(4), int64(5), "app%", "12", "true", float64(2), "2026-10-01T00:00:00Z"}
	for i, w := range want {
		if qargs[i+2] != w {
			t.Errorf("arg %d = %#v, want %#v", i+3, qargs[i+2], w)
		}
	}
	for _, v := range []string{"app", "template0", "app%"} {
		if strings.Contains(sql, v) {
			t.Errorf("value %q interpolated into SQL", v)
		}
	}
}

func TestBuildDatastoreQueryBooleanFilter(t *testing.T) {
	_, qargs := mustBuild(t, configSource(t, "connections"), map[string]any{
		"source":  "connections",
		"filters": []any{map[string]any{"column": "is_shared", "op": "=", "value": true}},
	}, everyone)
	if !containsArg(qargs, true) {
		t.Errorf("boolean not bound: %v", qargs)
	}
}

func TestBuildDatastoreQueryColumnsAndOrder(t *testing.T) {
	sql, qargs := mustBuild(t, testMetricSource(), map[string]any{
		"source":    "pg_stat_database",
		"columns":   []any{"datname", "xact_commit"},
		"order_by":  []any{map[string]any{"column": "xact_commit", "direction": "desc"}, map[string]any{"column": "datname"}},
		"page":      3,
		"page_size": 20,
	}, everyone)
	if !strings.HasPrefix(sql, `SELECT t."datname", t."xact_commit" FROM`) ||
		!strings.Contains(sql, `ORDER BY t."xact_commit" DESC, t."datname" ASC, t."connection_id" ASC, t."collected_at" ASC LIMIT $3 OFFSET $4`) {
		t.Errorf("SQL = %s", sql)
	}
	if qargs[2] != int64(21) || qargs[3] != int64(40) {
		t.Errorf("paging args = %v", qargs[2:])
	}
}

func TestBuildDatastoreQueryLatest(t *testing.T) {
	sql, _ := mustBuild(t, testMetricSource(), map[string]any{
		"source":  "pg_stat_database",
		"shape":   "latest",
		"columns": []any{"datname", "xact_commit"},
		"filters": []any{map[string]any{"column": "datname", "op": "!=", "value": "template1"}},
	}, restricted)
	want := `SELECT "datname", "xact_commit" FROM (SELECT DISTINCT ON (t."connection_id", t."datname") t."connection_id", t."collected_at", t."datname", t."xact_commit", t."blks_hit", t."cache_ratio", t."stats_reset" FROM (SELECT t."connection_id", t."collected_at", t."datname", t."xact_commit", t."blks_hit", t."cache_ratio", t."stats_reset" FROM metrics."pg_stat_database" t WHERE t."connection_id" = ANY($1::int[]) AND EXISTS (SELECT 1 FROM public.connections lc WHERE lc.id = t."connection_id" AND lc.deleted_at IS NULL) AND t."collected_at" >= $2 AND t."collected_at" <= $3 OFFSET 0) t WHERE t."datname" <> $4 ORDER BY t."connection_id", t."datname", t."collected_at" DESC) latest ORDER BY "connection_id" ASC, "datname" ASC LIMIT $5 OFFSET $6`
	if sql != want {
		t.Errorf("SQL =\n%s\nwant\n%s", sql, want)
	}
	sql, _ = mustBuild(t, testMetricSource(), map[string]any{
		"source":   "pg_stat_database",
		"shape":    "latest",
		"order_by": []any{map[string]any{"column": "xact_commit", "direction": "desc"}},
	}, everyone)
	if !strings.Contains(sql, `latest ORDER BY "xact_commit" DESC, "connection_id" ASC, "datname" ASC LIMIT`) {
		t.Errorf("latest order_by: %s", sql)
	}
}

func TestBuildDatastoreQueryAggregate(t *testing.T) {
	sql, qargs := mustBuild(t, testMetricSource(), map[string]any{
		"source":      "pg_stat_database",
		"shape":       "aggregate",
		"aggregation": "avg",
		"columns":     []any{"xact_commit", "cache_ratio"},
		"group_by":    []any{"connection_id", "datname"},
		"buckets":     6,
	}, restricted)
	want := `SELECT date_bin($4::interval, t."collected_at", $2::timestamptz) AS "bucket_time", t."connection_id", t."datname", avg(t."xact_commit") AS "avg_xact_commit", avg(t."cache_ratio") AS "avg_cache_ratio" FROM (SELECT t."connection_id", t."collected_at", t."datname", t."xact_commit", t."blks_hit", t."cache_ratio", t."stats_reset" FROM metrics."pg_stat_database" t WHERE t."connection_id" = ANY($1::int[]) AND EXISTS (SELECT 1 FROM public.connections lc WHERE lc.id = t."connection_id" AND lc.deleted_at IS NULL) AND t."collected_at" >= $2 AND t."collected_at" <= $3 OFFSET 0) t GROUP BY 1, 2, 3 ORDER BY "bucket_time" ASC, "connection_id" ASC, "datname" ASC LIMIT $5 OFFSET $6`
	if sql != want {
		t.Errorf("SQL =\n%s\nwant\n%s", sql, want)
	}
	if qargs[3] != "600000000 microseconds" {
		t.Errorf("bucket width arg = %#v", qargs[3])
	}

	sql, _ = mustBuild(t, testMetricSource(), map[string]any{
		"source":      "pg_stat_database",
		"shape":       "aggregate",
		"aggregation": "count",
	}, everyone)
	if !strings.HasPrefix(sql, `SELECT count(*) AS "count" FROM`) || strings.Contains(sql, "GROUP BY") || strings.Contains(sql, "ORDER BY") {
		t.Errorf("count(*) SQL = %s", sql)
	}

	sql, _ = mustBuild(t, configSource(t, "alerts"), map[string]any{
		"source":      "alerts",
		"shape":       "aggregate",
		"aggregation": "max",
		"columns":     []any{"triggered_at"},
		"group_by":    []any{"severity"},
		"order_by":    []any{map[string]any{"column": "max_triggered_at", "direction": "desc"}},
	}, everyone)
	if !strings.Contains(sql, `SELECT t."severity", max(t."triggered_at") AS "max_triggered_at" FROM (SELECT t."id", `) ||
		!strings.Contains(sql, `FROM public."alerts" t WHERE t."connection_id" IS NOT NULL OFFSET 0) t GROUP BY 1`) ||
		!strings.Contains(sql, `GROUP BY 1 ORDER BY "max_triggered_at" DESC, "severity" ASC LIMIT`) {
		t.Errorf("max SQL = %s", sql)
	}

	sql, _ = mustBuild(t, configSource(t, "blackouts"), map[string]any{
		"source":      "blackouts",
		"shape":       "aggregate",
		"aggregation": "count",
		"columns":     []any{"reason"},
		"buckets":     2,
		"time_start":  "2d",
		"time_end":    "now",
	}, everyone)
	if !strings.Contains(sql, `date_bin($3::interval, t."start_time", $1::timestamptz) AS "bucket_time", count(t."reason") AS "count_reason"`) ||
		!strings.Contains(sql, `GROUP BY 1 ORDER BY "bucket_time" ASC`) {
		t.Errorf("config buckets SQL = %s", sql)
	}
}

func TestBuildDatastoreQueryExplainIgnored(t *testing.T) {
	a, aargs := mustBuild(t, testMetricSource(), map[string]any{"source": "pg_stat_database"}, everyone)
	b, bargs := mustBuild(t, testMetricSource(), map[string]any{"source": "pg_stat_database", "explain": true}, everyone)
	if a != b || len(aargs) != len(bargs) || strings.Contains(b, "EXPLAIN") {
		t.Errorf("explain should not change the generated statement")
	}
}

func TestCoerceFilterValue(t *testing.T) {
	intCol := col("n", typeBigint)
	floatCol := metricCol("f", typeReal)
	textCol := col("s", typeText)
	boolCol := col("b", typeBoolean)
	otherCol := col("ts", typeTimestamptz)
	cases := []struct {
		c    datastoreColumn
		in   any
		want any
	}{
		{intCol, 4, int64(4)},
		{intCol, json.Number("5"), int64(5)},
		{floatCol, int64(3), float64(3)},
		{floatCol, json.Number("1.5"), 1.5},
		{textCol, 7, "7"},
		{textCol, int64(8), "8"},
		{textCol, json.Number("9"), "9"},
		{textCol, false, "false"},
		{boolCol, "TRUE", true},
		{boolCol, false, false},
		{otherCol, "2026-01-01", "2026-01-01"},
		{col("lag", "interval"), float64(5), "5"},
	}
	for _, tc := range cases {
		got, err := coerceFilterValue(tc.c, tc.in, "f")
		if err != nil || got != tc.want {
			t.Errorf("coerce(%s, %#v) = %#v, %v; want %#v", tc.c.DataType, tc.in, got, err, tc.want)
		}
	}
	for _, tc := range []struct {
		c  datastoreColumn
		in any
	}{
		{floatCol, json.Number("x")},
		{intCol, json.Number("1.5")},
		{boolCol, 1},
		{textCol, []any{}},
	} {
		if _, err := coerceFilterValue(tc.c, tc.in, "f"); err == nil {
			t.Errorf("coerce(%s, %#v) should fail", tc.c.DataType, tc.in)
		}
	}
}

// TestCoerceFloatValueNativeNumbers covers the Go number types a float
// column accepts as they are, without parsing.
func TestCoerceFloatValueNativeNumbers(t *testing.T) {
	floatCol := metricCol("f", typeReal)
	for _, tc := range []struct {
		in   any
		want float64
	}{
		{2.5, 2.5},
		{2, 2},
	} {
		got, err := coerceFloatValue(floatCol, tc.in, "f")
		if err != nil || got != tc.want {
			t.Errorf("coerceFloatValue(%#v) = %#v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestIntegerValue(t *testing.T) {
	if n, ok := integerValue(float64(math.MinInt64)); !ok || n != math.MinInt64 {
		t.Errorf("MinInt64: %d %v", n, ok)
	}
	if n, ok := integerValue(json.Number("12")); !ok || n != 12 {
		t.Errorf("json.Number: %d %v", n, ok)
	}
	// float64(math.MaxInt64) is 2^63, one past the int64 range.
	for _, v := range []any{1e30, float64(math.MaxInt64), -1e19, 1.5, "1", json.Number("1.2"), nil} {
		if _, ok := integerValue(v); ok {
			t.Errorf("integerValue(%#v) should fail", v)
		}
	}
}

func TestParseDatastoreTimeRejectsNonPositiveDuration(t *testing.T) {
	_, err := parseDatastoreTime("-1h", "time_start", testNow, testNow)
	wantErr(t, err, "must be positive")
}

func TestDatastoreBuilderDoesNotMutateRequest(t *testing.T) {
	req := mustParse(t, map[string]any{"source": "pg_stat_database", "connection_ids": []any{3}})
	if _, _, err := buildDatastoreQuery(testMetricSource(), req, restricted, testNow); err != nil {
		t.Fatal(err)
	}
	if len(req.ConnectionIDs) != 1 || req.ConnectionIDs[0] != 3 {
		t.Errorf("request mutated: %+v", req)
	}
}

// TestParseDatastoreRequestNullConnections checks that a null
// connection_ids, or a null connection_id, which several LLM providers
// send for an omitted parameter, is treated as absent rather than
// refused, whilst a connection_id with a value is still refused as
// unknown (issue #706).
func TestParseDatastoreRequestNullConnections(t *testing.T) {
	req := mustParse(t, map[string]any{"source": "alerts",
		"connection_ids": nil, "connection_id": nil})
	if req.ConnectionIDs != nil {
		t.Errorf("Expected no connection_ids, got %v", req.ConnectionIDs)
	}
	if _, err := parseDatastoreRequest(map[string]any{"source": "alerts",
		"connection_id": float64(3)}); err == nil ||
		!strings.Contains(err.Error(), "connection_ids") {
		t.Errorf("Expected a valued connection_id refused, got %v", err)
	}
}
