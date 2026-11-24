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
	"log"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/sqllex"
)

// Errors returned by the review service.
var (
	// ErrEmptyProposal is returned when the SQL holds no statements.
	ErrEmptyProposal = errors.New("proposal contains no statements")

	// ErrNoWrites is returned when every statement is read-only.
	ErrNoWrites = errors.New("proposal contains no writes; run read-only queries directly")

	// ErrTransactionControl is returned when a statement controls
	// transactions, which the executor manages itself.
	ErrTransactionControl = errors.New("proposal must not contain transaction control statements")

	// ErrTimeoutOverride is returned when a statement would change the
	// executor's lock or statement timeout, or reset session settings.
	ErrTimeoutOverride = errors.New("proposal must not change lock_timeout or statement_timeout or reset session settings")

	// ErrBadRevision is returned when the revised action is not a vetoed
	// action on the same connection from the same proposer, when it has
	// already been revised, or when a concurrent revision replaced this
	// one before its review was stored.
	ErrBadRevision = errors.New("action cannot be revised")

	// ErrChainExhausted is returned when a proposal would exceed the
	// maximum number of attempts in its chain.
	ErrChainExhausted = errors.New("maximum number of attempts reached")

	// ErrOverrideNotAllowed is returned when the caller or the action's
	// review status does not permit an override.
	ErrOverrideNotAllowed = errors.New("override not allowed")

	// ErrConfirmMismatch is returned when the typed confirmation is not
	// the connection's name.
	ErrConfirmMismatch = errors.New("confirmation does not match the connection name")
)

// reviewStore is the part of Store the review service uses.
type reviewStore interface {
	Create(ctx context.Context, n NewAction) (*Action, error)
	Get(ctx context.Context, id int64) (*Action, error)
	SetReview(ctx context.Context, id int64, status string, v Verdict) error
}

// ProposeRequest describes an action put forward for review. The
// proposer's username and channel come from the request context.
type ProposeRequest struct {
	ConnectionID    int
	DatabaseName    *string
	SQL             string
	Rationale       string
	Source          string
	RevisesActionID *int64
	ConversationID  *string
	AlertID         *int64
	Evidence        json.RawMessage
}

// ProposeResult is the outcome of a proposal.
type ProposeResult struct {
	Action       *Action
	AttemptsLeft int
	// ChainExhausted is true when the action was vetoed on the last
	// permitted attempt; BestAttempt is then the least dangerous attempt
	// in the chain, the latest on a tie.
	ChainExhausted bool
	BestAttempt    *Action
}

// Service classifies, stores and reviews proposed actions.
type Service struct {
	store       reviewStore
	reviewer    Reviewer
	maxAttempts int
}

// NewService builds a Service. A maxAttempts below one falls back to the
// configured default.
func NewService(store reviewStore, reviewer Reviewer, maxAttempts int) *Service {
	if maxAttempts < 1 {
		maxAttempts = config.DefaultActionsMaxAttempts
	}
	return &Service{store: store, reviewer: reviewer, maxAttempts: maxAttempts}
}

// classify splits sql into statements and refuses transaction control and
// batches with no writes. Each statement is judged on its own, under
// every reading of a plain string literal, so that one reading cannot
// hide a statement from the check.
func classify(sql string) ([]string, error) {
	statements := sqllex.SplitStatements(sql, sqllex.StandardStrings)
	if len(statements) == 0 {
		return nil, ErrEmptyProposal
	}
	allReadOnly := true
	for _, reading := range sqllex.StringReadings {
		for _, stmt := range sqllex.SplitStatements(sql, reading) {
			if sqllex.IsTransactionControl(stmt) {
				return nil, ErrTransactionControl
			}
			if sqllex.IsTimeoutOverride(stmt) {
				return nil, ErrTimeoutOverride
			}
			if !sqllex.IsReadOnly(stmt) {
				allReadOnly = false
			}
		}
	}
	if allReadOnly {
		return nil, ErrNoWrites
	}
	return statements, nil
}

