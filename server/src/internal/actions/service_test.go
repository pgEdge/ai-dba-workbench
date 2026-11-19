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
	"errors"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// fakeReviewStore is an in-memory reviewStore for service tests.
type fakeReviewStore struct {
	actions      map[int64]*Action
	next         int64
	setReviewErr error
}

func newFakeReviewStore() *fakeReviewStore {
	return &fakeReviewStore{actions: map[int64]*Action{}}
}

func (f *fakeReviewStore) Create(_ context.Context, n NewAction) (*Action, error) {
	f.next++
	attempt := n.Attempt
	if attempt < 1 {
		attempt = 1
	}
	a := &Action{
		ID: f.next, ConnectionID: n.ConnectionID, DatabaseName: n.DatabaseName,
		Statements: n.Statements, Source: n.Source, ProposedBy: n.ProposedBy,
		ProposedVia: n.ProposedVia, ConversationID: n.ConversationID,
		AlertID: n.AlertID, ParentActionID: n.ParentActionID,
		Attempt: attempt, Evidence: n.Evidence, ReviewStatus: "pending",
		Approval: "none", ExecStatus: "not_started",
	}
	f.actions[a.ID] = a
	c := *a
	return &c, nil
}

func (f *fakeReviewStore) Get(_ context.Context, id int64) (*Action, error) {
	a, ok := f.actions[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *a
	return &c, nil
}

func (f *fakeReviewStore) SetReview(_ context.Context, id int64, status string, v Verdict) error {
	if f.setReviewErr != nil {
		return f.setReviewErr
	}
	a, ok := f.actions[id]
	if !ok {
		return ErrNotFound
	}
	a.ReviewStatus = status
	a.Review = &v
	return nil
}

// scriptedReviewer returns the queued verdicts in order, repeating the last.
type scriptedReviewer struct {
	verdicts []Verdict
	err      error
}

func (s *scriptedReviewer) Review(_ context.Context, _ *Action) (Verdict, error) {
	if s.err != nil {
		return Verdict{}, s.err
	}
	v := s.verdicts[0]
	if len(s.verdicts) > 1 {
		s.verdicts = s.verdicts[1:]
	}
	return v, nil
}

func sessionCtx(user string) context.Context {
	return context.WithValue(context.Background(), auth.UsernameContextKey, user)
}

func tokenCtx(user string) context.Context {
	return context.WithValue(sessionCtx(user), auth.IsAPITokenContextKey, true)
}

func veto(danger string) Verdict {
	return Verdict{Decision: "veto", Danger: danger, Reasons: []string{"no"}, Reviewer: "t"}
}

func pass() Verdict {
	return Verdict{Decision: "pass", Danger: "low", Reviewer: "t"}
}

func req(sql string) ProposeRequest {
	return ProposeRequest{ConnectionID: 1, SQL: sql, Rationale: "r", Source: "chat_tool"}
}

func TestStubReviewer(t *testing.T) {
	v, err := StubReviewer{}.Review(context.Background(), &Action{})
	if err != nil || v.Decision != "pass" || v.Danger != "low" || v.Reviewer != "none" {
		t.Fatalf("unexpected verdict %+v, %v", v, err)
	}
}

func TestProposeClassification(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want error
	}{
		{"read only", "SELECT 1; SHOW work_mem", ErrNoWrites},
		{"empty", "  ;  ", ErrEmptyProposal},
		{"transaction control", "BEGIN; UPDATE t SET a = 1", ErrTransactionControl},
		{"commit last", "UPDATE t SET a = 1; COMMIT", ErrTransactionControl},
		{"mixed ok", "SELECT 1; UPDATE t SET a = 1", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeReviewStore()
			svc := NewService(st, StubReviewer{}, 3)
			_, err := svc.Propose(sessionCtx("alice"), req(tc.sql))
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want != nil && len(st.actions) != 0 {
				t.Fatal("a refused proposal must not be stored")
			}
		})
	}
}

