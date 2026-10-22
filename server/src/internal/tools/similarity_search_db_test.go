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
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// similaritySearchFixture creates the tables the similarity_search test
// searches, in a schema of its own. It must run before the first
// resolution, since the resolved client loads its metadata once.
const similaritySearchFixture = `
DROP SCHEMA IF EXISTS ai_wb_simsearch CASCADE;
CREATE SCHEMA ai_wb_simsearch;
CREATE TABLE ai_wb_simsearch.docs (
    id serial PRIMARY KEY,
    content text,
    content_embedding public.vector(3)
);
INSERT INTO ai_wb_simsearch.docs (content, content_embedding) VALUES
    ('PostgreSQL vacuum reclaims storage held by dead tuples, and autovacuum runs it automatically in the background.', '[0.1,0.2,0.3]'),
    ('Checkpoints flush dirty buffers to disk; tuning max_wal_size spaces them out and smooths write bursts.', '[0.3,0.2,0.1]'),
    ('Replication slots retain WAL until every consumer has received it, so an idle slot can fill the disk.', '[0.2,0.2,0.2]');
CREATE TABLE ai_wb_simsearch.novec (id int, content text);
CREATE TABLE ai_wb_simsearch.vecnotext (id int, blob_embedding public.vector(3));
CREATE TABLE ai_wb_simsearch.wrongdim (
    id int, content text, content_embedding public.vector(4));
INSERT INTO ai_wb_simsearch.wrongdim VALUES (1, 'four dims', '[1,2,3,4]');
CREATE TABLE ai_wb_simsearch.emptydocs (
    id int, content text, content_embedding public.vector(3));
`

// TestSimilaritySearchToolDatabase drives similarity_search through a
// resolved connection against a live database with pgvector, covering
// the argument checks, each diagnosis and each output format.
func TestSimilaritySearchToolDatabase(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx,
		`CREATE EXTENSION IF NOT EXISTS vector SCHEMA public`); err != nil {
		t.Skipf("pgvector extension unavailable: %v", err)
	}
	if _, err := env.pool.Exec(ctx, similaritySearchFixture); err != nil {
		t.Fatalf("Failed to create the similarity search fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = env.pool.Exec(context.Background(),
			"DROP SCHEMA IF EXISTS ai_wb_simsearch CASCADE")
	})

	const apiKey = "test-key"
	srv := newMockGeminiEmbeddingServer(t, apiKey)
	defer srv.Close()

	superCtx := context.WithValue(ctx, auth.IsSuperuserContextKey, true)
	enabled := SimilaritySearchTool(nil, env.resolver,
		geminiEmbeddingConfig(srv.URL, apiKey))
	disabled := SimilaritySearchTool(nil, env.resolver, &config.Config{})

	call := func(tool Tool, table, query string, extra map[string]any) (string, bool) {
		t.Helper()
		args := map[string]any{
			"__context":     superCtx,
			"connection_id": float64(env.connID),
			"table_name":    table,
			"query_text":    query,
		}
		for k, v := range extra {
			args[k] = v
		}
		resp, err := tool.Handler(args)
		if err != nil {
			t.Fatalf("Handler: %v", err)
		}
		return responseText(resp), resp.IsError
	}

	tests := []struct {
		name    string
		tool    Tool
		table   string
		query   string
		extra   map[string]any
		wantErr bool
		want    string
	}{
		{"invalid table name", enabled, "bad;name", "vacuum", nil, true,
			"Invalid table name"},
		{"blank query", enabled, "ai_wb_simsearch.docs", "   ", nil, true,
			"query_text cannot be empty"},
		{"table not found", enabled, "ai_wb_simsearch.missing", "vacuum",
			map[string]any{"top_n": float64(5), "chunk_size_tokens": float64(50),
				"lambda": 0.5, "distance_metric": "l2"},
			true, "Table 'ai_wb_simsearch.missing' not found"},
		{"no vector columns", enabled, "ai_wb_simsearch.novec", "vacuum", nil,
			true, "No vector columns found"},
		{"no text columns", enabled, "ai_wb_simsearch.vecnotext", "vacuum",
			nil, true, "No text columns found"},
		{"embedding disabled", disabled, "ai_wb_simsearch.docs", "vacuum",
			nil, true, "Failed to generate query embedding"},
		{"dimension mismatch", enabled, "ai_wb_simsearch.wrongdim", "vacuum",
			nil, true, "Vector search failed"},
		{"no results", enabled, "ai_wb_simsearch.emptydocs", "vacuum", nil,
			false, "No results found"},
		{"full format", enabled, "ai_wb_simsearch.docs", "vacuum dead tuples",
			nil, false, "Table: ai_wb_simsearch.docs"},
		{"summary format", enabled, "ai_wb_simsearch.docs", "checkpoints",
			map[string]any{"output_format": "summary"}, false,
			"Table: ai_wb_simsearch.docs"},
		{"ids only format", enabled, "ai_wb_simsearch.docs", "replication",
			map[string]any{"output_format": "ids_only"}, false,
			"Table: ai_wb_simsearch.docs"},
		{"budget too small", enabled, "ai_wb_simsearch.docs", "vacuum",
			map[string]any{"max_output_tokens": float64(1)}, false,
			"no chunks fit within the token budget"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isErr := call(tt.tool, tt.table, tt.query, tt.extra)
			if isErr != tt.wantErr {
				t.Fatalf("IsError = %v, want %v:\n%s", isErr, tt.wantErr, text)
			}
			if !strings.Contains(text, tt.want) {
				t.Errorf("response lacks %q:\n%s", tt.want, text)
			}
		})
	}

	t.Run("missing query text", func(t *testing.T) {
		resp, err := enabled.Handler(map[string]any{
			"__context": superCtx, "table_name": "ai_wb_simsearch.docs"})
		if err != nil {
			t.Fatalf("Handler: %v", err)
		}
		if !resp.IsError {
			t.Errorf("expected an error without query_text: %s",
				responseText(resp))
		}
	})

	t.Run("missing table name", func(t *testing.T) {
		resp, err := enabled.Handler(map[string]any{
			"__context": superCtx, "query_text": "vacuum"})
		if err != nil {
			t.Fatalf("Handler: %v", err)
		}
		if !resp.IsError {
			t.Errorf("expected an error without table_name: %s",
				responseText(resp))
		}
	})
}
