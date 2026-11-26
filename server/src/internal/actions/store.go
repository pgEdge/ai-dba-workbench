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
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/rollback"
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
	// commitClaim commits a claim's transaction; nil selects
	// tx.Commit. Tests replace it to lose the COMMIT's reply.
	commitClaim func(ctx context.Context, tx pgx.Tx) error
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
//
// A revision replaces an earlier revision of the same parent by the same
// proposer that is still waiting for its review, with nothing approved
// or run: that is a proposal whose review failed (the reviewer errored,
// or the verdict could not be stored), and leaving it would block every
// later revision of the parent on idx_proposed_actions_parent. Such a
// row can never have been run, nor revised itself, since only a vetoed
// action can be. A revision that already has a verdict still returns
// ErrAlreadyRevised. A review still in flight for the replaced row then
// fails to store its verdict, because the row is gone, so a verdict can
// never land on statements it did not review.
func (s *Store) Create(ctx context.Context, n NewAction) (*Action, error) {
	args, err := insertArgs(n)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("create proposed action: %w", err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit
	if n.ParentActionID != nil {
		if _, err := tx.Exec(ctx, `
            DELETE FROM proposed_actions
             WHERE parent_action_id = $1 AND proposed_by = $2
               AND review_status = 'pending' AND approval = 'none'
               AND exec_status = 'not_started'`,
			*n.ParentActionID, n.ProposedBy); err != nil {
			return nil, fmt.Errorf("replace unreviewed revision: %w", err)
		}
	}
	a, err := scanAction(tx.QueryRow(ctx, `
        INSERT INTO proposed_actions
            (connection_id, database_name, statements, source, proposed_by,
             proposed_via, conversation_id, alert_id, parent_action_id,
             attempt, rationale, evidence, review_status)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'pending')
        RETURNING `+selectColumns, args...))
	if err != nil {
		return nil, createError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("create proposed action: %w", err)
	}
	return a, nil
}

// insertArgs returns the twelve parameters of Create's INSERT for n: the
// statements encoded as JSON (an empty list for none), the attempt
// raised to at least 1, and an empty rationale or evidence as NULL.
func insertArgs(n NewAction) ([]any, error) {
	stmts := n.Statements
	if stmts == nil {
		stmts = []string{}
	}
	statementsJSON, err := json.Marshal(stmts)
	if err != nil {
		return nil, fmt.Errorf("encode statements: %w", err)
	}
	attempt := max(n.Attempt, 1)
	var rationale *string
	if n.Rationale != "" {
		rationale = &n.Rationale
	}
	var evidence []byte
	if len(n.Evidence) > 0 {
		evidence = n.Evidence
	}
	return []any{
		n.ConnectionID, n.DatabaseName, statementsJSON, n.Source,
		n.ProposedBy, n.ProposedVia, n.ConversationID, n.AlertID,
		n.ParentActionID, attempt, rationale, evidence,
	}, nil
}

// createError maps a failed INSERT to ErrAlreadyRevised when the parent
// already has a revision, and wraps any other error.
func createError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "idx_proposed_actions_parent" {
		return ErrAlreadyRevised
	}
	return fmt.Errorf("create proposed action: %w", err)
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

// Reapprove records by as the approver of an action whose earlier
// approval or override (of the same kind, by previousBy) was never
// started, for a superuser who retries someone else's decision: the run
// is then on record as theirs. It is a compare-and-swap on the approver,
// so of several concurrent retries exactly one is recorded, and it
// returns ErrInvalidState when the action has started, or its decision
// or approver has changed, since it was read.
func (s *Store) Reapprove(ctx context.Context, id int64, approval, previousBy, by, via string) error {
	if approval != "approved" && approval != "overridden" {
		return fmt.Errorf("invalid approval %q", approval)
	}
	if via != "session" && via != "token" {
		return fmt.Errorf("invalid approval channel %q", via)
	}
	tag, err := s.pool.Exec(ctx, `
        UPDATE proposed_actions
           SET approved_by = $4, approved_via = $5,
               approved_at = now(), updated_at = now()
         WHERE id = $1 AND exec_status = 'not_started' AND approval = $2
           AND approved_by = $3`,
		id, approval, previousBy, by, via)
	if err != nil {
		return fmt.Errorf("reapprove action: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState
	}
	return nil
}

// claimReconcileTimeout bounds the read that settles a claim whose
// COMMIT failed. It runs on a context detached from the claim's, whose
// deadline may be what ended the claim.
const claimReconcileTimeout = 10 * time.Second

// The claim runs in a transaction under these local limits, so that
// once its client has given up the server either commits it or aborts
// it within claimSettledAfter: lock_timeout and statement_timeout end
// an UPDATE that waits or runs too long (the pool's own
// statement_timeout is configurable, and its default equals
// claimTimeout), and idle_in_transaction_session_timeout ends the
// session if the COMMIT does not arrive soon after, as when the client
// has gone. All of them are below claimTimeout, so the server gives up
// first.
const (
	claimLockTimeout      = 5 * time.Second
	claimStatementTimeout = 10 * time.Second
	claimIdleTimeout      = 5 * time.Second
)

// claimSettledAfter is how long after a claim begins it can no longer
// commit: two statements (the UPDATE and the COMMIT) of at most
// claimStatementTimeout each, two idle gaps before them of at most
// claimIdleTimeout each, and a margin. A release that matches nothing
// after this is final; before it, the COMMIT may still be on its way.
const claimSettledAfter = 2*claimStatementTimeout + 2*claimIdleTimeout + 10*time.Second

// errClaimUnsettled marks a claim whose COMMIT failed and that the
// read-back did not find held, so it may yet commit, or may have
// committed with the read-back failing. Executor.Start then settles it
// in the background (settleClaim).
var errClaimUnsettled = errors.New("the claim's outcome is not known")

// claim atomically moves a not-started action that has a recorded
// decision to executing under token, a fresh random value no other
// claim can have written: it passed review and was approved, or was
// vetoed and then overridden. A passed but unapproved action is never
// claimable, so the database is the final gate whatever the callers do.
// The claim records the start time in executed_at. It returns true for
// exactly one caller however many race for the same action; every other
// caller, and any action not eligible, gets false.
//
// The UPDATE runs in a transaction under the claim limits above, so an
// error before the COMMIT means nothing was claimed. Only a failed
// COMMIT leaves the outcome open: the row is then read back, and a row
// executing under token took effect and returns true, so the caller
// runs the action rather than leaving it executing with no run behind
// it. Otherwise the error wraps errClaimUnsettled, and the action is
// left as it is. Executor.Start is the only caller, since only it
// settles an unsettled claim.
func (s *Store) claim(ctx context.Context, id int64, token string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("claim action: %w", err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx, `
        SELECT set_config('lock_timeout', $1, true),
               set_config('statement_timeout', $2, true),
               set_config('idle_in_transaction_session_timeout', $3, true)`,
		pgMillis(claimLockTimeout), pgMillis(claimStatementTimeout),
		pgMillis(claimIdleTimeout)); err != nil {
		return false, fmt.Errorf("claim action: %w", err)
	}
	tag, err := tx.Exec(ctx, `
        UPDATE proposed_actions
           SET exec_status = 'executing', executed_at = now(),
               updated_at = now(), claim_token = $2
         WHERE id = $1
           AND exec_status = 'not_started'
           AND ((review_status = 'passed' AND approval = 'approved')
                OR (review_status = 'vetoed' AND approval = 'overridden'))`, id, token)
	if err != nil {
		return false, fmt.Errorf("claim action: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	commit := s.commitClaim
	if commit == nil {
		commit = func(ctx context.Context, tx pgx.Tx) error { return tx.Commit(ctx) }
	}
	if err := commit(ctx, tx); err == nil {
		return true, nil
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimReconcileTimeout)
	defer cancel()
	var held bool
	rerr := s.pool.QueryRow(rctx, `
        SELECT exec_status = 'executing' AND claim_token = $2
          FROM proposed_actions WHERE id = $1`, id, token).Scan(&held)
	if rerr == nil && held {
		return true, nil
	}
	if rerr != nil {
		return false, fmt.Errorf("claim action: %w; read back: %v", errClaimUnsettled, rerr)
	}
	return false, fmt.Errorf("claim action: %w", errClaimUnsettled)
}

// pgMillis renders d as a whole number of milliseconds, the unit
// PostgreSQL assumes for a bare timeout value.
func pgMillis(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10)
}

// releaseClaim returns an action that is executing under token to
// not_started, clearing the start time and the token as though it had
// never been claimed, and reports whether it did. It is only for a
// claim that no run was started under (one that reported
// errClaimUnsettled), so nothing has reached the target. A false
// result is final only after claimSettledAfter (see settleClaim). The approval
// stands, so the action can be started again. A row that is in any
// other state, or held under another token, is left alone.
func (s *Store) releaseClaim(ctx context.Context, id int64, token string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
        UPDATE proposed_actions
           SET exec_status = 'not_started', executed_at = NULL,
               claim_token = NULL, updated_at = now()
         WHERE id = $1 AND exec_status = 'executing' AND claim_token = $2`,
		id, token)
	if err != nil {
		return false, fmt.Errorf("release claim: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// FinishExecution records the outcome of an executing action; status is
// one of the exec_status end states. It also accepts an action that
// MarkInterruptedUnknown marked unknown whilst the run was still going
// (another server instance starting up, say), which it tells from a
// finished one by its results: the sweep leaves them NULL, whilst this
// method always writes them, so the run's real outcome replaces the
// guess but can never overwrite another recorded outcome. It returns
// ErrInvalidState when the action is in any other state.
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
               updated_at = now()
         WHERE id = $1
           AND (exec_status = 'executing'
                OR (exec_status = 'unknown' AND results IS NULL))`,
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
