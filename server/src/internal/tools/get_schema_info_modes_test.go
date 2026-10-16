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
	"fmt"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

// schemaModesMetadata returns twelve tables, enough to trigger the
// auto-summary: eleven plain tables in "app" and one vector table in
// "search".
func schemaModesMetadata() map[string]database.TableInfo {
	metadata := make(map[string]database.TableInfo)
	for i := 0; i < 11; i++ {
		name := fmt.Sprintf("t%02d", i)
		metadata["app."+name] = database.TableInfo{
			SchemaName: "app",
			TableName:  name,
			TableType:  "TABLE",
			Columns: []database.ColumnInfo{
				{ColumnName: "id", DataType: "integer", IsNullable: "NO"},
			},
		}
	}
	metadata["search.docs"] = database.TableInfo{
		SchemaName: "search",
		TableName:  "docs",
		TableType:  "TABLE",
		Columns: []database.ColumnInfo{
			{ColumnName: "id", DataType: "integer", IsNullable: "NO"},
			{ColumnName: "embedding", DataType: "vector(3)", IsNullable: "YES",
				IsVectorColumn: true, VectorDimensions: 3},
		},
	}
	return metadata
}

// TestGetSchemaInfoModes covers the summary, compact, full and empty
// result modes, including the next-step hints, which point at
// connection_id rather than any connection string in a query (#662).
func TestGetSchemaInfoModes(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		isError bool
		want    []string
		notWant []string
	}{
		{
			name:    "table without schema",
			args:    map[string]any{"table_name": "docs"},
			isError: true,
			want:    []string{"table_name requires schema_name"},
		},
		{
			name: "auto summary",
			args: map[string]any{},
			want: []string{
				"Found 12 tables across 2 schemas",
				"(+6 more)",
				"Vector-enabled: docs",
				`get_schema_info(schema_name="search")`,
			},
		},
		{
			name: "compact vector tables",
			args: map[string]any{"compact": true, "vector_tables_only": true},
			want: []string{"schema\ttable\ttype\ttable_desc\n", "search\tdocs\tTABLE"},
			notWant: []string{
				"app\tt00",
			},
		},
		{
			name: "compact filtered by schema and table",
			args: map[string]any{"compact": true, "schema_name": "app"},
			want: []string{"app\tt00\tTABLE"},
			notWant: []string{
				"search\tdocs",
			},
		},
		{
			name: "full vector tables",
			args: map[string]any{"vector_tables_only": true},
			want: []string{"search\tdocs\tTABLE\t\tembedding\tvector(3)"},
			notWant: []string{
				"app\tt00",
			},
		},
		{
			name: "full single table",
			args: map[string]any{"schema_name": "app", "table_name": "t01"},
			want: []string{"app\tt01\tTABLE\t\tid\tinteger"},
			notWant: []string{
				"app\tt02",
			},
		},
		{
			name: "missing table",
			args: map[string]any{"schema_name": "app", "table_name": "nope"},
			want: []string{
				"Table 'app.nope' not found",
				`get_schema_info(schema_name="app", compact=true)`,
				`get_schema_info(connection_id=<id from list_connections>, database_name="other_db")`,
			},
			notWant: []string{"postgres://user@host"},
		},
		{
			name: "schema without vector tables",
			args: map[string]any{"schema_name": "app", "vector_tables_only": true},
			want: []string{
				"No tables with vector columns found in schema 'app'",
				`get_schema_info(schema_name="app")`,
			},
		},
		{
			name: "missing schema",
			args: map[string]any{"schema_name": "nope"},
			want: []string{
				"Schema 'nope' not found or has no tables",
				"Try without schema filter",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := GetSchemaInfoTool(createMockClient(schemaModesMetadata()), nil)
			resp, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if resp.IsError != tt.isError {
				t.Fatalf("IsError = %v, want %v: %+v", resp.IsError, tt.isError, resp.Content)
			}
			text := resp.Content[0].Text
			for _, w := range tt.want {
				if !strings.Contains(text, w) {
					t.Errorf("expected %q in:\n%s", w, text)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(text, w) {
					t.Errorf("did not expect %q in:\n%s", w, text)
				}
			}
		})
	}
}

// TestGetSchemaInfoNoVectorTables covers the empty-result diagnosis for
// a database with no vector columns at all.
func TestGetSchemaInfoNoVectorTables(t *testing.T) {
	metadata := map[string]database.TableInfo{
		"app.t": {SchemaName: "app", TableName: "t", TableType: "TABLE"},
	}
	tool := GetSchemaInfoTool(createMockClient(metadata), nil)
	resp, err := tool.Handler(map[string]any{"vector_tables_only": true})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	text := resp.Content[0].Text
	for _, w := range []string{
		"No tables with vector columns found in any schema",
		"Check if pgvector extension is installed",
		"get_schema_info()\n",
	} {
		if !strings.Contains(text, w) {
			t.Errorf("expected %q in:\n%s", w, text)
		}
	}
}

// TestGetSchemaInfoConnectionErrors covers the connection_id path with
// no resolver and the fallback path with no client.
func TestGetSchemaInfoConnectionErrors(t *testing.T) {
	tool := GetSchemaInfoTool(nil, nil)

	resp, err := tool.Handler(map[string]any{"connection_id": float64(1)})
	if err != nil || !resp.IsError ||
		!strings.Contains(resp.Content[0].Text, "connection resolution is not available") {
		t.Errorf("expected the resolver refusal, got %+v, %v", resp, err)
	}

	resp, err = tool.Handler(map[string]any{})
	if err != nil || !resp.IsError ||
		!strings.Contains(resp.Content[0].Text, "No database connection available") {
		t.Errorf("expected the no-connection error, got %+v, %v", resp, err)
	}
}
