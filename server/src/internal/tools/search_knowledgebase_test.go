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
	"database/sql"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
	_ "modernc.org/sqlite"
)

// TestDeserializeEmbedding checks that stored embeddings decode from
// their little-endian blob form, and that empty or truncated blobs
// decode to nothing at all.
func TestDeserializeEmbedding(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want []float32
	}{
		{
			name: "valid embedding",
			data: func() []byte {
				buf := make([]byte, 12) // 3 float32s
				binary.LittleEndian.PutUint32(buf[0:], math.Float32bits(1.0))
				binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(2.0))
				binary.LittleEndian.PutUint32(buf[8:], math.Float32bits(3.0))
				return buf
			}(),
			want: []float32{1.0, 2.0, 3.0},
		},
		{
			name: "empty data",
			data: []byte{},
			want: nil,
		},
		{
			name: "nil data",
			data: nil,
			want: nil,
		},
		{
			name: "invalid length not multiple of 4",
			data: []byte{1, 2, 3},
			want: nil,
		},
		{
			name: "single float",
			data: func() []byte {
				buf := make([]byte, 4)
				binary.LittleEndian.PutUint32(buf, math.Float32bits(0.5))
				return buf
			}(),
			want: []float32{0.5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deserializeEmbedding(tt.data)
			if len(got) != len(tt.want) {
				t.Errorf("deserializeEmbedding() returned %d elements, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("deserializeEmbedding()[%d] = %f, want %f", i, v, tt.want[i])
				}
			}
		})
	}
}

// TestCosineSimilarity covers the similarity measure, including the
// zero results returned for mismatched lengths and for zero vectors.
func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
		want float64
	}{
		{
			name: "identical vectors",
			a:    []float32{1.0, 0.0, 0.0},
			b:    []float32{1.0, 0.0, 0.0},
			want: 1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1.0, 0.0, 0.0},
			b:    []float32{0.0, 1.0, 0.0},
			want: 0.0,
		},
		{
			name: "opposite vectors",
			a:    []float32{1.0, 0.0, 0.0},
			b:    []float32{-1.0, 0.0, 0.0},
			want: -1.0,
		},
		{
			name: "same direction different magnitude",
			a:    []float32{1.0, 2.0, 3.0},
			b:    []float32{2.0, 4.0, 6.0},
			want: 1.0,
		},
		{
			name: "different lengths returns 0",
			a:    []float32{1.0, 2.0},
			b:    []float32{1.0, 2.0, 3.0},
			want: 0.0,
		},
		{
			name: "zero vector a",
			a:    []float32{0.0, 0.0, 0.0},
			b:    []float32{1.0, 2.0, 3.0},
			want: 0.0,
		},
		{
			name: "zero vector b",
			a:    []float32{1.0, 2.0, 3.0},
			b:    []float32{0.0, 0.0, 0.0},
			want: 0.0,
		},
		{
			name: "empty vectors",
			a:    []float32{},
			b:    []float32{},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("cosineSimilarity() = %f, want %f", got, tt.want)
			}
		})
	}
}

