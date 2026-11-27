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

// TestApproveIsOneShot races several approvals of one action: exactly one
// may succeed, the rest are refused as invalid state, and the approver on
// record is the winner. A second, sequential approval (including an
// override of an approved action) is refused too.
func TestApproveIsOneShot(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_oneshot")
	ctx := context.Background()

	a, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const racers = 8
	users := make([]string, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		users[i] = "user" + string(rune('a'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Approve(ctx, a.ID, "approved", users[i], "session")
		}()
	}
	wg.Wait()

	winner := ""
	for i, err := range errs {
		switch {
		case err == nil:
			if winner != "" {
				t.Fatalf("more than one approval succeeded (%s and %s)", winner, users[i])
			}
			winner = users[i]
		case !errors.Is(err, ErrInvalidState):
			t.Errorf("loser %s got %v, want ErrInvalidState", users[i], err)
		}
	}
	if winner == "" {
		t.Fatal("no approval succeeded")
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ApprovedBy == nil || *got.ApprovedBy != winner {
		t.Errorf("approved_by = %v, want %s", got.ApprovedBy, winner)
	}

	if err := s.Approve(ctx, a.ID, "overridden", "mallory", "session"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("second approval: %v, want ErrInvalidState", err)
	}
}

// TestOverrideAfterVeto confirms the one-shot guard leaves the override
// flow intact: a vetoed action has no approval yet, so it can be
// overridden once and then claimed.
func TestOverrideAfterVeto(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_override")
	ctx := context.Background()

	a, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.SetReview(ctx, a.ID, "vetoed", Verdict{Decision: "veto", Danger: "high"}); err != nil {
		t.Fatalf("veto: %v", err)
	}
	if err := s.Approve(ctx, a.ID, "overridden", "bob", "session"); err != nil {
		t.Fatalf("override: %v", err)
	}
	if ok, err := s.claimNew(ctx, a.ID); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
}
