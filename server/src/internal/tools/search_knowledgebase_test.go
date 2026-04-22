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
	"database/sql"
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

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

// createSearchKBChunksSchema creates the `chunks` table used by searchKB
// tests. Centralizing the DDL keeps the schema consistent across test
// fixtures.
func createSearchKBChunksSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	const schema = `
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
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
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

	// Serialize a float32 embedding to little-endian blob.
	encode := func(vec []float32) []byte {
		buf := make([]byte, len(vec)*4)
		for i, v := range vec {
			binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
		}
		return buf
	}

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

func TestSearchKB_FallsBackWhenProviderBlobMissing(t *testing.T) {
	path := buildSearchKBTestDB(t)

	// Query with provider "voyage" but our DB only has openai blobs.
	// The function falls back to the openai blob when voyage is empty.
	queryEmbedding := []float32{1, 0, 0}
	results, err := searchKB(path, queryEmbedding, nil, nil, 10, "voyage")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(results) == 0 {
		t.Errorf("expected fallback to openai embeddings, got 0 results")
	}
}

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

	results, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "voyage")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	// The two rows with no usable embedding should be skipped. The
	// voyage row matches directly, and the ollama row falls back from
	// voyage to ollama.
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Repeat with provider="ollama" so we hit the ollama branch first.
	resultsOllama, err := searchKB(path, []float32{1, 0, 0}, nil, nil, 10, "ollama")
	if err != nil {
		t.Fatalf("searchKB: %v", err)
	}
	if len(resultsOllama) != 2 {
		t.Errorf("expected 2 results, got %d", len(resultsOllama))
	}
}

func TestSearchKB_OpenFailure(t *testing.T) {
	// A path containing a null byte is invalid for SQLite and should
	// fail to open, exercising the error branch of searchKB.
	_, err := searchKB("/nonexistent/dir\x00/kb.sqlite", []float32{1, 0, 0},
		nil, nil, 10, "openai")
	if err == nil {
		t.Errorf("expected error from invalid path, got nil")
	}
}

// containsString checks if the string contains the substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findInString(s, substr))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