// Propose classifies the SQL, stores it as a new attempt, has it reviewed
// and records the verdict. A reviewer error leaves the action pending and
// is returned, so nothing unreviewed can run; a later revision of the
// same parent replaces it (see Store.Create), so the chain can go on.
func (s *Service) Propose(ctx context.Context, req ProposeRequest) (*ProposeResult, error) {
	statements, err := classify(req.SQL)
	if err != nil {
		return nil, err
	}

	username := auth.GetUsernameFromContext(ctx)
	via := "session"
	if auth.IsAPITokenFromContext(ctx) {
		via = "token"
	}

	attempt, err := s.nextAttempt(ctx, req, username)
	if err != nil {
		return nil, err
	}

	created, err := s.store.Create(ctx, NewAction{
		ConnectionID:   req.ConnectionID,
		DatabaseName:   req.DatabaseName,
		Statements:     statements,
		Source:         req.Source,
		ProposedBy:     username,
		ProposedVia:    via,
		ConversationID: req.ConversationID,
		AlertID:        req.AlertID,
		ParentActionID: req.RevisesActionID,
		Attempt:        attempt,
		Rationale:      req.Rationale,
		Evidence:       req.Evidence,
	})
	if errors.Is(err, ErrAlreadyRevised) {
		return nil, ErrBadRevision
	}
	if err != nil {
		return nil, err
	}

	status, err := s.review(ctx, created)
	if err != nil {
		return nil, err
	}

	res := &ProposeResult{Action: created, AttemptsLeft: s.maxAttempts - attempt}
	if status == reviewStatusVetoed && res.AttemptsLeft == 0 {
		res.ChainExhausted = true
		res.BestAttempt = s.bestAttempt(ctx, created)
	}
	return res, nil
}

// nextAttempt returns the attempt number for a proposal: 1 for a new
// change, or one more than the action it revises. A revision must name a
// vetoed action on the same connection by the same proposer, and the
// chain must have attempts left.
func (s *Service) nextAttempt(ctx context.Context, req ProposeRequest, username string) (int, error) {
	attempt := 1
	if req.RevisesActionID != nil {
		parent, err := s.store.Get(ctx, *req.RevisesActionID)
		if errors.Is(err, ErrNotFound) {
			// Indistinguishable from another proposer's action, so ids
			// cannot be probed.
			return 0, ErrBadRevision
		}
		if err != nil {
			return 0, err
		}
		if parent.ReviewStatus != reviewStatusVetoed ||
			parent.ConnectionID != req.ConnectionID ||
			parent.ProposedBy != username {
			return 0, ErrBadRevision
		}
		attempt = parent.Attempt + 1
	}
	if attempt > s.maxAttempts {
		return 0, ErrChainExhausted
	}
	return attempt, nil
}

// review has a stored action reviewed, records the verdict and updates
// the action to match, returning its review status.
func (s *Service) review(ctx context.Context, created *Action) (string, error) {
	verdict, err := s.reviewer.Review(ctx, created)
	if err != nil {
		return "", fmt.Errorf("review action %d: %w", created.ID, err)
	}
	var status string
	switch verdict.Decision {
	case decisionPass:
		status = reviewStatusPassed
	case decisionVeto:
		status = reviewStatusVetoed
	default:
		return "", fmt.Errorf("review action %d: unknown decision %q", created.ID, verdict.Decision)
	}
	if err := s.store.SetReview(ctx, created.ID, status, verdict); err != nil {
		if errors.Is(err, ErrNotFound) && created.ParentActionID != nil {
			// A concurrent revision of the same parent replaced this
			// one whilst it was being reviewed.
			return "", ErrBadRevision
		}
		return "", err
	}
	created.ReviewStatus = status
	created.Review = &verdict
	return status, nil
}

// bestAttempt walks the chain back from last and returns the attempt with
// the lowest danger, the latest on a tie. If an ancestor cannot be read it
// logs the failure and settles for what it has seen so far.
func (s *Service) bestAttempt(ctx context.Context, last *Action) *Action {
	best := last
	cur := last
	for cur.ParentActionID != nil {
		p, err := s.store.Get(ctx, *cur.ParentActionID)
		if err != nil {
			log.Printf("[WARN] Best attempt for action %d: reading action %d failed: %v",
				last.ID, *cur.ParentActionID, err)
			break
		}
		if attemptRank(p) < attemptRank(best) {
			best = p
		}
		cur = p
	}
	return best
}

func attemptRank(a *Action) int {
	if a.Review == nil {
		return dangerRank("")
	}
	return dangerRank(a.Review.Danger)
}

// CheckOverride decides whether the caller may override a veto: only a
// signed-in session (not a token) may, only on an action whose review
// status is vetoed, and only with the connection's name typed exactly.
// The caller still checks write access on the connection.
func (s *Service) CheckOverride(ctx context.Context, a *Action, confirm, connectionName string) error {
	if auth.IsAPITokenFromContext(ctx) || a == nil || a.ReviewStatus != reviewStatusVetoed {
		return ErrOverrideNotAllowed
	}
	if confirm != connectionName || confirm == "" {
		return ErrConfirmMismatch
	}
	return nil
}