// TestFormatKBResults checks that the rendered report echoes the query,
// the chunk metadata, and any project or version filter applied.
func TestFormatKBResults(t *testing.T) {
	tests := []struct {
		name            string
		results         []KBSearchResult
		query           string
		projectNames    []string
		projectVersions []string
		wantContains    []string
	}{
		{
			name: "basic results",
			results: []KBSearchResult{
				{
					Text:           "Test content",
					Title:          "Test Title",
					Section:        "Section 1",
					ProjectName:    "PostgreSQL",
					ProjectVersion: "17",
					Similarity:     0.95,
				},
			},
			query:           "test query",
			projectNames:    nil,
			projectVersions: nil,
			wantContains: []string{
				`"test query"`,
				"Test content",
				"Test Title",
				"PostgreSQL",
				"0.950",
			},
		},
		{
			name: "with project filter",
			results: []KBSearchResult{
				{
					Text:        "Content",
					ProjectName: "pgEdge",
					Similarity:  0.85,
				},
			},
			query:           "search",
			projectNames:    []string{"pgEdge"},
			projectVersions: nil,
			wantContains: []string{
				"Filter - Projects: pgEdge",
			},
		},
		{
			name: "with version filter",
			results: []KBSearchResult{
				{
					Text:           "Content",
					ProjectName:    "PostgreSQL",
					ProjectVersion: "16",
					Similarity:     0.90,
				},
			},
			query:           "search",
			projectNames:    []string{"PostgreSQL"},
			projectVersions: []string{"16"},
			wantContains: []string{
				"Filter - Projects: PostgreSQL",
				"Versions: 16",
			},
		},
		{
			name:            "empty results",
			results:         []KBSearchResult{},
			query:           "nothing",
			projectNames:    nil,
			projectVersions: nil,
			wantContains: []string{
				"Found 0 relevant chunks",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatKBResults(tt.results, tt.query, tt.projectNames, tt.projectVersions)
			for _, want := range tt.wantContains {
				if !containsString(got, want) {
					t.Errorf("formatKBResults() missing %q in output:\n%s", want, got)
				}
			}
		})
	}
}

// TestKBSearchResultStruct guards the field names and types of the
// result struct that the tool output is built from.
func TestKBSearchResultStruct(t *testing.T) {
	result := KBSearchResult{
		Text:           "Sample documentation text",
		Title:          "Getting Started",
		Section:        "Introduction",
		ProjectName:    "PostgreSQL",
		ProjectVersion: "17",
		FilePath:       "/docs/intro.md",
		Similarity:     0.92,
	}

	if result.Text != "Sample documentation text" {
		t.Errorf("Text = %q, want %q", result.Text, "Sample documentation text")
	}
	if result.Title != "Getting Started" {
		t.Errorf("Title = %q, want %q", result.Title, "Getting Started")
	}
	if result.Section != "Introduction" {
		t.Errorf("Section = %q, want %q", result.Section, "Introduction")
	}
	if result.ProjectName != "PostgreSQL" {
		t.Errorf("ProjectName = %q, want %q", result.ProjectName, "PostgreSQL")
	}
	if result.ProjectVersion != "17" {
		t.Errorf("ProjectVersion = %q, want %q", result.ProjectVersion, "17")
	}
	if result.FilePath != "/docs/intro.md" {
		t.Errorf("FilePath = %q, want %q", result.FilePath, "/docs/intro.md")
	}
	if result.Similarity != 0.92 {
		t.Errorf("Similarity = %f, want %f", result.Similarity, 0.92)
	}
}

// The knowledgebase layouts exercised by the searchKB tests, each a
// static DDL statement so that no identifier is ever assembled at
// runtime. Between them they cover an older file with no
// gemini_embedding column, a current file carrying all four embedding
// columns, a file built for Gemini alone, a file with no embedding
// columns whatsoever, and a file whose columns appear in an unexpected
// order with an extra column of its own.
const (
	kbLegacySchemaDDL = `
        CREATE TABLE chunks (
            text TEXT,
            title TEXT,
            section TEXT,
            project_name TEXT,
            project_version TEXT,
            file_path TEXT,
            openai_embedding BLOB,
            voyage_embedding BLOB,
            ollama_embedding BLOB
        );
    `

	kbAllProvidersSchemaDDL = `
        CREATE TABLE chunks (
            text TEXT,
            title TEXT,
            section TEXT,
            project_name TEXT,
            project_version TEXT,
            file_path TEXT,
            openai_embedding BLOB,
            voyage_embedding BLOB,
            ollama_embedding BLOB,
            gemini_embedding BLOB
        );
    `

	kbGeminiOnlySchemaDDL = `
        CREATE TABLE chunks (
            text TEXT,
            title TEXT,
            section TEXT,
            project_name TEXT,
            project_version TEXT,
            file_path TEXT,
            gemini_embedding BLOB
        );
    `

	kbNoEmbeddingsSchemaDDL = `
        CREATE TABLE chunks (
            text TEXT,
            title TEXT,
            section TEXT,
            project_name TEXT,
            project_version TEXT,
            file_path TEXT
        );
    `

	kbReorderedSchemaDDL = `
        CREATE TABLE chunks (
            id INTEGER PRIMARY KEY,
            gemini_embedding BLOB,
            file_path TEXT,
            token_count INTEGER,
            text TEXT,
            project_version TEXT,
            openai_embedding BLOB,
            project_name TEXT,
            section TEXT,
            title TEXT
        );
    `
)

