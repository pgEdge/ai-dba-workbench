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
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/memory"
)

// memoryEmbeddingTableDDL mirrors the chat_memories table that the memory
// store reads from and writes to. The vector dimension is 3 so the mocked
// Gemini endpoint can return a three-element vector and the store can
// persist it without dimension mismatch. The table is created inside a
// per-test private schema (see newMemoryEmbeddingStore) so it never
// clobbers the canonical public.chat_memories shared with other tests.
const memoryEmbeddingTableDDL = `
CREATE TABLE chat_memories (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL,
    scope TEXT NOT NULL,
    category TEXT NOT NULL,
    content TEXT NOT NULL,
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    embedding vector(3),
    model_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

// newMemoryEmbeddingStore wires a memory.Store to the
// TEST_AI_WORKBENCH_SERVER Postgres instance. To avoid clobbering the
// shared public.chat_memories table (which other tools and memory-package
// integration tests rely on), it creates a uniquely-named private schema
// and a chat_memories table with a vector(3) embedding column inside it.
// The pool's search_path is set so the memory store's unqualified
// chat_memories references resolve to this private table. Teardown drops
// the whole schema, leaving the shared table untouched. The test skips
// cleanly when the database is unconfigured or pgvector is unavailable.
func newMemoryEmbeddingStore(t *testing.T) (*memory.Store, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping memory embedding test")
	}

	// Derive a unique, SQL-safe schema name from the test name so parallel
	// or sequential runs never collide and the shared public schema is
	// never touched.
	schema := uniqueSchemaName(t.Name())

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Skipf("Could not parse test database connection string: %v", err)
	}
	// Resolve unqualified table references to the private schema first,
	// then fall back to public for the vector extension's types.
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	// The vector extension lives in the public schema; create it there so
	// the vector type persists in public (resolvable via the search_path
	// fallback) rather than being created in and dropped with the private
	// test schema on every run.
	//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; constant string literal, no user input
	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector SCHEMA public`); err != nil {
		pool.Close()
		t.Skipf("pgvector extension unavailable: %v", err)
	}
	// Quote the schema name via pgx.Identifier so it is safely escaped; the
	// name itself is derived from the SQL-safe test name, not user input.
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; schema identifier sanitized via pgx.Identifier, DDL identifiers cannot be parameterized
	if _, err := pool.Exec(ctx,
		fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE; CREATE SCHEMA %s`, schemaIdent, schemaIdent)); err != nil {
		pool.Close()
		t.Skipf("Failed to create private test schema: %v", err)
	}
	//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; constant string literal, no user input
	if _, err := pool.Exec(ctx, memoryEmbeddingTableDDL); err != nil {
		pool.Close()
		t.Skipf("Failed to create memory embedding table (pgvector may be missing): %v", err)
	}

	store := memory.NewStore(pool)
	cleanup := func() {
		//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; schema identifier sanitized via pgx.Identifier, DDL identifiers cannot be parameterized
		if _, err := pool.Exec(context.Background(),
			fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schemaIdent)); err != nil {
			t.Logf("memory embedding teardown failed: %v", err)
		}
		pool.Close()
	}
	return store, pool, cleanup
}

// uniqueSchemaName builds an SQL-safe, lower-case schema identifier from a
// test name. Non-alphanumeric characters (which Go test names embed, e.g.
// "/" from subtests) are replaced with underscores so the result is a
// valid unquoted PostgreSQL identifier.
func uniqueSchemaName(testName string) string {
	var b strings.Builder
	b.WriteString("test_mem_")
	for _, c := range strings.ToLower(testName) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// newMockGeminiEmbeddingServer stands in for the Gemini embedContent
// endpoint. The wire contract mirrors the pgedge-go-llm-lib Gemini provider:
// the request path is /v1beta/models/<model>:embedContent, the API key is
// carried in the x-goog-api-key header, and the response is
// {"embedding":{"values":[...]}}. A three-element vector matches the
// vector(3) column used by the test schema.
func newMockGeminiEmbeddingServer(t *testing.T, apiKey string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":embedContent") {
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != apiKey {
			t.Errorf("expected API key %q in x-goog-api-key header, got %q", apiKey, got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]}}`))
	}))
}

// geminiEmbeddingConfig builds a config that enables embedding and points the
// Gemini provider at the supplied mock base URL.
func geminiEmbeddingConfig(baseURL, apiKey string) *config.Config {
	cfg := &config.Config{}
	cfg.Embedding.Enabled = true
	cfg.Embedding.Provider = "gemini"
	cfg.Embedding.Model = "gemini-embedding-001"
	cfg.Embedding.GeminiAPIKey = apiKey
	cfg.Embedding.GeminiBaseURL = baseURL
	return cfg
}

// TestStoreMemoryGeneratesEmbeddingIntegration drives the store_memory tool
// down its embedding-success path so that the vec.Float64ToFloat32 conversion
// at the call site executes against a real memory store. The mocked Gemini
// endpoint returns a three-element vector that the tool converts to float32
// and persists; the success response reports the generated embedding.
func TestStoreMemoryGeneratesEmbeddingIntegration(t *testing.T) {
	store, _, cleanup := newMemoryEmbeddingStore(t)
	defer cleanup()

	const apiKey = "test-gemini-key"
	srv := newMockGeminiEmbeddingServer(t, apiKey)
	defer srv.Close()

	cfg := geminiEmbeddingConfig(srv.URL, apiKey)
	tool := StoreMemoryTool(store, cfg, nil)

	ctx := context.WithValue(context.Background(),
		auth.UsernameContextKey, "embed-user")
	args := map[string]any{
		"content":   "The primary replica lives in eu-west-2.",
		"category":  "fact",
		"__context": ctx,
	}

	resp, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success response, got error: %s", resp.Content[0].Text)
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "Embedding: generated") {
		t.Errorf("expected generated-embedding marker in response, got: %s", body)
	}
	if !strings.Contains(body, "model: gemini-embedding-001") {
		t.Errorf("expected model name in response, got: %s", body)
	}
}

// TestRecallMemoriesGeneratesQueryEmbeddingIntegration drives the
// recall_memories tool down its embedding-success path so that the
// vec.Float64ToFloat32 conversion at the call site executes. The mocked
// Gemini endpoint supplies the query vector; the tool must convert it to
// float32 and run a vector search without error. A memory seeded with a
// matching embedding is expected back in the response.
func TestRecallMemoriesGeneratesQueryEmbeddingIntegration(t *testing.T) {
	store, _, cleanup := newMemoryEmbeddingStore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := store.Store(ctx, "embed-user", "user", "fact",
		"The primary replica lives in eu-west-2.", false,
		[]float32{0.1, 0.2, 0.3}, "gemini-embedding-001"); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	const apiKey = "test-gemini-key"
	srv := newMockGeminiEmbeddingServer(t, apiKey)
	defer srv.Close()

	cfg := geminiEmbeddingConfig(srv.URL, apiKey)
	tool := RecallMemoriesTool(store, cfg)

	authCtx := context.WithValue(context.Background(),
		auth.UsernameContextKey, "embed-user")
	args := map[string]any{
		"query":     "where is the primary replica",
		"__context": authCtx,
	}

	resp, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success response, got error: %s", resp.Content[0].Text)
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "eu-west-2") {
		t.Errorf("expected seeded memory content in response, got: %s", body)
	}
}
