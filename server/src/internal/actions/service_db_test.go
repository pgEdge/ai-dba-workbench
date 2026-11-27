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
	"sync"
	"testing"
)

// TestStoreCreateSecondRevisionRefused proves a parent can be revised
// only once, except that a revision still waiting for its review, whose
// review failed, is replaced by the proposer's next revision; another
// proposer cannot replace it.
func TestStoreCreateSecondRevisionRefused(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_rev")
	ctx := context.Background()
	parent, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	child := sampleAction(connID, "alice")
	child.ParentActionID = &parent.ID
	child.Attempt = 2
	unreviewed, err := s.Create(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	other := child
	other.ProposedBy = "bob"
	if _, err := s.Create(ctx, other); !errors.Is(err, ErrAlreadyRevised) {
		t.Fatalf("another proposer: got %v want ErrAlreadyRevised", err)
	}
	replacement, err := s.Create(ctx, child)
	if err != nil {
		t.Fatalf("replacing an unreviewed revision: %v", err)
	}
	if _, err := s.Get(ctx, unreviewed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the unreviewed revision survived: %v", err)
	}
	if err := s.SetReview(ctx, replacement.ID, "vetoed", veto("high")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, child); !errors.Is(err, ErrAlreadyRevised) {
		t.Fatalf("got %v want ErrAlreadyRevised", err)
	}
}

// TestProposeRetryAfterFailedReview proves a revision whose review
// failed does not block the chain: the proposer's retry replaces it and
// is reviewed.
func TestProposeRetryAfterFailedReview(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_rvf")
	rv := &scriptedReviewer{verdicts: []Verdict{veto("high")}}
	svc := NewService(s, rv, 3)
	ctx := sessionCtx("alice")

	first := req("DROP TABLE t")
	first.ConnectionID = connID
	r1, err := svc.Propose(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	rq := req("DELETE FROM t")
	rq.ConnectionID = connID
	rq.RevisesActionID = &r1.Action.ID
	rv.err = errors.New("reviewer unavailable")
	if _, err := svc.Propose(ctx, rq); err == nil {
		t.Fatal("expected the review to fail")
	}
	rv.err = nil
	r2, err := svc.Propose(ctx, rq)
	if err != nil {
		t.Fatalf("retry after a failed review: %v", err)
	}
	if r2.Action.ReviewStatus != reviewStatusVetoed || r2.Action.Attempt != 2 {
		t.Errorf("retry = %+v", r2.Action)
	}
}

// TestReviewReplacedRevision proves a review whose revision was replaced
// whilst it ran reports the revision refused rather than not found.
func TestReviewReplacedRevision(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_rvr")
	svc := NewService(s, &scriptedReviewer{verdicts: []Verdict{pass()}}, 3)
	gone := &Action{ID: 987654, ConnectionID: connID, ParentActionID: new(int64)}
	if _, err := svc.review(context.Background(), gone); !errors.Is(err, ErrBadRevision) {
		t.Fatalf("got %v want ErrBadRevision", err)
	}
	gone.ParentActionID = nil
	if _, err := svc.review(context.Background(), gone); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v want ErrNotFound", err)
	}
}

func TestProposeConcurrentRevisionsOneWins(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_race")
	rv := &scriptedReviewer{verdicts: []Verdict{veto("high")}}
	svc := NewService(s, rv, 3)
	ctx := sessionCtx("alice")

	first := req("DROP TABLE t")
	first.ConnectionID = connID
	r1, err := svc.Propose(ctx, first)
	if err != nil {
		t.Fatal(err)
	}

	const racers = 8
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rq := req("DELETE FROM t")
			rq.ConnectionID = connID
			id := r1.Action.ID
			rq.RevisesActionID = &id
			<-start
			_, errs[i] = svc.Propose(ctx, rq)
		}(i)
	}
	close(start)
	wg.Wait()

	ok := 0
	for _, e := range errs {
		switch {
		case e == nil:
			ok++
		case !errors.Is(e, ErrBadRevision):
			t.Fatalf("unexpected error %v", e)
		}
	}
	if ok != 1 {
		t.Fatalf("%d revisions succeeded, want exactly 1", ok)
	}
}
