/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound is returned when no action matches, or when the action
	// is not in a state the operation applies to.
	ErrNotFound = errors.New("action not found")

	// ErrInvalidState is returned by FinishExecution when the action is
	// not currently executing.
	ErrInvalidState = errors.New("action is not in the required state")

	// ErrAlreadyRevised is returned by Create when the parent action has
	// already been revised, which would branch the retry chain.
	ErrAlreadyRevised = errors.New("action has already been revised")
)

// Store reads and writes the proposed_actions table. The table has no
// updated_at trigger, so every UPDATE here sets updated_at itself.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool, the datastore pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const selectColumns = `id, connection_id, database_name, statements, source,
    proposed_by, proposed_via, conversation_id, alert_id, parent_action_id,
    attempt, rationale, evidence, review_status, review, approval,
    approved_by, approved_via, approved_at, exec_status, transactional,
    results, preview, undo, verification, outcome, created_at, updated_at,
    executed_at`

// scanAction reads one row selected with selectColumns.
func scanAction(row pgx.Row) (*Action, error) {
	var (
		a                                       Action
		statements, review, results             []byte
		evidence, preview, undo, verif, outcome []byte
	)
	err := row.Scan(
		&a.ID, &a.ConnectionID, &a.DatabaseName, &statements, &a.Source,
		&a.ProposedBy, &a.ProposedVia, &a.ConversationID, &a.AlertID,
		&a.ParentActionID, &a.Attempt, &a.Rationale, &evidence,
		&a.ReviewStatus, &review, &a.Approval, &a.ApprovedBy,
		&a.ApprovedVia, &a.ApprovedAt, &a.ExecStatus, &a.Transactional,
		&results, &preview, &undo, &verif, &outcome, &a.CreatedAt,
		&a.UpdatedAt, &a.ExecutedAt,
	)
	if err != nil {
		return nil, err
	}
	a.Statements = []string{}
	if err := json.Unmarshal(statements, &a.Statements); err != nil {
		return nil, fmt.Errorf("decode statements: %w", err)
	}
	if len(review) > 0 {
		a.Review = &Verdict{}
		if err := json.Unmarshal(review, a.Review); err != nil {
			return nil, fmt.Errorf("decode review: %w", err)
		}
	}
	if len(results) > 0 {
		if err := json.Unmarshal(results, &a.Results); err != nil {
			return nil, fmt.Errorf("decode results: %w", err)
		}
	}
	a.Evidence, a.Preview, a.Undo = evidence, preview, undo
	a.Verification, a.Outcome = verif, outcome
	return &a, nil
}

// Create inserts a new action with review pending, no approval and
// execution not started, and returns it.
func (s *Store) Create(ctx context.Context, n NewAction) (*Action, error) {
	stmts := n.Statements
	if stmts == nil {
		stmts = []string{}
	}
	statementsJSON, err := json.Marshal(stmts)
	if err != nil {
		return nil, fmt.Errorf("encode statements: %w", err)
	}
	attempt := n.Attempt
	if attempt < 1 {
		attempt = 1
	}
	var rationale *string
	if n.Rationale != "" {
		rationale = &n.Rationale
	}
	var evidence []byte
	if len(n.Evidence) > 0 {
		evidence = n.Evidence
	}
	a, err := scanAction(s.pool.QueryRow(ctx, `
        INSERT INTO proposed_actions
            (connection_id, database_name, statements, source, proposed_by,
             proposed_via, conversation_id, alert_id, parent_action_id,
             attempt, rationale, evidence, review_status)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'pending')
        RETURNING `+selectColumns,
		n.ConnectionID, n.DatabaseName, statementsJSON, n.Source,
		n.ProposedBy, n.ProposedVia, n.ConversationID, n.AlertID,
		n.ParentActionID, attempt, rationale, evidence))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			pgErr.ConstraintName == "idx_proposed_actions_parent" {
			return nil, ErrAlreadyRevised
		}
		return nil, fmt.Errorf("create proposed action: %w", err)
	}
	return a, nil
}

// Get returns the action with the given id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id int64) (*Action, error) {
	a, err := scanAction(s.pool.QueryRow(ctx,
		`SELECT `+selectColumns+` FROM proposed_actions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get proposed action: %w", err)
	}
	return a, nil
}

