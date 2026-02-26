/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors for memory operations
var (
	// ErrNotFound is returned when a memory is not found
	ErrNotFound = errors.New("memory not found")

	// ErrAccessDenied is returned when access to a memory is denied
	ErrAccessDenied = errors.New("access denied")
)

// Memory represents a row in the chat_memories table
type Memory struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Scope     string    `json:"scope"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Pinned    bool      `json:"pinned"`
	ModelName string    `json:"model_name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store manages chat memory persistence using PostgreSQL
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new memory store backed by PostgreSQL
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Store inserts a new memory and returns the created record.
// The embedding parameter may be nil when no vector is available.
func (s *Store) Store(
	ctx context.Context,
	username, scope, category, content string,
	pinned bool,
	embedding []float32,
	modelName string,
) (*Memory, error) {
	// Validate scope invariants
	if scope != "user" && scope != "system" {
		return nil, fmt.Errorf("invalid scope %q: must be \"user\" or \"system\"", scope)
	}
	if scope == "user" && username == "" {
		return nil, fmt.Errorf("username must not be empty for user-scoped memories")
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("content must not be empty")
	}

	now := time.Now().UTC()

	var embeddingArg interface{}
	if embedding != nil {
		embeddingArg = formatEmbedding(embedding)
	}

	mem := &Memory{
		Username:  username,
		Scope:     scope,
		Category:  category,
		Content:   content,
		Pinned:    pinned,
		ModelName: modelName,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := s.pool.QueryRow(ctx,
		`INSERT INTO chat_memories
             (username, scope, category, content, pinned, embedding,
              model_name, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6::vector, $7, $8, $9)
         RETURNING id`,
		mem.Username, mem.Scope, mem.Category, mem.Content,
		mem.Pinned, embeddingArg, mem.ModelName,
		mem.CreatedAt, mem.UpdatedAt,
	).Scan(&mem.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to insert memory: %w", err)
	}

	return mem, nil
}

// Delete removes a memory by ID. The caller must own the memory
// (username must match the stored username).
func (s *Store) Delete(ctx context.Context, id int64, username string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM chat_memories
         WHERE id = $1 AND username = $2`,
		id, username,
	)
	if err != nil {
		return fmt.Errorf("failed to delete memory: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Search finds memories using vector similarity or text matching.
// Results always include both the user's own memories and system-scope
// memories. When embedding is non-nil, results are ordered by cosine
// distance; otherwise a text ILIKE search is performed on content.
// Pass an empty string for category or scope to skip those filters.
func (s *Store) Search(
	ctx context.Context,
	username, query, category, scope string,
	limit int,
	embedding []float32,
) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	if embedding != nil {
		return s.searchByVector(ctx, username, category, scope, limit, embedding)
	}
	return s.searchByText(ctx, username, query, category, scope, limit)
}

// searchByVector performs cosine-distance similarity search.
func (s *Store) searchByVector(
	ctx context.Context,
	username, category, scope string,
	limit int,
	embedding []float32,
) ([]Memory, error) {
	embStr := formatEmbedding(embedding)

	var conditions []string
	args := []interface{}{embStr}
	paramIdx := 2

	// Visibility: user's own memories OR system scope
	conditions = append(conditions,
		fmt.Sprintf("(username = $%d OR scope = 'system')", paramIdx))
	args = append(args, username)
	paramIdx++

	if category != "" {
		conditions = append(conditions,
			fmt.Sprintf("category = $%d", paramIdx))
		args = append(args, category)
		paramIdx++
	}

	if scope != "" {
		conditions = append(conditions,
			fmt.Sprintf("scope = $%d", paramIdx))
		args = append(args, scope)
		paramIdx++
	}

	// Require embedding to be non-null for vector ordering
	conditions = append(conditions, "embedding IS NOT NULL")

	whereClause := strings.Join(conditions, " AND ")
	args = append(args, limit)

	query := fmt.Sprintf(
		`SELECT id, username, scope, category, content, pinned,
                model_name, created_at, updated_at
         FROM chat_memories
         WHERE %s
         ORDER BY embedding <=> $1::vector
         LIMIT $%d`,
		whereClause, paramIdx)

	return s.queryMemories(ctx, query, args...)
}

// searchByText performs a text ILIKE search on the content column.
func (s *Store) searchByText(
	ctx context.Context,
	username, query, category, scope string,
	limit int,
) ([]Memory, error) {
	var conditions []string
	var args []interface{}
	paramIdx := 1

	// Visibility: user's own memories OR system scope
	conditions = append(conditions,
		fmt.Sprintf("(username = $%d OR scope = 'system')", paramIdx))
	args = append(args, username)
	paramIdx++

	if query != "" {
		conditions = append(conditions,
			fmt.Sprintf("content ILIKE $%d", paramIdx))
		args = append(args, "%"+query+"%")
		paramIdx++
	}

	if category != "" {
		conditions = append(conditions,
			fmt.Sprintf("category = $%d", paramIdx))
		args = append(args, category)
		paramIdx++
	}

	if scope != "" {
		conditions = append(conditions,
			fmt.Sprintf("scope = $%d", paramIdx))
		args = append(args, scope)
		paramIdx++
	}

	whereClause := strings.Join(conditions, " AND ")
	args = append(args, limit)

	sqlQuery := fmt.Sprintf(
		`SELECT id, username, scope, category, content, pinned,
                model_name, created_at, updated_at
         FROM chat_memories
         WHERE %s
         ORDER BY updated_at DESC
         LIMIT $%d`,
		whereClause, paramIdx)

	return s.queryMemories(ctx, sqlQuery, args...)
}

// GetPinned returns all pinned memories visible to the given user.
// This includes the user's own pinned memories and system-scope
// pinned memories.
func (s *Store) GetPinned(ctx context.Context, username string) ([]Memory, error) {
	return s.queryMemories(ctx,
		`SELECT id, username, scope, category, content, pinned,
                model_name, created_at, updated_at
         FROM chat_memories
         WHERE pinned = TRUE
           AND (username = $1 OR scope = 'system')
         ORDER BY updated_at DESC`,
		username,
	)
}

// ListByUser returns memories for a user, optionally filtered by
// category. Results are ordered by most recently updated first.
func (s *Store) ListByUser(
	ctx context.Context,
	username, category string,
	limit int,
) ([]Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	if category != "" {
		return s.queryMemories(ctx,
			`SELECT id, username, scope, category, content, pinned,
                    model_name, created_at, updated_at
             FROM chat_memories
             WHERE (username = $1 OR scope = 'system')
               AND category = $2
             ORDER BY updated_at DESC
             LIMIT $3`,
			username, category, limit,
		)
	}

	return s.queryMemories(ctx,
		`SELECT id, username, scope, category, content, pinned,
                model_name, created_at, updated_at
         FROM chat_memories
         WHERE (username = $1 OR scope = 'system')
         ORDER BY updated_at DESC
         LIMIT $2`,
		username, limit,
	)
}

// queryMemories is an internal helper that executes a query and scans
// the result set into a slice of Memory values.
func (s *Store) queryMemories(
	ctx context.Context,
	query string,
	args ...interface{},
) ([]Memory, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(
			&m.ID, &m.Username, &m.Scope, &m.Category,
			&m.Content, &m.Pinned, &m.ModelName,
			&m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan memory row: %w", err)
		}
		memories = append(memories, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating memory rows: %w", err)
	}

	return memories, nil
}

// formatEmbedding converts a float32 slice into the PostgreSQL vector
// literal format: [0.1,0.2,...].
func formatEmbedding(v []float32) string {
	parts := make([]string, len(v))
	for i, val := range v {
		parts[i] = fmt.Sprintf("%g", val)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
