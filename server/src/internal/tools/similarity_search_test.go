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

func TestBuildNoTextColumnsHint(t *testing.T) {
	t.Run("qualified table splits schema and table", func(t *testing.T) {
		hint := buildNoTextColumnsHint("public.items")

		if !strings.Contains(hint, `get_schema_info(schema_name="public")`) {
			t.Errorf("expected schema hint for qualified name; got %q", hint)
		}
		if !strings.Contains(hint,
			"table_schema = 'public' AND table_name = 'items'") {
			t.Errorf("expected table_schema + table_name filters; got %q", hint)
		}
		if strings.Contains(hint, "table_name = 'public.items'") {
			t.Errorf("hint must not use the full qualified name as table_name; got %q", hint)
		}
	})

	t.Run("unqualified table omits table_schema and notes search_path", func(t *testing.T) {
		hint := buildNoTextColumnsHint("items")

		if !strings.Contains(hint, "search_path") {
			t.Errorf("expected search_path note for unqualified name; got %q", hint)
		}
		if strings.Contains(hint, "get_schema_info(schema_name=\"items\")") {
			t.Errorf("unqualified name must not be emitted as a schema; got %q", hint)
		}
		if strings.Contains(hint, "table_schema =") {
			t.Errorf("expected no table_schema clause for unqualified name; got %q", hint)
		}
		if !strings.Contains(hint, "table_name = 'items'") {
			t.Errorf("expected information_schema filter on table_name='items'; got %q", hint)
		}
	})

	t.Run("multiple dots splits only on the first", func(t *testing.T) {
		// Preserves everything after the first dot as the table name,
		// which avoids silently dropping information from the name.
		hint := buildNoTextColumnsHint("weird.schema.name")

		if !strings.Contains(hint,
			`get_schema_info(schema_name="weird")`) {
			t.Errorf("expected schema 'weird'; got %q", hint)
		}
		if !strings.Contains(hint,
			"table_schema = 'weird' AND table_name = 'schema.name'") {
			t.Errorf("expected table_name 'schema.name'; got %q", hint)
		}
	})
}

func TestQualifiedTableNameHint(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		want      string
	}{
		{
			name:      "unqualified name gets schema_name prefix",
			tableName: "items",
			want:      "schema_name.items",
		},
		{
			name:      "already qualified name is preserved as-is",
			tableName: "public.items",
			want:      "public.items",
		},
		{
			name:      "qualified name with custom schema preserved",
			tableName: "inventory.items",
			want:      "inventory.items",
		},
		{
			name:      "multi-dot name is not prefixed again",
			tableName: "weird.schema.name",
			want:      "weird.schema.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qualifiedTableNameHint(tt.tableName)
			if got != tt.want {
				t.Errorf("qualifiedTableNameHint(%q) = %q, want %q",
					tt.tableName, got, tt.want)
			}
		})
	}
}

// TestQualifiedTableNameHint_RenderedInHint renders the helper into the
// exact fmt.Fprintf hint string used by the "table not found" recovery
// block and asserts that qualified names never collapse into a nonsense
// "schema_name.schema.table" triple, while unqualified names receive the
// "schema_name." placeholder prefix.
func TestQualifiedTableNameHint_RenderedInHint(t *testing.T) {
	render := func(tableName string) string {
		return fmt.Sprintf(
			"   → similarity_search(table_name=%q, query_text=\"...\")\n",
			qualifiedTableNameHint(tableName),
		)
	}

	t.Run("qualified name is not double-prefixed", func(t *testing.T) {
		hint := render("public.items")

		if strings.Contains(hint, "schema_name.public.items") {
			t.Errorf("qualified name must not be prefixed with schema_name.; got %q", hint)
		}
		if strings.Contains(hint, "schema_name.schema.table") {
			t.Errorf("hint must not contain the schema_name.schema.table nonsense form; got %q", hint)
		}
		if !strings.Contains(hint, `table_name="public.items"`) {
			t.Errorf("expected qualified name to appear as-is inside table_name=; got %q", hint)
		}
	})

	t.Run("unqualified name receives schema_name placeholder", func(t *testing.T) {
		hint := render("items")

		if !strings.Contains(hint, `table_name="schema_name.items"`) {
			t.Errorf("expected schema_name.items placeholder for unqualified name; got %q", hint)
		}
		if strings.Contains(hint, "schema_name.schema_name.items") {
			t.Errorf("unqualified name must not be double-prefixed; got %q", hint)
		}
	})

	t.Run("multi-dot qualified name is preserved", func(t *testing.T) {
		hint := render("weird.schema.name")

		if strings.Contains(hint, "schema_name.weird.schema.name") {
			t.Errorf("multi-dot name must not be prefixed; got %q", hint)
		}
		if !strings.Contains(hint, `table_name="weird.schema.name"`) {
			t.Errorf("expected multi-dot name preserved inside table_name=; got %q", hint)
		}
	})
}

