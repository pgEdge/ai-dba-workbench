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
	"strings"
	"testing"
)

// Unit tests for the query_datastore handler that need no database. The
// scoping and execution paths are covered against a real datastore in
// query_datastore_integration_test.go.

func TestQueryDatastoreToolDefinition(t *testing.T) {
	tool := QueryDatastoreTool(nil, nil, nil, nil)
	def := tool.Definition

	if def.Name != "query_datastore" {
		t.Errorf("name = %q", def.Name)
	}
	if len(def.InputSchema.Required) != 1 || def.InputSchema.Required[0] != "source" {
		t.Errorf("required = %v, want [source]", def.InputSchema.Required)
	}
	for _, prop := range []string{
		"source", "columns", "connection_ids", "time_start", "time_end",
		"filters", "shape", "group_by", "aggregation", "buckets",
		"order_by", "page", "page_size", "explain",
	} {
		if _, ok := def.InputSchema.Properties[prop]; !ok {
			t.Errorf("input schema lacks %q", prop)
		}
		if !datastoreRequestKeys[prop] {
			t.Errorf("schema property %q is not accepted by the parser", prop)
		}
	}
	if len(def.InputSchema.Properties) != len(datastoreRequestKeys) {
		t.Errorf("schema has %d properties, parser accepts %d keys",
			len(def.InputSchema.Properties), len(datastoreRequestKeys))
	}
	for _, removed := range []string{"query", "sql", "limit", "offset"} {
		if _, ok := def.InputSchema.Properties[removed]; ok {
			t.Errorf("input schema still offers %q", removed)
		}
	}

	// The description must send callers to discovery and to
	// query_metrics for rates, and must not invite them to write SQL.
	for _, text := range []string{def.Description, def.CompactDescription} {
		for _, want := range []string{"describe_datastore", "query_metrics"} {
			if !strings.Contains(text, want) {
				t.Errorf("description lacks %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "SELECT ") {
			t.Errorf("description still carries SQL examples:\n%s", text)
		}
	}
}

func TestQueryDatastoreRejectsSQL(t *testing.T) {
	// The rejection comes before the pool check, so a caller is told the
	// tool changed whether or not a datastore is configured.
	tool := QueryDatastoreTool(nil, nil, nil, nil)
	for _, key := range []string{"sql", "query"} {
		t.Run(key, func(t *testing.T) {
			resp, err := tool.Handler(map[string]any{key: "SELECT * FROM connections"})
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !resp.IsError {
				t.Fatal("expected an error response")
			}
			text := resp.Content[0].Text
			for _, want := range []string{"no longer accepts SQL", "describe_datastore"} {
				if !strings.Contains(text, want) {
					t.Errorf("response %q lacks %q", text, want)
				}
			}
		})
	}
}

func TestQueryDatastoreValidationBeforePool(t *testing.T) {
	runToolCases(t, QueryDatastoreTool(nil, nil, nil, nil), []toolCase{
		{name: "missing source", raw: true, wantErr: true, want: "source is required"},
		{name: "unknown parameter", raw: true, args: map[string]any{"source": "alerts", "limit": float64(5)},
			wantErr: true, want: `unknown parameter "limit"`},
		{name: "bad page_size", raw: true, args: map[string]any{"source": "alerts", "page_size": float64(5000)},
			wantErr: true, want: "page_size must be"},
		{name: "nil pool", raw: true, args: map[string]any{"source": "alerts"},
			wantErr: true, want: "Datastore not configured"},
	})
}

func TestWriteDatastorePage(t *testing.T) {
	req := datastoreRequest{Page: 3, PageSize: 10}
	cases := []struct {
		name string
		qr   *queryResult
		want string
	}{
		{"empty", &queryResult{}, "Results (0 rows):"},
		{"last page", &queryResult{Rows: [][]any{{1}, {2}}}, "Results (rows 21-22):"},
		{"truncated", &queryResult{Rows: make([][]any, 10), WasTruncated: true},
			"Results (rows 21-30, more available - use page=4 for the next page):"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			writeDatastorePage(&sb, tc.qr, req)
			if !strings.HasPrefix(sb.String(), tc.want) {
				t.Errorf("got %q, want prefix %q", sb.String(), tc.want)
			}
		})
	}
}