// SetReview records the reviewer's verdict; status is "passed" or
// "vetoed".
func (s *Store) SetReview(ctx context.Context, id int64, status string, v Verdict) error {
	if status != "passed" && status != "vetoed" {
		return fmt.Errorf("invalid review status %q", status)
	}
	if v.Reasons == nil {
		v.Reasons = []string{}
	}
	verdict, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode verdict: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `
        UPDATE proposed_actions
           SET review_status = $2, review = $3, updated_at = now()
         WHERE id = $1`, id, status, verdict)
	if err != nil {
		return fmt.Errorf("set review: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Approve records an approval ("approved") or an override of a veto
// ("overridden") by the named user. It is one-shot: it applies only to
// an action whose execution has not started and that has no approval
// recorded, so of several concurrent callers exactly one wins and the
// approver on record is the winner. It returns ErrNotFound when no such
// action exists and ErrInvalidState when the action is no longer
// awaiting a decision.
func (s *Store) Approve(ctx context.Context, id int64, approval, by, via string) error {
	if approval != "approved" && approval != "overridden" {
		return fmt.Errorf("invalid approval %q", approval)
	}
	if via != "session" && via != "token" {
		return fmt.Errorf("invalid approval channel %q", via)
	}
	tag, err := s.pool.Exec(ctx, `
        UPDATE proposed_actions
           SET approval = $2, approved_by = $3, approved_via = $4,
               approved_at = now(), updated_at = now()
         WHERE id = $1 AND exec_status = 'not_started' AND approval = 'none'`,
		id, approval, by, via)
	if err != nil {
		return fmt.Errorf("approve action: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM proposed_actions WHERE id = $1)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("approve action: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return ErrInvalidState
}

// ClaimForExecution atomically moves a not-started action that has
// either passed review or been overridden to executing. It returns true
// for exactly one caller however many race for the same action; every
// other caller, and any action not eligible, gets false.
func (s *Store) ClaimForExecution(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
        UPDATE proposed_actions
           SET exec_status = 'executing', updated_at = now()
         WHERE id = $1
           AND exec_status = 'not_started'
           AND (review_status = 'passed' OR approval = 'overridden')`, id)
	if err != nil {
		return false, fmt.Errorf("claim action: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// FinishExecution records the outcome of an executing action; status is
// one of the exec_status end states. It returns ErrInvalidState when
// the action is not executing.
func (s *Store) FinishExecution(ctx context.Context, id int64, status string, transactional bool, results []StatementResult) error {
	switch status {
	case "succeeded", "failed", "cancelled", "unknown": //nolint:misspell // value fixed by the table CHECK
	default:
		return fmt.Errorf("invalid execution status %q", status)
	}
	if results == nil {
		results = []StatementResult{}
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `
        UPDATE proposed_actions
           SET exec_status = $2, transactional = $3, results = $4,
               executed_at = now(), updated_at = now()
         WHERE id = $1 AND exec_status = 'executing'`,
		id, status, transactional, encoded)
	if err != nil {
		return fmt.Errorf("finish execution: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState
	}
	return nil
}

// MarkInterruptedUnknown marks every action left executing, which means
// the server stopped part-way through, as unknown, and returns how many
// it changed. It is meant to run once at startup.
func (s *Store) MarkInterruptedUnknown(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
        UPDATE proposed_actions
           SET exec_status = 'unknown', updated_at = now()
         WHERE exec_status = 'executing'`)
	if err != nil {
		return 0, fmt.Errorf("mark interrupted actions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// LinkConversation attaches the given actions to a conversation, but
// only those proposed by username that have no conversation yet.
func (s *Store) LinkConversation(ctx context.Context, ids []int64, conversationID, username string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
        UPDATE proposed_actions
           SET conversation_id = $2, updated_at = now()
         WHERE id = ANY($1) AND proposed_by = $3
           AND conversation_id IS NULL`, ids, conversationID, username)
	if err != nil {
		return fmt.Errorf("link conversation: %w", err)
	}
	return nil
}