func TestProposePass(t *testing.T) {
	st := newFakeReviewStore()
	svc := NewService(st, StubReviewer{}, 3)
	res, err := svc.Propose(sessionCtx("alice"), req("CREATE INDEX i ON t (a); ANALYZE t"))
	if err != nil {
		t.Fatal(err)
	}
	a := res.Action
	if a.ReviewStatus != "passed" || a.Review == nil || a.Attempt != 1 ||
		a.ProposedBy != "alice" || a.ProposedVia != "session" ||
		len(a.Statements) != 2 || res.AttemptsLeft != 2 || res.ChainExhausted {
		t.Fatalf("unexpected result %+v / %+v", res, a)
	}
	if st.actions[a.ID].ReviewStatus != "passed" {
		t.Fatal("verdict not persisted")
	}
}

func TestProposeViaToken(t *testing.T) {
	svc := NewService(newFakeReviewStore(), StubReviewer{}, 3)
	res, err := svc.Propose(tokenCtx("alice"), req("DROP TABLE t"))
	if err != nil || res.Action.ProposedVia != "token" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestProposeVetoThenRevise(t *testing.T) {
	st := newFakeReviewStore()
	rv := &scriptedReviewer{verdicts: []Verdict{veto("high"), pass()}}
	svc := NewService(st, rv, 3)
	ctx := sessionCtx("alice")

	r1, err := svc.Propose(ctx, req("DROP TABLE t"))
	if err != nil {
		t.Fatal(err)
	}
	if r1.Action.ReviewStatus != "vetoed" || r1.AttemptsLeft != 2 || r1.ChainExhausted {
		t.Fatalf("unexpected %+v", r1)
	}
	rq := req("DELETE FROM t WHERE a = 1")
	parent := r1.Action.ID
	rq.RevisesActionID = &parent
	r2, err := svc.Propose(ctx, rq)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Action.Attempt != 2 || r2.Action.ParentActionID == nil ||
		*r2.Action.ParentActionID != parent || r2.Action.ReviewStatus != "passed" ||
		r2.AttemptsLeft != 1 {
		t.Fatalf("unexpected %+v / %+v", r2, r2.Action)
	}
}

// runChain proposes one attempt per danger level, each revising the last,
// and returns the results.
func runChain(t *testing.T, svc *Service, n int) []*ProposeResult {
	t.Helper()
	var out []*ProposeResult
	for i := 0; i < n; i++ {
		rq := req("DROP TABLE t")
		if i > 0 {
			id := out[i-1].Action.ID
			rq.RevisesActionID = &id
		}
		r, err := svc.Propose(sessionCtx("alice"), rq)
		if err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
		out = append(out, r)
	}
	return out
}

func TestProposeChainExhaustedAndBestAttempt(t *testing.T) {
	st := newFakeReviewStore()
	// Attempts 1 and 2 share the lowest danger; the later one wins.
	rv := &scriptedReviewer{verdicts: []Verdict{veto("moderate"), veto("moderate"), veto("critical")}}
	svc := NewService(st, rv, 3)

	res := runChain(t, svc, 3)
	last := res[2]
	if !last.ChainExhausted || last.AttemptsLeft != 0 || last.BestAttempt == nil ||
		last.BestAttempt.ID != res[1].Action.ID {
		t.Fatalf("unexpected %+v best=%+v", last, last.BestAttempt)
	}
	if res[0].ChainExhausted || res[0].BestAttempt != nil {
		t.Fatal("first attempt must not report exhaustion")
	}

	// A fourth attempt is refused.
	id := last.Action.ID
	rq := req("DROP TABLE t")
	rq.RevisesActionID = &id
	if _, err := svc.Propose(sessionCtx("alice"), rq); !errors.Is(err, ErrChainExhausted) {
		t.Fatalf("got %v want ErrChainExhausted", err)
	}
	if len(st.actions) != 3 {
		t.Fatal("exhausted proposal must not be stored")
	}
}

func TestBestAttemptPrefersLowerDanger(t *testing.T) {
	st := newFakeReviewStore()
	rv := &scriptedReviewer{verdicts: []Verdict{veto("low"), veto("high"), veto("bogus")}}
	svc := NewService(st, rv, 3)
	res := runChain(t, svc, 3)
	if res[2].BestAttempt.ID != res[0].Action.ID {
		t.Fatalf("best %d want %d", res[2].BestAttempt.ID, res[0].Action.ID)
	}
}

func TestProposeBadRevision(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(rq *ProposeRequest, parent, passed int64)
		ctx    context.Context
		want   error
	}{
		{"parent passed", func(rq *ProposeRequest, _, passed int64) { rq.RevisesActionID = &passed }, sessionCtx("alice"), ErrBadRevision},
		{"other connection", func(rq *ProposeRequest, parent, _ int64) { rq.RevisesActionID = &parent; rq.ConnectionID = 2 }, sessionCtx("alice"), ErrBadRevision},
		{"other proposer", func(rq *ProposeRequest, parent, _ int64) { rq.RevisesActionID = &parent }, sessionCtx("bob"), ErrBadRevision},
		{"missing parent", func(rq *ProposeRequest, _, _ int64) { id := int64(999); rq.RevisesActionID = &id }, sessionCtx("alice"), ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeReviewStore()
			rv := &scriptedReviewer{verdicts: []Verdict{veto("high"), pass()}}
			svc := NewService(st, rv, 3)
			r1, err := svc.Propose(sessionCtx("alice"), req("DROP TABLE t"))
			if err != nil {
				t.Fatal(err)
			}
			r2, err := svc.Propose(sessionCtx("alice"), req("DROP TABLE u"))
			if err != nil {
				t.Fatal(err)
			}
			n := len(st.actions)
			rq := req("DELETE FROM t")
			tc.mutate(&rq, r1.Action.ID, r2.Action.ID)
			if _, err := svc.Propose(tc.ctx, rq); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if len(st.actions) != n {
				t.Fatal("bad revision must not be stored")
			}
		})
	}
}

