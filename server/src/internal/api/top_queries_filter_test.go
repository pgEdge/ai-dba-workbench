/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests for the Top Queries panel's "Hide monitoring queries" filter.
// See GitHub issue #364: the filter previously excluded only the
// collector's probe marker, so the collector's and alerter's own
// datastore statements still appeared in the panel.
package api

import (
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// TestExcludeWorkbenchQueriesClause asserts the clause excludes both
// markers: the collector's probe column alias and the in-statement
// comment carried by the Workbench's own datastore traffic.
func TestExcludeWorkbenchQueriesClause(t *testing.T) {
	for _, marker := range []string{probeMarkerAlias, sqlmarker.Marker} {
		want := "pss.query NOT LIKE '%" + marker + "%'"
		if !strings.Contains(excludeWorkbenchQueriesClause, want) {
			t.Errorf("clause is missing %q: %s",
				want, excludeWorkbenchQueriesClause)
		}
	}
	if !strings.HasPrefix(excludeWorkbenchQueriesClause, "AND ") {
		t.Errorf("clause must be appendable to a WHERE list: %s",
			excludeWorkbenchQueriesClause)
	}
	if strings.Contains(excludeWorkbenchQueriesClause, "$") {
		t.Errorf("clause must not introduce bind parameters: %s",
			excludeWorkbenchQueriesClause)
	}
	if probeMarkerAlias != "ai_dba_wb_probe" {
		t.Errorf("probeMarkerAlias = %q; it must match WrapQuery in the "+
			"collector's probes package", probeMarkerAlias)
	}
	// A NULL query must survive the filter. NULL NOT LIKE '...' is NULL,
	// not true, so the clause needs an explicit IS NULL arm; see the
	// clause's own comment.
	if !strings.Contains(excludeWorkbenchQueriesClause, "pss.query IS NULL OR") {
		t.Errorf("clause must let a NULL query through: %s",
			excludeWorkbenchQueriesClause)
	}
}

// TestSafeTopQueryOrdering asserts that the ordering pair interpolated
// into the ORDER BY clause is whitelisted at the point of use, so the
// injection-safety property does not depend on the caller.
func TestSafeTopQueryOrdering(t *testing.T) {
	tests := []struct {
		name          string
		orderBy       string
		order         string
		wantOrderBy   string
		wantOrder     string
		wantSanitized bool
	}{
		{
			name:        "valid pair passes through",
			orderBy:     "calls",
			order:       "asc",
			wantOrderBy: "calls",
			wantOrder:   "asc",
		},
		{
			name:          "empty values fall back",
			wantOrderBy:   defaultTopQueryOrderBy,
			wantOrder:     defaultTopQueryOrder,
			wantSanitized: true,
		},
		{
			name:          "injected column falls back",
			orderBy:       "calls; DROP TABLE connections --",
			order:         "asc",
			wantOrderBy:   defaultTopQueryOrderBy,
			wantOrder:     "asc",
			wantSanitized: true,
		},
		{
			name:          "injected direction falls back",
			orderBy:       "rows",
			order:         "desc; DROP TABLE connections --",
			wantOrderBy:   "rows",
			wantOrder:     defaultTopQueryOrder,
			wantSanitized: true,
		},
		{
			name:          "uppercase direction is not accepted",
			orderBy:       "rows",
			order:         "DESC",
			wantOrderBy:   "rows",
			wantOrder:     defaultTopQueryOrder,
			wantSanitized: true,
		},
		{
			name:          "unknown column falls back",
			orderBy:       "wal_bytes",
			order:         "desc",
			wantOrderBy:   defaultTopQueryOrderBy,
			wantOrder:     "desc",
			wantSanitized: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOrderBy, gotOrder := safeTopQueryOrdering(tt.orderBy, tt.order)
			if gotOrderBy != tt.wantOrderBy || gotOrder != tt.wantOrder {
				t.Fatalf("safeTopQueryOrdering(%q, %q) = (%q, %q), "+
					"want (%q, %q)", tt.orderBy, tt.order,
					gotOrderBy, gotOrder, tt.wantOrderBy, tt.wantOrder)
			}

			// The builder must apply the same fallback, so no
			// unwhitelisted text can ever reach the ORDER BY clause.
			query, _ := buildTopQueriesQuery(
				1, 10, "", false, tt.orderBy, tt.order)
			want := "ORDER BY " + tt.wantOrderBy + " " + tt.wantOrder
			if !strings.Contains(query, want) {
				t.Errorf("query is missing %q: %s", want, query)
			}
			if tt.wantSanitized && strings.Contains(query, "DROP TABLE") {
				t.Errorf("unvalidated ordering text reached the query: %s",
					query)
			}
		})
	}
}

// TestBuildTopQueriesQuery covers every combination of the optional
// queryid filter and the Workbench-internal exclusion.
func TestBuildTopQueriesQuery(t *testing.T) {
	tests := []struct {
		name             string
		queryID          string
		excludeCollector bool
		wantArgs         int
		wantContains     []string
		wantMissing      []string
	}{
		{
			name:         "plain",
			wantArgs:     2,
			wantContains: []string{"FROM metrics.pg_stat_statements pss"},
			wantMissing: []string{
				sqlmarker.Marker, probeMarkerAlias,
				"pss.queryid::text = $",
			},
		},
		{
			name:         "with queryid",
			queryID:      "12345",
			wantArgs:     3,
			wantContains: []string{"AND pss.queryid::text = $3"},
			wantMissing:  []string{sqlmarker.Marker},
		},
		{
			name:             "excluding workbench queries",
			excludeCollector: true,
			wantArgs:         2,
			wantContains: []string{
				"NOT LIKE '%" + probeMarkerAlias + "%'",
				"NOT LIKE '%" + sqlmarker.Marker + "%'",
			},
		},
		{
			name:             "queryid and exclusion together",
			queryID:          "999",
			excludeCollector: true,
			wantArgs:         3,
			wantContains: []string{
				"AND pss.queryid::text = $3",
				"NOT LIKE '%" + probeMarkerAlias + "%'",
				"NOT LIKE '%" + sqlmarker.Marker + "%'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := buildTopQueriesQuery(
				7, 25, tt.queryID, tt.excludeCollector,
				"total_exec_time", "desc")

			if len(args) != tt.wantArgs {
				t.Fatalf("len(args) = %d, want %d", len(args), tt.wantArgs)
			}
			if args[0] != 7 || args[1] != 25 {
				t.Errorf("args = %v, want connection 7 and limit 25", args)
			}
			if tt.queryID != "" && args[2] != tt.queryID {
				t.Errorf("args[2] = %v, want %q", args[2], tt.queryID)
			}
			if !strings.Contains(query, "ORDER BY total_exec_time desc") {
				t.Errorf("ordering clause missing: %s", query)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(query, want) {
					t.Errorf("query is missing %q", want)
				}
			}
			for _, unwanted := range tt.wantMissing {
				if strings.Contains(query, unwanted) {
					t.Errorf("query unexpectedly contains %q", unwanted)
				}
			}
		})
	}
}
