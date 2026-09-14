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
		want := "strpos(pss.query, '" + marker + "') = 0"
		if !strings.Contains(excludeWorkbenchQueriesClause, want) {
			t.Errorf("clause is missing %q: %s",
				want, excludeWorkbenchQueriesClause)
		}
	}

	// Both markers contain underscores, which LIKE treats as
	// single-character wildcards, so LIKE would also match unrelated
	// user queries and hide them. The match must be literal.
	if strings.Contains(excludeWorkbenchQueriesClause, "LIKE") {
		t.Errorf("clause must match literally rather than with LIKE: %s",
			excludeWorkbenchQueriesClause)
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
	// A NULL query must survive the filter. strpos(NULL, '...') is
	// NULL and NULL = 0 is NULL rather than true, so the clause needs
	// an explicit IS NULL arm; see the clause's own comment.
	if !strings.Contains(excludeWorkbenchQueriesClause, "pss.query IS NULL OR") {
		t.Errorf("clause must let a NULL query through: %s",
			excludeWorkbenchQueriesClause)
	}
}

// TestSafeTopQueryOrdering asserts that the ordering pair interpolated
// into the ORDER BY clause is whitelisted at the point of use, so the
// TestSafeTopQueryOrdering covers the defense-in-depth fallback in
// buildTopQueriesSQL. The handler resolves request values to SQL
// literals through validTopQueryOrderColumns and
// validTopQueryOrderDirections before the builder ever sees them, so
// this operates on literals: anything that is not a literal one of
// those maps can produce is replaced by the default.
func TestSafeTopQueryOrdering(t *testing.T) {
	tests := []struct {
		name         string
		orderCol     string
		orderDir     string
		wantOrderCol string
		wantOrderDir string
	}{
		{
			name:         "resolved literals pass through",
			orderCol:     "calls",
			orderDir:     "ASC",
			wantOrderCol: "calls",
			wantOrderDir: "ASC",
		},
		{
			name:         "empty values fall back",
			wantOrderCol: validTopQueryOrderColumns[defaultTopQueryOrderBy],
			wantOrderDir: validTopQueryOrderDirections[defaultTopQueryOrder],
		},
		{
			name:         "injected column falls back",
			orderCol:     "calls; DROP TABLE connections --",
			orderDir:     "ASC",
			wantOrderCol: validTopQueryOrderColumns[defaultTopQueryOrderBy],
			wantOrderDir: "ASC",
		},
		{
			name:         "injected direction falls back",
			orderCol:     "rows",
			orderDir:     "DESC; DROP TABLE connections --",
			wantOrderCol: "rows",
			wantOrderDir: validTopQueryOrderDirections[defaultTopQueryOrder],
		},
		{
			name:         "an unresolved request value is not a literal",
			orderCol:     "rows",
			orderDir:     "desc",
			wantOrderCol: "rows",
			wantOrderDir: validTopQueryOrderDirections[defaultTopQueryOrder],
		},
		{
			name:         "unknown column falls back",
			orderCol:     "wal_bytes",
			orderDir:     "DESC",
			wantOrderCol: validTopQueryOrderColumns[defaultTopQueryOrderBy],
			wantOrderDir: "DESC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCol, gotDir := safeTopQueryOrdering(tt.orderCol, tt.orderDir)
			if gotCol != tt.wantOrderCol || gotDir != tt.wantOrderDir {
				t.Fatalf("safeTopQueryOrdering(%q, %q) = (%q, %q), "+
					"want (%q, %q)", tt.orderCol, tt.orderDir,
					gotCol, gotDir, tt.wantOrderCol, tt.wantOrderDir)
			}

			// The builder must apply the same fallback, so no
			// unwhitelisted text can ever reach the ORDER BY clause.
			_, pageSQL, _, _ := buildTopQueriesSQL(
				1, "", "", false, tt.orderCol, tt.orderDir, 10, 0)
			want := "ORDER BY " + tt.wantOrderCol + " " + tt.wantOrderDir
			if !strings.Contains(pageSQL, want) {
				t.Errorf("query is missing %q: %s", want, pageSQL)
			}
			if strings.Contains(pageSQL, "DROP TABLE") {
				t.Errorf("unvalidated ordering text reached the query: %s",
					pageSQL)
			}
		})
	}
}
