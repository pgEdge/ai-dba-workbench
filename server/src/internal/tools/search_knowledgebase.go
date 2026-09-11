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
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/pgEdge/pgedge-go-llm-lib/llm/vec"
	"github.com/pgedge/ai-workbench/pkg/embedding"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	_ "modernc.org/sqlite"
)

// SearchKnowledgebaseTool creates the search_knowledgebase tool for searching documentation
func SearchKnowledgebaseTool(kbPath string, cfg *config.Config) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "search_knowledgebase",
			Description: `Search the pre-built documentation knowledgebase for relevant information.

<critical>
IMPORTANT: Product names require EXACT matches. "pgEdge" will NOT match
"pgEdge RAG Server", "pgEdge Cloud", or "pgEdge Platform" - these are
separate products.

ALWAYS call with list_products=true FIRST to discover exact product names
before filtering by project_names.
</critical>

Use this tool when you need information about:
- PostgreSQL features, syntax, functions
- pgEdge products and capabilities
- Other documented products and technologies

The knowledgebase contains chunked, embedded documentation from multiple sources
with semantic search capabilities.

Note: In this tool, "project" and "product" are used interchangeably - they
both refer to the software product/project being documented.

<workflow>
1. First call: {"list_products": true} to see available products
2. Note the EXACT product names from the output
3. Search with exact names: {"query": "...", "project_names": ["Exact Name"]}
</workflow>

<troubleshooting>
If you get zero results:
- You likely have the wrong product name - call list_products=true
- Try searching without project_names filter to see what's available
- Check for typos or partial names (e.g., "pgEdge" vs "pgEdge RAG Server")
</troubleshooting>

<examples>
✓ {"list_products": true} - ALWAYS do this first!
✓ {"query": "PostgreSQL window functions"}
✓ {"query": "RAG overview", "project_names": ["pgEdge RAG Server"]}
✓ {"query": "replication", "project_names": ["pgEdge Platform"]}
✓ {"query": "JSON functions", "project_names": ["PostgreSQL"], "project_versions": ["17"]}
</examples>`,
			CompactDescription: `Search the pre-built documentation knowledgebase for PostgreSQL and pgEdge product information. Filter by project name and version. Use list_products=true to see available documentation.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language search query (required unless list_products is true)",
					},
					"project_names": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by project/product name(s) (e.g., ['PostgreSQL'], ['pgEdge', 'pgAdmin'])",
					},
					"project_versions": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by project/product version(s) (e.g., ['17'], ['16', '17'])",
					},
					"top_n": map[string]any{
						"type":        "integer",
						"description": "Number of results to return (default: 5, max: 20)",
						"default":     5,
					},
					"list_products": map[string]any{
						"type":        "boolean",
						"description": "If true, returns only the list of available products and versions in the knowledgebase (ignores other parameters). Use this to discover what documentation is available before searching.",
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			// Check for list_products mode first
			if listProducts, ok := args["list_products"].(bool); ok && listProducts {
				products, err := listKBProducts(kbPath)
				if err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to list products: %v", err))
				}
				return mcp.NewToolSuccess(products)
			}

			// Validate query
			query, errResp := ValidateStringParam(args, "query")
			if errResp != nil {
				return *errResp, nil
			}

			query = strings.TrimSpace(query)
			if query == "" {
				return mcp.NewToolError("query parameter is required when not using list_products")
			}

			// Get optional parameters
			var projectNames, projectVersions []string
			topN := 5

			// Extract project_names array
			if pn, ok := args["project_names"].([]any); ok {
				for _, v := range pn {
					if s, ok := v.(string); ok && s != "" {
						projectNames = append(projectNames, s)
					}
				}
			}
			// Extract project_versions array
			if pv, ok := args["project_versions"].([]any); ok {
				for _, v := range pv {
					if s, ok := v.(string); ok && s != "" {
						projectVersions = append(projectVersions, s)
					}
				}
			}
			if tn, ok := args["top_n"].(float64); ok {
				topN = int(tn)
				if topN < 1 {
					topN = 1
				}
				if topN > 20 {
					topN = 20
				}
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Generate query embedding
			queryEmbedding, provider, err := generateKBQueryEmbedding(ctx, cfg, query)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to generate query embedding: %v", err))
			}

			// Search knowledgebase
			results, err := searchKB(kbPath, queryEmbedding, projectNames, projectVersions, topN, provider)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Knowledgebase search failed: %v", err))
			}

			if len(results) == 0 {
				msg := fmt.Sprintf("No results found for query: %q", query)
				if len(projectNames) > 0 {
					msg += fmt.Sprintf(" (projects: %s", strings.Join(projectNames, ", "))
					if len(projectVersions) > 0 {
						msg += fmt.Sprintf("; versions: %s", strings.Join(projectVersions, ", "))
					}
					msg += ")"
				}
				return mcp.NewToolSuccess(msg)
			}

			// Format results
			output := formatKBResults(results, query, projectNames, projectVersions)
			return mcp.NewToolSuccess(output)
		},
	}
}

// KBSearchResult represents a search result from the knowledgebase
type KBSearchResult struct {
	Text           string
	Title          string
	Section        string
	ProjectName    string
	ProjectVersion string
	FilePath       string
	Similarity     float64
}

// listKBProducts returns a formatted list of all products and versions in the knowledgebase
func listKBProducts(kbPath string) (string, error) {
	db, err := sql.Open("sqlite", kbPath)
	if err != nil {
		return "", fmt.Errorf("failed to open knowledgebase: %w", err)
	}
	defer db.Close()

	// Query distinct products and versions with chunk counts
	rows, err := db.Query(`
        SELECT project_name, project_version, COUNT(*) as chunk_count
        FROM chunks
        GROUP BY project_name, project_version
        ORDER BY project_name, project_version
    `)
	if err != nil {
		return "", fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var sb strings.Builder
	sb.WriteString("Available Products in Knowledgebase\n")
	sb.WriteString(strings.Repeat("=", 50))
	sb.WriteString("\n\n")

	currentProduct := ""
	totalChunks := 0

	for rows.Next() {
		var name, version string
		var count int
		if err := rows.Scan(&name, &version, &count); err != nil {
			continue
		}

		if name != currentProduct {
			if currentProduct != "" {
				sb.WriteString("\n")
			}
			fmt.Fprintf(&sb, "Product: %s\n", name)
			currentProduct = name
		}

		if version != "" {
			fmt.Fprintf(&sb, "  - Version %s (%d chunks)\n", version, count)
		} else {
			fmt.Fprintf(&sb, "  - (no version) (%d chunks)\n", count)
		}
		totalChunks += count
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating products: %w", err)
	}

	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", 50))
	fmt.Fprintf(&sb, "\nTotal: %d chunks across all products\n", totalChunks)

	return sb.String(), nil
}

// generateKBQueryEmbedding embeds a search query using the
// knowledgebase-specific embedding configuration, which is deliberately
// independent of the generate_embeddings tool. It returns the query
// vector alongside the configured provider name, so that searchKB can
// compare the query against that provider's stored embeddings.
func generateKBQueryEmbedding(ctx context.Context, serverCfg *config.Config, queryText string) ([]float32, string, error) {
	// Use KB-specific embedding configuration (independent of generate_embeddings tool)
	kbCfg := serverCfg.Knowledgebase
	if kbCfg.EmbeddingProvider == "" {
		return nil, "", fmt.Errorf("knowledgebase embedding provider not configured")
	}

	embCfg := embedding.Config{
		Provider:      kbCfg.EmbeddingProvider,
		Model:         kbCfg.EmbeddingModel,
		VoyageAPIKey:  kbCfg.EmbeddingVoyageAPIKey,
		VoyageBaseURL: kbCfg.EmbeddingVoyageBaseURL,
		OpenAIAPIKey:  kbCfg.EmbeddingOpenAIAPIKey,
		OpenAIBaseURL: kbCfg.EmbeddingOpenAIBaseURL,
		GeminiAPIKey:  kbCfg.EmbeddingGeminiAPIKey,
		GeminiBaseURL: kbCfg.EmbeddingGeminiBaseURL,
		OllamaURL:     kbCfg.EmbeddingOllamaURL,
	}

	provider, err := embedding.NewProvider(embCfg)
	if err != nil {
		return nil, "", err
	}

	vector, err := provider.Embed(ctx, queryText)
	if err != nil {
		return nil, "", err
	}

	if len(vector) == 0 {
		return nil, "", fmt.Errorf("received empty embedding vector")
	}

	// Convert float64 to float32
	vector32 := vec.Float64ToFloat32(vector)

	return vector32, embCfg.Provider, nil
}

// kbEmbeddingColumns is the fixed allow-list of embedding columns that
// searchKB understands, in a stable order. Knowledgebase files vary by
// vintage: older ones carry no gemini_embedding column at all, and a
// provider-specific build may carry only one of these columns. These
// names are only ever compared against the column names the driver
// reports for a result set; none of them is interpolated into SQL.
var kbEmbeddingColumns = []string{
	"openai_embedding",
	"voyage_embedding",
	"ollama_embedding",
	"gemini_embedding",
}

// kbProviderColumn maps a configured embedding provider onto the column
// holding that provider's vectors. Unrecognized providers fall through
// to openai, matching the historic behavior of this tool.
func kbProviderColumn(provider string) string {
	switch strings.ToLower(provider) {
	case "voyage":
		return "voyage_embedding"
	case "ollama":
		return "ollama_embedding"
	case "gemini":
		return "gemini_embedding"
	default: // openai
		return "openai_embedding"
	}
}

// kbProviderName renders an embedding column as its provider name, for
// use in user-facing error messages.
func kbProviderName(column string) string {
	return strings.TrimSuffix(column, "_embedding")
}

// kbAvailableProviders reports which of the supported embedding
// providers the given result-set columns carry, in kbEmbeddingColumns
// order, so that an error can tell the user what the knowledgebase does
// contain.
func kbAvailableProviders(columns []string) []string {
	present := make(map[string]bool, len(columns))
	for _, name := range columns {
		present[name] = true
	}

	var providers []string
	for _, column := range kbEmbeddingColumns {
		if present[column] {
			providers = append(providers, kbProviderName(column))
		}
	}
	return providers
}

// kbMissingProviderError explains that the knowledgebase file carries no
// column for the configured provider at all. Substituting another
// provider's vectors would compare embeddings of differing dimensions
// and score every chunk alike, so this is a hard error rather than a
// silent degradation.
func kbMissingProviderError(kbPath, providerColumn string, available []string) error {
	if len(available) == 0 {
		return fmt.Errorf(
			"knowledgebase %q carries no embeddings at all (the chunks table has none of the "+
				"supported embedding columns: %s), so it cannot be searched with provider %q",
			kbPath, strings.Join(kbEmbeddingColumns, ", "), kbProviderName(providerColumn))
	}
	return fmt.Errorf(
		"knowledgebase %q carries no %q embeddings (it has no %s column; embeddings present: %s); "+
			"the configured knowledgebase embedding provider must match the provider used to build the "+
			"knowledgebase, so either rebuild the knowledgebase with %q embeddings or configure one of: %s",
		kbPath, kbProviderName(providerColumn), providerColumn,
		strings.Join(available, ", "), kbProviderName(providerColumn),
		strings.Join(available, ", "))
}

// kbEmptyProviderError explains that the provider's column exists but is
// empty for every chunk the search matched, which leaves nothing to rank
// and would otherwise be reported as a bland "no results".
func kbEmptyProviderError(kbPath, providerColumn string, rowCount int) error {
	return fmt.Errorf(
		"knowledgebase %q has an empty %s column for all %d matching chunks, so it carries no %q "+
			"embeddings; rebuild the knowledgebase with %q embeddings or configure a provider whose "+
			"embeddings it contains",
		kbPath, providerColumn, rowCount, kbProviderName(providerColumn),
		kbProviderName(providerColumn))
}

// kbSearchQuery builds the chunks query and its bound arguments. The
// statement is a literal that grows only by hard-coded "?" placeholder
// strings; the project name and version values travel in the returned
// argument slice and are bound as query parameters, never interpolated
// into SQL, so this is safe from SQL injection. It selects every column
// because the embedding columns a knowledgebase carries vary by vintage;
// searchKB resolves the ones it needs by name from the result set, which
// keeps identifiers out of the statement entirely.
func kbSearchQuery(projectNames, projectVersions []string) (string, []any) {
	var queryBuilder strings.Builder
	queryBuilder.WriteString(`
        SELECT * FROM chunks
        WHERE 1=1
    `)
	args := []any{}

	// Add project_names filter with IN clause
	if len(projectNames) > 0 {
		placeholders := make([]string, len(projectNames))
		for i, name := range projectNames {
			placeholders[i] = "?"
			args = append(args, name)
		}
		queryBuilder.WriteString(" AND project_name IN (")
		queryBuilder.WriteString(strings.Join(placeholders, ", "))
		queryBuilder.WriteString(")")
	}

	// Add project_versions filter with IN clause
	if len(projectVersions) > 0 {
		placeholders := make([]string, len(projectVersions))
		for i, version := range projectVersions {
			placeholders[i] = "?"
			args = append(args, version)
		}
		queryBuilder.WriteString(" AND project_version IN (")
		queryBuilder.WriteString(strings.Join(placeholders, ", "))
		queryBuilder.WriteString(")")
	}

	return queryBuilder.String(), args
}

// kbChunkRow holds the values scanned from one chunks row: the metadata
// searchKB reports, plus the embedding blob of the configured provider.
type kbChunkRow struct {
	text           string
	title          string
	section        string
	projectName    string
	projectVersion string
	filePath       string
	embedding      []byte
}

// kbScanTargets builds the Scan destinations for a result set whose
// columns are given, in order. Every destination is resolved by column
// name, so a knowledgebase file may order or extend its columns freely;
// columns the search does not need are read into a discard destination.
// The returned slice writes into row on each Scan.
func kbScanTargets(columns []string, providerColumn string, row *kbChunkRow) []any {
	wanted := map[string]any{
		"text":            &row.text,
		"title":           &row.title,
		"section":         &row.section,
		"project_name":    &row.projectName,
		"project_version": &row.projectVersion,
		"file_path":       &row.filePath,
		providerColumn:    &row.embedding,
	}

	targets := make([]any, len(columns))
	for i, name := range columns {
		if dest, ok := wanted[name]; ok {
			targets[i] = dest
			continue
		}
		targets[i] = new(any)
	}
	return targets
}

// kbRankChunks scores every row of rows against queryEmbedding using the
// embedding in providerColumn, whose position is resolved by name from
// columns, and returns the unsorted results. Rows that cannot be
// scanned, that hold no embedding for the provider, or whose embedding is
// malformed are skipped rather than misranked; if that leaves nothing to
// rank because no row carried an embedding for the provider, the
// resulting error says so rather than reporting an empty search.
func kbRankChunks(rows *sql.Rows, columns []string, kbPath, providerColumn string, queryEmbedding []float32) ([]KBSearchResult, error) {
	var results []KBSearchResult
	var rowCount, emptyProviderRows int

	var row kbChunkRow
	scanTargets := kbScanTargets(columns, providerColumn, &row)

	for rows.Next() {
		row = kbChunkRow{}
		if err := rows.Scan(scanTargets...); err != nil {
			continue
		}
		rowCount++

		if len(row.embedding) == 0 {
			emptyProviderRows++
			continue
		}

		// Deserialize embedding
		docEmbedding := deserializeEmbedding(row.embedding)
		if len(docEmbedding) == 0 {
			continue
		}

		results = append(results, KBSearchResult{
			Text:           row.text,
			Title:          row.title,
			Section:        row.section,
			ProjectName:    row.projectName,
			ProjectVersion: row.projectVersion,
			FilePath:       row.filePath,
			Similarity:     cosineSimilarity(queryEmbedding, docEmbedding),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating search results: %w", err)
	}

	// Distinguish an empty knowledgebase, or a filter that matched
	// nothing, from a knowledgebase whose rows carry no embeddings for
	// the configured provider; the latter is a misconfiguration and
	// deserves an explicit error rather than a bland "no results".
	if len(results) == 0 && rowCount > 0 && emptyProviderRows == rowCount {
		return nil, kbEmptyProviderError(kbPath, providerColumn, rowCount)
	}
	return results, nil
}

// searchKB ranks the chunks of the knowledgebase at kbPath against
// queryEmbedding, optionally filtered by project name and version, and
// returns the topN most similar. Only the configured provider's
// embeddings are considered: a knowledgebase that carries none for that
// provider is reported as an error rather than searched with another
// provider's vectors, whose dimensions would not match.
func searchKB(kbPath string, queryEmbedding []float32, projectNames, projectVersions []string, topN int, provider string) ([]KBSearchResult, error) {
	// Open database
	db, err := sql.Open("sqlite", kbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open knowledgebase: %w", err)
	}
	defer db.Close()

	query, args := kbSearchQuery(projectNames, projectVersions)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query knowledgebase %q: %w", kbPath, err)
	}
	defer rows.Close()

	// Resolve the result-set columns before reading any rows, so that a
	// knowledgebase lacking the configured provider's embeddings fails
	// fast with an explanation rather than returning misranked chunks.
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect chunks table of knowledgebase %q: %w", kbPath, err)
	}

	providerColumn := kbProviderColumn(provider)
	if !slices.Contains(columns, providerColumn) {
		return nil, kbMissingProviderError(kbPath, providerColumn,
			kbAvailableProviders(columns))
	}

	results, err := kbRankChunks(rows, columns, kbPath, providerColumn, queryEmbedding)
	if err != nil {
		return nil, err
	}

	// Sort by similarity (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	// Return top N
	if len(results) > topN {
		results = results[:topN]
	}

	return results, nil
}

// deserializeEmbedding decodes an embedding stored as a little-endian
// sequence of float32 values. It returns nil for an empty blob, or for
// one whose length is not a whole number of float32 values, so that a
// malformed embedding causes the chunk to be skipped rather than
// misranked.
func deserializeEmbedding(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}

	out := make([]float32, len(data)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		out[i] = math.Float32frombits(bits)
	}
	return out
}

// cosineSimilarity returns the cosine similarity of two vectors. It
// returns 0 when the vectors differ in length, which is why searchKB
// must never mix providers: vectors of differing dimensions would score
// every chunk 0 and destroy the ranking.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// formatKBResults renders ranked search results as the plain-text report
// the tool returns, echoing the query and any project or version filter
// so that the caller can see what was actually searched.
func formatKBResults(results []KBSearchResult, query string, projectNames, projectVersions []string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Knowledgebase Search Results: %q\n", query)
	if len(projectNames) > 0 {
		fmt.Fprintf(&sb, "Filter - Projects: %s", strings.Join(projectNames, ", "))
		if len(projectVersions) > 0 {
			fmt.Fprintf(&sb, "; Versions: %s", strings.Join(projectVersions, ", "))
		}
		sb.WriteString("\n")
	} else if len(projectVersions) > 0 {
		fmt.Fprintf(&sb, "Filter - Versions: %s\n", strings.Join(projectVersions, ", "))
	}
	sb.WriteString(strings.Repeat("=", 80))
	sb.WriteString("\n\n")

	fmt.Fprintf(&sb, "Found %d relevant chunks:\n\n", len(results))

	for i, result := range results {
		fmt.Fprintf(&sb, "Result %d/%d\n", i+1, len(results))
		if result.ProjectVersion != "" {
			fmt.Fprintf(&sb, "Project: %s %s\n", result.ProjectName, result.ProjectVersion)
		} else {
			fmt.Fprintf(&sb, "Project: %s\n", result.ProjectName)
		}
		if result.Title != "" {
			fmt.Fprintf(&sb, "Title: %s\n", result.Title)
		}
		if result.Section != "" {
			fmt.Fprintf(&sb, "Section: %s\n", result.Section)
		}
		fmt.Fprintf(&sb, "Similarity: %.3f\n\n", result.Similarity)
		sb.WriteString(result.Text)
		sb.WriteString("\n\n")
		sb.WriteString(strings.Repeat("-", 80))
		sb.WriteString("\n\n")
	}

	sb.WriteString(strings.Repeat("=", 80))
	fmt.Fprintf(&sb, "\nTotal: %d results\n", len(results))

	return sb.String()
}
