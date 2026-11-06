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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// TestGetSchemaInfoNamesInspectedDatabase checks that the result names
// the database it covers and says its objects are not databases, so a
// model asked to list databases cannot present tables as databases
// (issue #800).
func TestGetSchemaInfoNamesInspectedDatabase(t *testing.T) {
	metadata := map[string]database.TableInfo{
		"public.documents": {SchemaName: "public", TableName: "documents",
			TableType: "TABLE"},
	}
	tool := GetSchemaInfoTool(database.NewTestClient(
		"postgres://user:secret@localhost/ragdb", metadata), nil)

	resp, err := tool.Handler(map[string]any{"compact": true})
	if err != nil || resp.IsError {
		t.Fatalf("handler failed: %+v, %v", resp, err)
	}
	text := resp.Content[0].Text
	for _, want := range []string{
		"Database: ragdb\n",
		"Connection: postgres://user:***@localhost/ragdb\n",
		`inside the "ragdb" database; none of them is a database`,
		"SELECT datname FROM pg_database",
		"public\tdocuments\tTABLE",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
	for _, notWant := range []string{"secret", "Note: database_name"} {
		if strings.Contains(text, notWant) {
			t.Errorf("did not expect %q in:\n%s", notWant, text)
		}
	}
}

// TestSchemaScope covers the header and override note for each
// combination of known, configured and overridden databases.
func TestSchemaScope(t *testing.T) {
	tests := []struct {
		name     string
		scope    schemaScope
		want     []string
		notWant  []string
		override bool
	}{
		{
			name:    "unknown database keeps the connection string",
			scope:   schemaScope{connStr: "host=db"},
			want:    []string{"Database: host=db\n"},
			notWant: []string{"Scope:", "Note:"},
		},
		{
			name: "configured database",
			scope: schemaScope{dbName: "ragdb", configuredDB: "ragdb",
				connStr: "postgres://h/ragdb"},
			want:    []string{"Database: ragdb\n", "Scope:"},
			notWant: []string{"Note:"},
		},
		{
			name: "override of the configured database",
			scope: schemaScope{dbName: "postgres", configuredDB: "ragdb",
				connStr: "postgres://h/postgres"},
			want: []string{
				"Database: postgres\n",
				`Note: database_name "postgres" replaced this connection's configured database, "ragdb"`,
				"without database_name",
			},
			override: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.overridden(); got != tt.override {
				t.Errorf("overridden() = %v, want %v", got, tt.override)
			}
			header := tt.scope.header()
			for _, w := range tt.want {
				if !strings.Contains(header, w) {
					t.Errorf("expected %q in:\n%s", w, header)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(header, w) {
					t.Errorf("did not expect %q in:\n%s", w, header)
				}
			}
		})
	}
}

// TestDatabaseFromConnStr covers URL, keyword and unparsable strings.
func TestDatabaseFromConnStr(t *testing.T) {
	tests := map[string]string{
		"":                               "",
		"postgres://localhost/ragdb":     "ragdb",
		"host=localhost dbname=appdb":    "appdb",
		"postgres://localhost:notaport/": "",
	}
	for in, want := range tests {
		if got := databaseFromConnStr(in); got != want {
			t.Errorf("databaseFromConnStr(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDatabaseListingGuidance keeps the tool descriptions telling the
// model how to list databases, and that get_schema_info does not
// (issue #800).
func TestDatabaseListingGuidance(t *testing.T) {
	schema := GetSchemaInfoTool(nil, nil).Definition
	conns := ListConnectionsTool(nil, nil, nil).Definition
	for name, text := range map[string]string{
		"get_schema_info description":          schema.Description,
		"get_schema_info compact description":  schema.CompactDescription,
		"list_connections description":         conns.Description,
		"list_connections compact description": conns.CompactDescription,
	} {
		if !strings.Contains(text, "SELECT datname") ||
			!strings.Contains(text, "pg_database") {
			t.Errorf("%s does not say how to list databases", name)
		}
	}
	if !strings.Contains(schema.Description, "never lists the databases on a server") {
		t.Error("get_schema_info description does not rule out listing databases")
	}
	prop, ok := schema.InputSchema.Properties["database_name"].(map[string]any)
	if !ok {
		t.Fatal("get_schema_info has no database_name property")
	}
	if desc, _ := prop["description"].(string); !strings.Contains(desc,
		"Omit it to inspect the configured database") {
		t.Errorf("database_name description does not steer towards omitting it: %q", desc)
	}
}

// TestGetSchemaInfoDatabaseOverride runs the tool against a live
// connection, once on its configured database and once with a
// database_name override, and checks that the override is called out
// with the way back to the configured database (issue #801).
func TestGetSchemaInfoDatabaseOverride(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)
	tool := GetSchemaInfoTool(nil, env.resolver)

	resp, err := tool.Handler(map[string]any{
		"__context":     ctx,
		"connection_id": float64(env.connID),
		"compact":       true,
	})
	if err != nil || resp.IsError {
		t.Fatalf("configured database: %+v, %v", resp, err)
	}
	text := resp.Content[0].Text
	if !strings.HasPrefix(text, "Database: "+env.database+"\n") {
		t.Errorf("expected the configured database first, got:\n%s", text)
	}
	if strings.Contains(text, "Note: database_name") {
		t.Errorf("did not expect an override note:\n%s", text)
	}

	// template1 exists on every server and is never a connection's
	// configured database, so it is always an override here.
	resp, err = tool.Handler(map[string]any{
		"__context":     ctx,
		"connection_id": float64(env.connID),
		"database_name": "template1",
		"compact":       true,
	})
	if err != nil || resp.IsError {
		t.Fatalf("override: %+v, %v", resp, err)
	}
	text = resp.Content[0].Text
	for _, want := range []string{
		"template1",
		`Note: database_name "template1" replaced this connection's configured database, "` + env.database + `"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
}