// createSearchKBChunksSchema creates the legacy `chunks` table used by
// most searchKB tests, carrying the three original embedding columns and
// no gemini_embedding column.
func createSearchKBChunksSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	createSearchKBSchema(t, db, kbLegacySchemaDDL)
}

// createSearchKBSchema executes one of the static schema statements
// above, failing the test if the table cannot be created.
func createSearchKBSchema(t *testing.T, db *sql.DB, ddl string) {
	t.Helper()
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create schema: %v", err)
	}
}

// openSearchKBTestDB creates an empty SQLite database in the test's
// temporary directory, applies the given schema, and returns the open
// handle along with the file path. The caller closes the handle.
func openSearchKBTestDB(t *testing.T, name, ddl string) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	createSearchKBSchema(t, db, ddl)
	return db, path
}

// encodeKBVector serializes a float32 vector into the little-endian blob
// format that searchKB expects to find in the knowledgebase.
func encodeKBVector(vector []float32) []byte {
	buf := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// buildSearchKBTestDB creates a temporary SQLite knowledgebase pre-
// populated with chunk rows for searchKB tests. It returns the path to
// the database file.
func buildSearchKBTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kb.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	createSearchKBChunksSchema(t, db)

	encode := encodeKBVector

	rows := []struct {
		text, title, section, project, version, filePath string
		openai                                           []byte
	}{
		{
			text:     "PostgreSQL documentation for 17",
			title:    "Intro",
			section:  "Overview",
			project:  "PostgreSQL",
			version:  "17",
			filePath: "/docs/pg17.md",
			openai:   encode([]float32{1, 0, 0}),
		},
		{
			text:     "pgEdge documentation",
			title:    "Getting Started",
			section:  "Intro",
			project:  "pgEdge",
			version:  "1.0",
			filePath: "/docs/pgedge.md",
			openai:   encode([]float32{0, 1, 0}),
		},
		{
			text:     "Older PostgreSQL docs",
			title:    "Legacy",
			section:  "Overview",
			project:  "PostgreSQL",
			version:  "14",
			filePath: "/docs/pg14.md",
			openai:   encode([]float32{0, 0, 1}),
		},
	}

	for _, r := range rows {
		_, err := db.Exec(
			`INSERT INTO chunks (text, title, section, project_name,
                project_version, file_path, openai_embedding,
                voyage_embedding, ollama_embedding)
              VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
			r.text, r.title, r.section, r.project, r.version, r.filePath,
			r.openai,
		)
		if err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	return path
}

// TestSearchKB_UnfilteredReturnsAllResults checks that an unfiltered
// search returns every chunk, most similar first.
func TestSearchKB_UnfilteredReturnsAllResults(t *testing.T) {
	path := buildSearchKBTestDB(t)

	queryEmbedding := []float32{1, 0, 0}
	results, err := searchKB(path, queryEmbedding, nil, nil, 10, "openai")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// The highest similarity result should be the one with embedding
	// {1, 0, 0}, i.e. the PostgreSQL 17 row.
	if results[0].ProjectName != "PostgreSQL" || results[0].ProjectVersion != "17" {
		t.Errorf("expected top result to be PostgreSQL 17, got %s %s",
			results[0].ProjectName, results[0].ProjectVersion)
	}
}

// TestSearchKB_ProjectNameFilter checks that the project name filter is
// applied as a bound IN clause.
func TestSearchKB_ProjectNameFilter(t *testing.T) {
	path := buildSearchKBTestDB(t)

	queryEmbedding := []float32{1, 0, 0}
	results, err := searchKB(path, queryEmbedding, []string{"pgEdge"}, nil, 10, "openai")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 pgEdge result, got %d", len(results))
	}
	for _, r := range results {
		if r.ProjectName != "pgEdge" {
			t.Errorf("expected only pgEdge results, got %s", r.ProjectName)
		}
	}
}

// TestSearchKB_VersionFilter checks that project name and version
// filters combine.
func TestSearchKB_VersionFilter(t *testing.T) {
	path := buildSearchKBTestDB(t)

	queryEmbedding := []float32{1, 0, 0}
	results, err := searchKB(path, queryEmbedding,
		[]string{"PostgreSQL"}, []string{"17"}, 10, "openai")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ProjectVersion != "17" {
		t.Errorf("expected version 17, got %s", results[0].ProjectVersion)
	}
}

// TestSearchKB_TopNLimits checks that results are capped at topN after
// ranking, not before.
func TestSearchKB_TopNLimits(t *testing.T) {
	path := buildSearchKBTestDB(t)

	queryEmbedding := []float32{1, 0, 0}
	results, err := searchKB(path, queryEmbedding, nil, nil, 2, "openai")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results capped by topN, got %d", len(results))
	}
}

// TestSearchKB_ErrorsWhenProviderBlobMissingForAllRows checks that a
// knowledgebase whose provider column exists but is empty throughout is
// reported as a misconfiguration instead of being searched with another
// provider's vectors.
func TestSearchKB_ErrorsWhenProviderBlobMissingForAllRows(t *testing.T) {
	path := buildSearchKBTestDB(t)

	// Query with provider "voyage" but our DB only has openai blobs. The
	// voyage column exists yet is NULL throughout, so rather than
	// silently substituting another provider's vectors, searchKB reports
	// the mismatch.
	queryEmbedding := []float32{1, 0, 0}
	results, err := searchKB(path, queryEmbedding, nil, nil, 10, "voyage")
	if err == nil {
		t.Fatalf("expected an error, got %d results", len(results))
	}
	for _, want := range []string{path, "voyage_embedding", "empty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

// TestSearchKB_SkipsRowsWithNoEmbeddingAndBadBlobs checks that chunks
// with no embedding for the searched provider, or with a malformed one,
// are skipped whilst the remaining chunks are still ranked.
func TestSearchKB_SkipsRowsWithNoEmbeddingAndBadBlobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kb.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	createSearchKBChunksSchema(t, db)

	// Row with no embeddings at all - should be skipped entirely.
	_, err = db.Exec(`INSERT INTO chunks
        (text, title, section, project_name, project_version, file_path,
         openai_embedding, voyage_embedding, ollama_embedding)
        VALUES
        ('missing embedding', '', '', 'X', '1', '/x', NULL, NULL, NULL)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Row with an invalid embedding blob (len % 4 != 0) - deserialize
	// returns nil so the row is skipped after looking up the blob.
	_, err = db.Exec(`INSERT INTO chunks
        (text, title, section, project_name, project_version, file_path,
         openai_embedding, voyage_embedding, ollama_embedding)
        VALUES
        ('bad embedding', '', '', 'Y', '1', '/y', ?, NULL, NULL)`,
		[]byte{1, 2, 3})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Row with a voyage embedding only — exercises the voyage case.
	vecBuf := make([]byte, 12)
	binary.LittleEndian.PutUint32(vecBuf[0:], math.Float32bits(1))
	binary.LittleEndian.PutUint32(vecBuf[4:], math.Float32bits(0))
	binary.LittleEndian.PutUint32(vecBuf[8:], math.Float32bits(0))
	_, err = db.Exec(`INSERT INTO chunks
        (text, title, section, project_name, project_version, file_path,
         openai_embedding, voyage_embedding, ollama_embedding)
        VALUES
        ('voyage row', '', '', 'Z', '1', '/z', NULL, ?, NULL)`, vecBuf)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Row with an ollama embedding only — exercises the ollama case.
	_, err = db.Exec(`INSERT INTO chunks
        (text, title, section, project_name, project_version, file_path,
         openai_embedding, voyage_embedding, ollama_embedding)
        VALUES
        ('ollama row', '', '', 'W', '1', '/w', NULL, NULL, ?)`, vecBuf)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Row whose voyage blob is itself malformed (len % 4 != 0), so
	// deserialization yields nothing and the row is skipped.
	_, err = db.Exec(`INSERT INTO chunks
        (text, title, section, project_name, project_version, file_path,
         openai_embedding, voyage_embedding, ollama_embedding)
        VALUES
        ('bad voyage embedding', '', '', 'V', '1', '/v', NULL, ?, NULL)`,
		[]byte{1, 2, 3})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	results, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "voyage")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	// Only the voyage row carries a usable voyage embedding; the rows with no
	// voyage blob are skipped rather than falling back to another
	// provider's vectors.
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Text != "voyage row" {
		t.Errorf("expected the voyage row, got %q", results[0].Text)
	}

	// Repeat with provider="ollama" so we hit the ollama branch.
	resultsOllama, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "ollama")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(resultsOllama) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resultsOllama))
	}
	if resultsOllama[0].Text != "ollama row" {
		t.Errorf("expected the ollama row, got %q", resultsOllama[0].Text)
	}
}

