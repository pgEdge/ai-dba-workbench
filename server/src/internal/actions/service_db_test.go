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
	if _, err := s.Create(ctx, child); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, child); !errors.Is(err, ErrAlreadyRevised) {
		t.Fatalf("got %v want ErrAlreadyRevised", err)
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
