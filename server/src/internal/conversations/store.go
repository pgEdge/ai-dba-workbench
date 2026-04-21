/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors for conversation operations
var (
	// ErrNotFound is returned when a conversation is not found
	ErrNotFound = errors.New("conversation not found")

	// ErrAccessDenied is returned when access to a conversation is denied
	ErrAccessDenied = errors.New("access denied")
)

// Message represents a single message in a conversation
type Message struct {
	Role      string           `json:"role"`
	Content   any              `json:"content"`
	Timestamp string           `json:"timestamp,omitempty"`
	Provider  string           `json:"provider,omitempty"`
	Model     string           `json:"model,omitempty"`
	Activity  []map[string]any `json:"activity,omitempty"`
	IsError   bool             `json:"isError,omitempty"`
}

// Conversation represents a stored conversation
type Conversation struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Title      string    `json:"title"`
	Provider   string    `json:"provider,omitempty"`
	Model      string    `json:"model,omitempty"`
	Connection string    `json:"connection,omitempty"`
	Messages   []Message `json:"messages"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ConversationSummary provides a lightweight view for listing
type ConversationSummary struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Connection string    `json:"connection,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Preview    string    `json:"preview"`
}

// Store manages conversation persistence using PostgreSQL
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new conversation store backed by PostgreSQL
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// generateID creates a unique conversation ID
func generateID() string {
	return fmt.Sprintf("conv_%d", time.Now().UnixNano())
}

// generateTitle creates a title from the first user message
func generateTitle(messages []Message) string {
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}

		content := ""
		switch c := msg.Content.(type) {
		case string:
			content = c
		default:
			// For non-string content, use a default
			content = "New conversation"
		}
		// Truncate to reasonable length
		if len(content) > 50 {
			return content[:47] + "..."
		}
		if content == "" {
			return "New conversation"
		}
		return content
	}
	return "New conversation"
}

// Create creates a new conversation
func (s *Store) Create(ctx context.Context, username, provider, model, connection string, messages []Message) (*Conversation, error) {
	conv := &Conversation{
		ID:         generateID(),
		Username:   username,
		Title:      generateTitle(messages),
		Provider:   provider,
		Model:      model,
		Connection: connection,
		Messages:   messages,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	messagesJSON, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal messages: %w", err)
	}

	// nosemgrep: go-sql-concat-sqli
	_, err = s.pool.Exec(ctx,
		`INSERT INTO conversations (id, username, title, provider, model, connection, messages, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		conv.ID, conv.Username, conv.Title, conv.Provider, conv.Model, conv.Connection, messagesJSON,
		conv.CreatedAt, conv.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert conversation: %w", err)
	}

	return conv, nil
}

// Update updates an existing conversation
func (s *Store) Update(ctx context.Context, id, username, provider, model, connection string, messages []Message) (*Conversation, error) {
	// Verify ownership
	var existingUsername string
	err := s.pool.QueryRow(ctx,
		"SELECT username FROM conversations WHERE id = $1", id,
	).Scan(&existingUsername)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query conversation: %w", err)
	}
	if existingUsername != username {
		return nil, ErrAccessDenied
	}

	messagesJSON, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal messages: %w", err)
	}

	updatedAt := time.Now().UTC()

	_, err = s.pool.Exec(ctx,
		`UPDATE conversations
         SET provider = $1, model = $2, connection = $3, messages = $4, updated_at = $5
         WHERE id = $6 AND username = $7`,
		provider, model, connection, messagesJSON, updatedAt, id, username,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update conversation: %w", err)
	}

	// Fetch updated conversation
	return s.get(ctx, id, username)
}

// Get retrieves a conversation by ID
func (s *Store) Get(ctx context.Context, id, username string) (*Conversation, error) {
	return s.get(ctx, id, username)
}

// get retrieves a conversation (internal helper)
func (s *Store) get(ctx context.Context, id, username string) (*Conversation, error) {
	var conv Conversation
	var messagesJSON []byte

	err := s.pool.QueryRow(ctx,
		`SELECT id, username, title, provider, model, connection, messages, created_at, updated_at
         FROM conversations
         WHERE id = $1 AND username = $2`,
		id, username,
	).Scan(&conv.ID, &conv.Username, &conv.Title, &conv.Provider, &conv.Model, &conv.Connection,
		&messagesJSON, &conv.CreatedAt, &conv.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query conversation: %w", err)
	}

	if err := json.Unmarshal(messagesJSON, &conv.Messages); err != nil {
		return nil, fmt.Errorf("failed to unmarshal messages: %w", err)
	}

	return &conv, nil
}

// List lists all conversations for a user
func (s *Store) List(ctx context.Context, username string, limit, offset int) ([]ConversationSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, title, connection, messages, created_at, updated_at
         FROM conversations
         WHERE username = $1
         ORDER BY updated_at DESC
         LIMIT $2 OFFSET $3`,
		username, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query conversations: %w", err)
	}
	defer rows.Close()

	var summaries []ConversationSummary
	for rows.Next() {
		var summary ConversationSummary
		var messagesJSON []byte

		if err := rows.Scan(&summary.ID, &summary.Title, &summary.Connection, &messagesJSON,
			&summary.CreatedAt, &summary.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Extract preview from first user message
		var messages []Message
		if err := json.Unmarshal(messagesJSON, &messages); err == nil {
			for _, msg := range messages {
				if msg.Role == "user" {
					if content, ok := msg.Content.(string); ok {
						if len(content) > 100 {
							summary.Preview = content[:97] + "..."
						} else {
							summary.Preview = content
						}
						break
					}
				}
			}
		}

		summaries = append(summaries, summary)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return summaries, nil
}

// Rename renames a conversation
func (s *Store) Rename(ctx context.Context, id, username, title string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE conversations SET title = $1, updated_at = $2
         WHERE id = $3 AND username = $4`,
		title, time.Now().UTC(), id, username,
	)
	if err != nil {
		return fmt.Errorf("failed to rename conversation: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Delete deletes a conversation
func (s *Store) Delete(ctx context.Context, id, username string) error {
	tag, err := s.pool.Exec(ctx,
		"DELETE FROM conversations WHERE id = $1 AND username = $2",
		id, username,
	)
	if err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteAll deletes all conversations for a user
func (s *Store) DeleteAll(ctx context.Context, username string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		"DELETE FROM conversations WHERE username = $1",
		username,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to delete conversations: %w", err)
	}

	return tag.RowsAffected(), nil
}