// TestSearchKB_OpenFailure covers the error returned when the
// knowledgebase file cannot be opened at all.
func TestSearchKB_OpenFailure(t *testing.T) {
	// A path containing a null byte is invalid for SQLite and should
	// fail to open, exercising the error branch of searchKB.
	_, err := searchKB("/nonexistent/dir\x00/kb.sqlite", []float32{1, 0, 0},
		nil, nil, 10, "openai")
	if err == nil {
		t.Errorf("expected error from invalid path, got nil")
	}
}

// buildGeminiAwareKBTestDB creates a knowledgebase carrying both openai
// and gemini embedding columns. The two columns rank the rows in
// opposite orders, so a test can tell which column searchKB actually
// used. It returns the database path.
func buildGeminiAwareKBTestDB(t *testing.T) string {
	t.Helper()
	db, path := openSearchKBTestDB(t, "kb-gemini.sqlite", kbAllProvidersSchemaDDL)
	defer db.Close()

	rows := []struct {
		text           string
		openai, gemini []float32
	}{
		{text: "openai favorite", openai: []float32{1, 0, 0}, gemini: []float32{0, 0, 1}},
		{text: "gemini favorite", openai: []float32{0, 0, 1}, gemini: []float32{1, 0, 0}},
		{text: "neither favorite", openai: []float32{0, 1, 0}, gemini: []float32{0, 1, 0}},
	}
	for _, r := range rows {
		_, err := db.Exec(
			`INSERT INTO chunks (text, title, section, project_name,
                project_version, file_path, openai_embedding,
                voyage_embedding, ollama_embedding, gemini_embedding)
              VALUES (?, 'T', 'S', 'PostgreSQL', '17', '/docs/x.md', ?,
                NULL, NULL, ?)`,
			r.text, encodeKBVector(r.openai), encodeKBVector(r.gemini))
		if err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	return path
}

// TestSearchKB_GeminiUsesGeminiEmbedding confirms that a provider of
// "gemini" ranks on the gemini_embedding column, and that the openai
// path still works on the same, newer-layout knowledgebase.
func TestSearchKB_GeminiUsesGeminiEmbedding(t *testing.T) {
	path := buildGeminiAwareKBTestDB(t)
	queryEmbedding := []float32{1, 0, 0}

	geminiResults, err := searchKB(path, queryEmbedding, nil, nil, 10, "gemini")
	if err != nil {
		t.Fatalf("searchKB (gemini): %v", err)
	}
	if len(geminiResults) != 3 {
		t.Fatalf("expected 3 results, got %d", len(geminiResults))
	}
	if geminiResults[0].Text != "gemini favorite" {
		t.Errorf("gemini search ranked %q first; the gemini_embedding column was not used",
			geminiResults[0].Text)
	}
	if geminiResults[0].Similarity <= geminiResults[1].Similarity {
		t.Errorf("expected descending similarity, got %v then %v",
			geminiResults[0].Similarity, geminiResults[1].Similarity)
	}

	// Mixed-case provider values must resolve the same way.
	upperResults, err := searchKB(path, queryEmbedding, nil, nil, 10, "Gemini")
	if err != nil {
		t.Fatalf("searchKB (Gemini): %v", err)
	}
	if len(upperResults) == 0 || upperResults[0].Text != "gemini favorite" {
		t.Errorf("expected the gemini row first for provider %q, got %+v", "Gemini", upperResults)
	}

	openaiResults, err := searchKB(path, queryEmbedding, nil, nil, 10, "openai")
	if err != nil {
		t.Fatalf("searchKB (openai): %v", err)
	}
	if len(openaiResults) != 3 {
		t.Fatalf("expected 3 openai results, got %d", len(openaiResults))
	}
	if openaiResults[0].Text != "openai favorite" {
		t.Errorf("openai search ranked %q first, want %q",
			openaiResults[0].Text, "openai favorite")
	}
}

// TestSearchKB_GeminiOnlyKnowledgebase covers a knowledgebase built
// solely for Gemini, where the other embedding columns are absent from
// the schema entirely.
func TestSearchKB_GeminiOnlyKnowledgebase(t *testing.T) {
	db, path := openSearchKBTestDB(t, "kb-gemini-only.sqlite", kbGeminiOnlySchemaDDL)
	defer db.Close()

	inserts := []struct {
		text   string
		vector []float32
	}{
		{"close match", []float32{1, 0, 0}},
		{"distant match", []float32{0, 1, 0}},
	}
	for _, row := range inserts {
		_, err := db.Exec(
			`INSERT INTO chunks (text, title, section, project_name,
                project_version, file_path, gemini_embedding)
              VALUES (?, 'T', 'S', 'pgEdge', '1.0', '/docs/g.md', ?)`,
			row.text, encodeKBVector(row.vector))
		if err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	db.Close()

	results, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "gemini")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Text != "close match" {
		t.Errorf("expected %q first, got %q", "close match", results[0].Text)
	}
}

// TestSearchKB_GeminiColumnAbsentReturnsError verifies that searching an
// older knowledgebase, which has no gemini_embedding column at all, with
// provider "gemini" produces an explicit, actionable error rather than a
// bare SQLite "no such column" failure or a silent fallback.
func TestSearchKB_GeminiColumnAbsentReturnsError(t *testing.T) {
	path := buildSearchKBTestDB(t)

	results, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "gemini")
	if err == nil {
		t.Fatalf("expected an error, got %d results", len(results))
	}
	msg := err.Error()
	for _, want := range []string{path, "gemini", "gemini_embedding", "openai"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "no such column") {
		t.Errorf("error leaked a raw SQLite failure: %q", msg)
	}
}

