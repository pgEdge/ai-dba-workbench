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
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// The tests in this file cover the branches of the tools that issue #706
// taught to audit their refusals, beyond the refusals themselves.

// schemaInfoTestMetadata builds metadata for get_schema_info with more
// tables than the auto-summary threshold: seven plain tables in "app",
// and in "vec" one table with a vector column and three without.
func schemaInfoTestMetadata() map[string]database.TableInfo {
	md := make(map[string]database.TableInfo)
	plain := []database.ColumnInfo{{ColumnName: "id", DataType: "integer"}}
	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("t%d", i)
		md["app."+name] = database.TableInfo{
			SchemaName: "app", TableName: name, TableType: "TABLE",
			Columns: plain,
		}
	}
	md["vec.docs"] = database.TableInfo{
		SchemaName: "vec", TableName: "docs", TableType: "TABLE",
		Columns: []database.ColumnInfo{
			{ColumnName: "content", DataType: "text"},
			{ColumnName: "content_embedding", DataType: "vector(3)",
				IsVectorColumn: true, VectorDimensions: 3},
		},
	}
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("plain%d", i)
		md["vec."+name] = database.TableInfo{
			SchemaName: "vec", TableName: name, TableType: "TABLE",
			Columns: plain,
		}
	}
	return md
}

// TestGetSchemaInfoToolOutputModes drives each output mode and each
// empty-result diagnosis of get_schema_info.
func TestGetSchemaInfoToolOutputModes(t *testing.T) {
	client := database.NewTestClient("postgres://u@localhost/db",
		schemaInfoTestMetadata())
	tool := GetSchemaInfoTool(client, nil)

	tests := []struct {
		name    string
		args    map[string]any
		want    []string
		notWant []string
	}{
		{
			name: "auto summary over the threshold",
			args: map[string]any{},
			want: []string{"Database Schema Summary", "Found 11 tables",
				"(+2 more)", "Vector-enabled: docs",
				`get_schema_info(schema_name="vec")`},
		},
		{
			name: "compact with schema and vector filters",
			args: map[string]any{"schema_name": "vec",
				"vector_tables_only": true, "compact": true},
			want:    []string{"schema\ttable\ttype\ttable_desc", "vec\tdocs"},
			notWant: []string{"plain0", "content_embedding"},
		},
		{
			name: "compact with a table filter in another schema",
			args: map[string]any{"schema_name": "app", "compact": true,
				"vector_tables_only": false},
			want:    []string{"app\tt0"},
			notWant: []string{"vec\tdocs"},
		},
		{
			name:    "full with the vector filter",
			args:    map[string]any{"vector_tables_only": true},
			want:    []string{"content_embedding", "vector(3)"},
			notWant: []string{"plain0", "\tt0\t"},
		},
		{
			name: "no vector tables in a schema",
			args: map[string]any{"schema_name": "app",
				"vector_tables_only": true},
			want: []string{"No tables with vector columns found in schema 'app'",
				`get_schema_info(schema_name="app")`},
		},
		{
			name: "schema not found",
			args: map[string]any{"schema_name": "missing"},
			want: []string{"Schema 'missing' not found or has no tables",
				"List all available schemas"},
		},
		{
			name: "compact table filter not found",
			args: map[string]any{"schema_name": "app", "table_name": "nope",
				"compact": true},
			want: []string{"Table 'app.nope' not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if resp.IsError {
				t.Fatalf("unexpected error response: %s", responseText(resp))
			}
			text := responseText(resp)
			for _, w := range tt.want {
				if !strings.Contains(text, w) {
					t.Errorf("response lacks %q:\n%s", w, text)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(text, w) {
					t.Errorf("response unexpectedly contains %q:\n%s", w, text)
				}
			}
		})
	}
}

// TestGetSchemaInfoToolEmptyDiagnoses covers the diagnoses given when a
// database has no tables, or none with a vector column.
func TestGetSchemaInfoToolEmptyDiagnoses(t *testing.T) {
	plainOnly := map[string]database.TableInfo{
		"app.t": {SchemaName: "app", TableName: "t", TableType: "TABLE",
			Columns: []database.ColumnInfo{{ColumnName: "id"}}},
	}

	tests := []struct {
		name     string
		metadata map[string]database.TableInfo
		args     map[string]any
		want     string
	}{
		{"no vector tables anywhere", plainOnly,
			map[string]any{"vector_tables_only": true},
			"No tables with vector columns found in any schema"},
		{"empty database", map[string]database.TableInfo{},
			map[string]any{},
			"Database appears to be completely empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := GetSchemaInfoTool(
				database.NewTestClient("postgres://u@localhost/db", tt.metadata), nil)
			resp, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if text := responseText(resp); !strings.Contains(text, tt.want) {
				t.Errorf("response lacks %q:\n%s", tt.want, text)
			}
		})
	}
}

// TestGetSchemaInfoToolNoClient covers the call with neither a
// connection_id nor a selected connection.
func TestGetSchemaInfoToolNoClient(t *testing.T) {
	resp, err := GetSchemaInfoTool(nil, nil).Handler(map[string]any{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !resp.IsError ||
		!strings.Contains(responseText(resp), "No database connection available") {
		t.Errorf("unexpected response: %+v", resp)
	}
}

// TestGetSchemaInfoToolExplicitConnection covers get_schema_info for a
// connection_id the caller may read, against a live database.
func TestGetSchemaInfoToolExplicitConnection(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)

	resp, err := GetSchemaInfoTool(nil, env.resolver).Handler(map[string]any{
		"__context":     ctx,
		"connection_id": float64(env.connID),
		"schema_name":   resolverTestSchemaName,
		"table_name":    "connections",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", responseText(resp))
	}
	if text := responseText(resp); !strings.Contains(text, "connections\tTABLE") &&
		!strings.Contains(text, "\tconnections\t") {
		t.Errorf("response does not describe the connections table:\n%s", text)
	}
}

// TestTimelineHandlerDatastoreFailures covers get_timeline_events with no
// datastore, and with a datastore whose query fails.
func TestTimelineHandlerDatastoreFailures(t *testing.T) {
	resp, err := GetTimelineEventsTool(nil, nil, nil, nil).Handler(
		map[string]any{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !resp.IsError ||
		!strings.Contains(responseText(resp), "Datastore not configured") {
		t.Errorf("unexpected response with no datastore: %+v", resp)
	}

	store, cleanup := newRBACTestStore(t)
	defer cleanup()
	ctx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)
	tool := GetTimelineEventsTool(
		database.NewTestDatastore(unreachablePool(t)),
		auth.NewRBACChecker(store), nil, nil)

	resp, err = tool.Handler(map[string]any{"__context": ctx})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !resp.IsError ||
		!strings.Contains(responseText(resp), "Failed to query timeline events") {
		t.Errorf("unexpected response for a failed query: %+v", resp)
	}
}

// TestTestQueryToolTransactionFailure covers test_query when the
// resolved connection's pool cannot open a transaction: the client the
// resolver caches for the session has had its pool closed.
func TestTestQueryToolTransactionFailure(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)
	args := map[string]any{
		"__context":     ctx,
		"connection_id": float64(env.connID),
		"query":         "SELECT 1",
	}

	resolved, errResp := env.resolver.Resolve(ctx,
		deniedActionConnectionQueryValidate, args, nil)
	if errResp != nil {
		t.Fatalf("Resolve: %s", responseText(*errResp))
	}
	resolved.Pool.Close()

	resp, err := TestQueryTool(nil, env.resolver).Handler(args)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !resp.IsError {
		t.Errorf("expected an error with a closed pool, got %s",
			responseText(resp))
	}
}