func TestEscapeSQLSingleQuotes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty string is unchanged",
			in:   "",
			want: "",
		},
		{
			name: "string without quotes is unchanged",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "single apostrophe is doubled",
			in:   "what's new",
			want: "what''s new",
		},
		{
			name: "multiple apostrophes are each doubled",
			in:   "it's a user's guide",
			want: "it''s a user''s guide",
		},
		{
			name: "only apostrophes",
			in:   "'''",
			want: "''''''",
		},
		{
			name: "double quotes are not touched",
			in:   `he said "hi"`,
			want: `he said "hi"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeSQLSingleQuotes(tt.in)
			if got != tt.want {
				t.Errorf("escapeSQLSingleQuotes(%q) = %q, want %q",
					tt.in, got, tt.want)
			}
		})
	}
}

// TestEscapeSQLSingleQuotes_RenderedInILikeHint renders the helper into
// the exact fmt.Fprintf hint string used by the embedding-failure
// recovery block and asserts that apostrophes in the user-supplied query
// text are doubled so the emitted SQL parses cleanly.
func TestEscapeSQLSingleQuotes_RenderedInILikeHint(t *testing.T) {
	render := func(tableName, queryText string) string {
		escapedQueryText := escapeSQLSingleQuotes(queryText)
		return fmt.Sprintf(
			"   → query_database(query=\"SELECT * FROM %s WHERE text_column ILIKE '%%%s%%'\")\n",
			tableName, escapedQueryText,
		)
	}

	t.Run("apostrophe in query_text is escaped", func(t *testing.T) {
		hint := render("public.items", "what's new")

		if !strings.Contains(hint, "ILIKE '%what''s new%'") {
			t.Errorf("expected doubled-quote escape inside ILIKE literal; got %q", hint)
		}

		// Count total apostrophes in the emitted line. The only
		// unescaped singletons must come from the enclosing
		// ILIKE '...' delimiters (two of them). Every apostrophe
		// introduced by queryText must appear as a doubled pair.
		if strings.Contains(hint, "ILIKE '%what's new%'") {
			t.Errorf("hint must not contain an unescaped apostrophe from query_text; got %q", hint)
		}
	})

	t.Run("multiple apostrophes are each escaped", func(t *testing.T) {
		hint := render("public.items", "it's a user's guide")

		if !strings.Contains(hint, "ILIKE '%it''s a user''s guide%'") {
			t.Errorf("expected both apostrophes doubled; got %q", hint)
		}
	})

	t.Run("query text without quotes is unchanged in hint", func(t *testing.T) {
		hint := render("public.items", "hello world")

		if !strings.Contains(hint, "ILIKE '%hello world%'") {
			t.Errorf("expected unmodified interpolation for quote-free text; got %q", hint)
		}
	})
}

func TestInferTextColumnName(t *testing.T) {
	tests := []struct {
		name      string
		vectorCol string
		wantText  string
	}{
		{
			name:      "embedding suffix",
			vectorCol: "content_embedding",
			wantText:  "content",
		},
		{
			name:      "embeddings suffix",
			vectorCol: "content_embeddings",
			wantText:  "content",
		},
		{
			name:      "vector suffix",
			vectorCol: "title_vector",
			wantText:  "title",
		},
		{
			name:      "vectors suffix",
			vectorCol: "description_vectors",
			wantText:  "description",
		},
		{
			name:      "emb suffix",
			vectorCol: "text_emb",
			wantText:  "text",
		},
		{
			name:      "no suffix",
			vectorCol: "content",
			wantText:  "content",
		},
		{
			name:      "just embedding",
			vectorCol: "embedding",
			wantText:  "",
		},
		{
			name:      "uppercase suffix",
			vectorCol: "content_EMBEDDING",
			wantText:  "content",
		},
		{
			name:      "mixed case",
			vectorCol: "Title_Vector",
			wantText:  "Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferTextColumnName(tt.vectorCol)
			if got != tt.wantText {
				t.Errorf("inferTextColumnName(%q) = %q, want %q", tt.vectorCol, got, tt.wantText)
			}
		})
	}
}

func TestIsTextDataType(t *testing.T) {
	tests := []struct {
		name     string
		dataType string
		want     bool
	}{
		{"text type", "text", true},
		{"character varying", "character varying", true},
		{"varchar", "varchar", true},
		{"character", "character", true},
		{"char", "char", true},
		{"varchar with length", "varchar(255)", true},
		{"char with length", "char(10)", true},
		{"integer", "integer", false},
		{"boolean", "boolean", false},
		{"timestamp", "timestamp", false},
		{"json", "json", false},
		{"jsonb", "jsonb", false},
		{"vector", "vector", false},
		{"empty", "", false},
		{"uppercase TEXT", "TEXT", true},
		{"uppercase VARCHAR", "VARCHAR", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTextDataType(tt.dataType)
			if got != tt.want {
				t.Errorf("isTextDataType(%q) = %v, want %v", tt.dataType, got, tt.want)
			}
		})
	}
}

func TestGetDistanceOperator(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		want   string
	}{
		{"cosine default", "cosine", "<=>"},
		{"l2", "l2", "<->"},
		{"euclidean", "euclidean", "<->"},
		{"inner_product", "inner_product", "<#>"},
		{"inner", "inner", "<#>"},
		{"empty defaults to cosine", "", "<=>"},
		{"unknown defaults to cosine", "unknown", "<=>"},
		{"uppercase L2", "L2", "<->"},
		{"uppercase COSINE", "COSINE", "<=>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDistanceOperator(tt.metric)
			if got != tt.want {
				t.Errorf("getDistanceOperator(%q) = %q, want %q", tt.metric, got, tt.want)
			}
		})
	}
}

func TestFormatEmbeddingForPostgres(t *testing.T) {
	tests := []struct {
		name      string
		embedding []float64
		want      string
	}{
		{
			name:      "simple embedding",
			embedding: []float64{1.0, 2.0, 3.0},
			want:      "[1.000000,2.000000,3.000000]",
		},
		{
			name:      "empty embedding",
			embedding: []float64{},
			want:      "[]",
		},
		{
			name:      "single value",
			embedding: []float64{0.5},
			want:      "[0.500000]",
		},
		{
			name:      "negative values",
			embedding: []float64{-1.0, 0.0, 1.0},
			want:      "[-1.000000,0.000000,1.000000]",
		},
		{
			name:      "small values",
			embedding: []float64{0.001, 0.002, 0.003},
			want:      "[0.001000,0.002000,0.003000]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatEmbeddingForPostgres(tt.embedding)
			if got != tt.want {
				t.Errorf("formatEmbeddingForPostgres() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiscoverVectorColumns(t *testing.T) {
	tests := []struct {
		name      string
		tableInfo database.TableInfo
		wantCount int
	}{
		{
			name: "table with vector columns",
			tableInfo: database.TableInfo{
				Columns: []database.ColumnInfo{
					{ColumnName: "id", DataType: "integer", IsVectorColumn: false},
					{ColumnName: "content", DataType: "text", IsVectorColumn: false},
					{ColumnName: "embedding", DataType: "vector", IsVectorColumn: true},
				},
			},
			wantCount: 1,
		},
		{
			name: "table without vector columns",
			tableInfo: database.TableInfo{
				Columns: []database.ColumnInfo{
					{ColumnName: "id", DataType: "integer", IsVectorColumn: false},
					{ColumnName: "name", DataType: "text", IsVectorColumn: false},
				},
			},
			wantCount: 0,
		},
		{
			name: "table with multiple vector columns",
			tableInfo: database.TableInfo{
				Columns: []database.ColumnInfo{
					{ColumnName: "title_embedding", DataType: "vector", IsVectorColumn: true},
					{ColumnName: "content_embedding", DataType: "vector", IsVectorColumn: true},
				},
			},
			wantCount: 2,
		},
		{
			name:      "empty table",
			tableInfo: database.TableInfo{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := discoverVectorColumns(tt.tableInfo)
			if len(got) != tt.wantCount {
				t.Errorf("discoverVectorColumns() returned %d columns, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestDiscoverTextColumns(t *testing.T) {
	tests := []struct {
		name       string
		tableInfo  database.TableInfo
		vectorCols []database.ColumnInfo
		wantCols   []string
	}{
		{
			name: "matches vector to text column",
			tableInfo: database.TableInfo{
				Columns: []database.ColumnInfo{
					{ColumnName: "content", DataType: "text", IsVectorColumn: false},
					{ColumnName: "content_embedding", DataType: "vector", IsVectorColumn: true},
				},
			},
			vectorCols: []database.ColumnInfo{
				{ColumnName: "content_embedding"},
			},
			wantCols: []string{"content"},
		},
		{
			name: "returns all text columns if no match",
			tableInfo: database.TableInfo{
				Columns: []database.ColumnInfo{
					{ColumnName: "title", DataType: "text", IsVectorColumn: false},
					{ColumnName: "description", DataType: "text", IsVectorColumn: false},
					{ColumnName: "embedding", DataType: "vector", IsVectorColumn: true},
				},
			},
			vectorCols: []database.ColumnInfo{
				{ColumnName: "embedding"},
			},
			wantCols: []string{"title", "description"},
		},
		{
			name: "no text columns",
			tableInfo: database.TableInfo{
				Columns: []database.ColumnInfo{
					{ColumnName: "id", DataType: "integer", IsVectorColumn: false},
					{ColumnName: "embedding", DataType: "vector", IsVectorColumn: true},
				},
			},
			vectorCols: []database.ColumnInfo{
				{ColumnName: "embedding"},
			},
			wantCols: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := discoverTextColumns(tt.tableInfo, tt.vectorCols)
			if len(got) != len(tt.wantCols) {
				t.Errorf("discoverTextColumns() returned %d columns, want %d", len(got), len(tt.wantCols))
				return
			}
			for i, col := range got {
				if col != tt.wantCols[i] {
					t.Errorf("discoverTextColumns()[%d] = %q, want %q", i, col, tt.wantCols[i])
				}
			}
		})
	}
}

func TestFindTableInMetadataMap(t *testing.T) {
	metadata := map[string]database.TableInfo{
		"public.users": {
			SchemaName: "public",
			TableName:  "users",
			Columns:    []database.ColumnInfo{{ColumnName: "id"}},
		},
		"public.posts": {
			SchemaName: "public",
			TableName:  "posts",
			Columns:    []database.ColumnInfo{{ColumnName: "id"}},
		},
		"custom.data": {
			SchemaName: "custom",
			TableName:  "data",
			Columns:    []database.ColumnInfo{{ColumnName: "id"}},
		},
	}

	tests := []struct {
		name      string
		tableName string
		wantErr   bool
	}{
		{
			name:      "public.users with schema",
			tableName: "public.users",
			wantErr:   false,
		},
		{
			name:      "users without schema defaults to public",
			tableName: "users",
			wantErr:   false,
		},
		{
			name:      "custom.data with schema",
			tableName: "custom.data",
			wantErr:   false,
		},
		{
			name:      "non-existent table",
			tableName: "nonexistent",
			wantErr:   true,
		},
		{
			name:      "non-existent schema",
			tableName: "other.users",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findTableInMetadataMap(metadata, tt.tableName)
			if (err != nil) != tt.wantErr {
				t.Errorf("findTableInMetadataMap() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