// TestSearchKB_NoEmbeddingColumns covers a chunks table that carries
// none of the supported embedding columns.
func TestSearchKB_NoEmbeddingColumns(t *testing.T) {
	db, path := openSearchKBTestDB(t, "kb-bare.sqlite", kbNoEmbeddingsSchemaDDL)
	db.Close()

	_, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "gemini")
	if err == nil {
		t.Fatal("expected an error for a knowledgebase with no embedding columns")
	}
	if !strings.Contains(err.Error(), "no embeddings at all") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestSearchKB_NoChunksTable covers a knowledgebase file that has no
// chunks table whatsoever.
func TestSearchKB_NoChunksTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kb-empty.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (id INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	_, err = searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "openai")
	if err == nil {
		t.Fatal("expected an error when the chunks table is absent")
	}
	if !strings.Contains(err.Error(), "failed to query knowledgebase") ||
		!strings.Contains(err.Error(), path) {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestSearchKB_CorruptDatabaseFile covers the failure path where the
// knowledgebase file exists but is not a SQLite database, so the query
// fails outright.
func TestSearchKB_CorruptDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kb-corrupt.sqlite")
	if err := os.WriteFile(path, []byte("this is not a database"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "gemini")
	if err == nil {
		t.Fatal("expected an error for a corrupt knowledgebase file")
	}
	if !strings.Contains(err.Error(), "failed to query knowledgebase") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestSearchKB_ResolvesColumnsByName covers a knowledgebase whose chunks
// table orders its columns unexpectedly and carries extra columns of its
// own, confirming that every scan destination is resolved by column name
// rather than by position.
func TestSearchKB_ResolvesColumnsByName(t *testing.T) {
	db, path := openSearchKBTestDB(t, "kb-reordered.sqlite", kbReorderedSchemaDDL)
	defer db.Close()

	inserts := []struct {
		text           string
		gemini, openai []float32
	}{
		{"reordered close", []float32{1, 0, 0}, []float32{0, 1, 0}},
		{"reordered far", []float32{0, 1, 0}, []float32{1, 0, 0}},
	}
	for _, row := range inserts {
		_, err := db.Exec(
			`INSERT INTO chunks (id, gemini_embedding, file_path, token_count,
                text, project_version, openai_embedding, project_name,
                section, title)
              VALUES (NULL, ?, '/docs/r.md', 42, ?, '18', ?, 'PostgreSQL',
                'Overview', 'Reordered')`,
			encodeKBVector(row.gemini), row.text, encodeKBVector(row.openai))
		if err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}

	results, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "gemini")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	top := results[0]
	if top.Text != "reordered close" {
		t.Errorf("expected %q first, got %q", "reordered close", top.Text)
	}
	if top.Title != "Reordered" || top.Section != "Overview" ||
		top.ProjectName != "PostgreSQL" || top.ProjectVersion != "18" ||
		top.FilePath != "/docs/r.md" {
		t.Errorf("metadata resolved incorrectly: %+v", top)
	}
}

// TestSearchKB_SkipsUnscannableRows covers a chunk whose metadata cannot
// be scanned, here because a NOT NULL-free schema allows a NULL text
// column; the row is skipped and the rest of the search proceeds.
func TestSearchKB_SkipsUnscannableRows(t *testing.T) {
	db, path := openSearchKBTestDB(t, "kb-nulltext.sqlite", kbGeminiOnlySchemaDDL)
	defer db.Close()

	_, err := db.Exec(
		`INSERT INTO chunks (text, title, section, project_name,
            project_version, file_path, gemini_embedding)
          VALUES (NULL, 'T', 'S', 'pgEdge', '1.0', '/docs/n.md', ?)`,
		encodeKBVector([]float32{1, 0, 0}))
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO chunks (text, title, section, project_name,
            project_version, file_path, gemini_embedding)
          VALUES ('scannable', 'T', 'S', 'pgEdge', '1.0', '/docs/s.md', ?)`,
		encodeKBVector([]float32{1, 0, 0}))
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}

	results, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "gemini")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Text != "scannable" {
		t.Errorf("expected the scannable row, got %q", results[0].Text)
	}
}

// TestKBScanTargets checks that scan destinations are mapped by column
// name, that the provider's embedding column is picked up, and that
// unwanted columns are given a discard destination.
func TestKBScanTargets(t *testing.T) {
	columns := []string{"id", "gemini_embedding", "text", "openai_embedding"}
	var row kbChunkRow
	targets := kbScanTargets(columns, "gemini_embedding", &row)

	if len(targets) != len(columns) {
		t.Fatalf("expected %d targets, got %d", len(columns), len(targets))
	}
	if targets[1] != any(&row.embedding) {
		t.Errorf("expected the gemini column to target the embedding field")
	}
	if targets[2] != any(&row.text) {
		t.Errorf("expected the text column to target the text field")
	}
	if targets[0] == any(&row.embedding) || targets[3] == any(&row.embedding) {
		t.Errorf("unwanted columns must not target the embedding field")
	}
}

// TestKBAvailableProviders checks that the reported providers follow the
// allow-list order and ignore columns that are not embeddings.
func TestKBAvailableProviders(t *testing.T) {
	got := kbAvailableProviders([]string{
		"text", "gemini_embedding", "id", "openai_embedding"})
	want := []string{"openai", "gemini"}
	if len(got) != len(want) {
		t.Fatalf("kbAvailableProviders() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("kbAvailableProviders() = %v, want %v", got, want)
			break
		}
	}
	if providers := kbAvailableProviders([]string{"text"}); len(providers) != 0 {
		t.Errorf("expected no providers, got %v", providers)
	}
}

// TestKBProviderColumn checks the provider-to-column mapping, including
// the openai default for unrecognized providers.
func TestKBProviderColumn(t *testing.T) {
	tests := map[string]string{
		"openai":  "openai_embedding",
		"voyage":  "voyage_embedding",
		"ollama":  "ollama_embedding",
		"gemini":  "gemini_embedding",
		"GEMINI":  "gemini_embedding",
		"unknown": "openai_embedding",
		"":        "openai_embedding",
	}
	for provider, want := range tests {
		if got := kbProviderColumn(provider); got != want {
			t.Errorf("kbProviderColumn(%q) = %q, want %q", provider, got, want)
		}
	}
	if got := kbProviderName("gemini_embedding"); got != "gemini" {
		t.Errorf("kbProviderName() = %q, want %q", got, "gemini")
	}
}

// containsString checks if the string contains the substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findInString(s, substr))
}

// findInString reports whether substr occurs anywhere in s.
func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestGenerateKBQueryEmbedding_NoProvider verifies the helper returns
// an explicit error when no knowledgebase embedding provider is set.
func TestGenerateKBQueryEmbedding_NoProvider(t *testing.T) {
	cfg := &config.Config{}
	_, _, err := generateKBQueryEmbedding(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("expected error when KB embedding provider is unset")
	}
	if !strings.Contains(err.Error(), "provider not configured") {
		t.Errorf("expected 'provider not configured' error, got: %v", err)
	}
}

// TestGenerateKBQueryEmbedding_Gemini exercises the full Gemini code
// path for the knowledgebase helper, confirming the new
// EmbeddingGeminiAPIKey / EmbeddingGeminiBaseURL config fields wire
// through to the embedding factory.
func TestGenerateKBQueryEmbedding_Gemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.5,0.6,0.7]}}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		Knowledgebase: config.KnowledgebaseConfig{
			EmbeddingProvider:      "gemini",
			EmbeddingModel:         "gemini-embedding-001",
			EmbeddingGeminiAPIKey:  "kb-test-key",
			EmbeddingGeminiBaseURL: srv.URL,
		},
	}

	vec, provider, err := generateKBQueryEmbedding(context.Background(), cfg, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "gemini" {
		t.Errorf("expected provider 'gemini', got %q", provider)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3-element vector, got %d elements", len(vec))
	}
	if vec[0] != float32(0.5) {
		t.Errorf("expected first element 0.5, got %v", vec[0])
	}
}

// TestGenerateKBQueryEmbedding_InvalidProvider verifies factory errors
// are propagated for unsupported provider values.
func TestGenerateKBQueryEmbedding_InvalidProvider(t *testing.T) {
	cfg := &config.Config{
		Knowledgebase: config.KnowledgebaseConfig{
			EmbeddingProvider: "unknown",
		},
	}
	_, _, err := generateKBQueryEmbedding(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("expected error for invalid KB provider, got nil")
	}
}