func TestProposeReviewerErrorFailsClosed(t *testing.T) {
	st := newFakeReviewStore()
	boom := errors.New("llm down")
	svc := NewService(st, &scriptedReviewer{err: boom}, 3)
	res, err := svc.Propose(sessionCtx("alice"), req("DROP TABLE t"))
	if !errors.Is(err, boom) || res != nil {
		t.Fatalf("got %+v, %v", res, err)
	}
	if len(st.actions) != 1 || st.actions[1].ReviewStatus != "pending" {
		t.Fatalf("action must stay pending: %+v", st.actions)
	}
}

func TestProposeBadDecisionFailsClosed(t *testing.T) {
	st := newFakeReviewStore()
	svc := NewService(st, &scriptedReviewer{verdicts: []Verdict{{Decision: "maybe"}}}, 3)
	if _, err := svc.Propose(sessionCtx("alice"), req("DROP TABLE t")); err == nil {
		t.Fatal("expected error")
	}
	if st.actions[1].ReviewStatus != "pending" {
		t.Fatal("action must stay pending")
	}
}

func TestProposeSetReviewError(t *testing.T) {
	st := newFakeReviewStore()
	st.setReviewErr = errors.New("db down")
	svc := NewService(st, StubReviewer{}, 3)
	if _, err := svc.Propose(sessionCtx("alice"), req("DROP TABLE t")); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewServiceDefaultsMaxAttempts(t *testing.T) {
	svc := NewService(newFakeReviewStore(), StubReviewer{}, 0)
	res, err := svc.Propose(sessionCtx("alice"), req("DROP TABLE t"))
	if err != nil || res.AttemptsLeft != 2 {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestCheckOverride(t *testing.T) {
	svc := NewService(newFakeReviewStore(), StubReviewer{}, 3)
	tests := []struct {
		name    string
		ctx     context.Context
		status  string
		confirm string
		want    error
	}{
		{"vetoed session ok", sessionCtx("alice"), "vetoed", "prod", nil},
		{"token refused", tokenCtx("alice"), "vetoed", "prod", ErrOverrideNotAllowed},
		{"passed refused", sessionCtx("alice"), "passed", "prod", ErrOverrideNotAllowed},
		{"pending refused", sessionCtx("alice"), "pending", "prod", ErrOverrideNotAllowed},
		{"wrong name", sessionCtx("alice"), "vetoed", "Prod", ErrConfirmMismatch},
		{"empty name", sessionCtx("alice"), "vetoed", "", ErrConfirmMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.CheckOverride(tc.ctx, &Action{ReviewStatus: tc.status}, tc.confirm, "prod")
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	if err := svc.CheckOverride(sessionCtx("a"), nil, "prod", "prod"); !errors.Is(err, ErrOverrideNotAllowed) {
		t.Fatalf("nil action: %v", err)
	}
}
