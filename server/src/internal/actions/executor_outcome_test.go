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
	"time"
)

// flakyOutcomes wraps a Store, failing the first fail calls to
// FinishExecution. With landFirst set, the first call is written before
// it reports its error, as when a connection drops after the commit.
type flakyOutcomes struct {
	*Store
	mu        sync.Mutex
	fail      int
	landFirst bool
	calls     int
	onCall    func(int)
}

func (f *flakyOutcomes) FinishExecution(ctx context.Context, id int64, status string,
	transactional bool, results []StatementResult) error {

	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()
	if f.onCall != nil {
		f.onCall(call)
	}
	if call == 1 && f.landFirst {
		if err := f.Store.FinishExecution(ctx, id, status, transactional, results); err != nil {
			return err
		}
		return errors.New("connection reset")
	}
	if call <= f.fail {
		return errors.New("connection reset")
	}
	return f.Store.FinishExecution(ctx, id, status, transactional, results)
}

// claimedAction creates an action and claims it, so its outcome can be
// recorded.
func claimedAction(t *testing.T, store *Store, connID int) int64 {
	t.Helper()
	ctx := context.Background()
	a, err := store.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetReview(ctx, a.ID, "passed", Verdict{Decision: "pass", Danger: "low", Reviewer: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Approve(ctx, a.ID, "approved", "tester", "session"); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.claimNew(ctx, a.ID); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	return a.ID
}

// TestRecordOutcomeRetries covers the retries of a failed outcome write:
// a transient failure is retried, a write that landed but reported an
// error is taken as written, a persistent failure gives up after
// finishAttempts, and Shutdown lets the retries go on only whilst its
// context lasts: within the budget a transient failure is still
// recorded, and a spent budget abandons the retries at once.
func TestRecordOutcomeRetries(t *testing.T) {
	t.Parallel()
	store, _, connID := newTestStore(t, "_x_rec")
	st := &runState{transactional: true, results: []StatementResult{{Statement: "SELECT 1", Applied: true}}}

	spent, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name       string
		flaky      *flakyOutcomes
		shutdown   context.Context // Shutdown is called with this on the first write, when set
		wantCalls  int
		wantStatus string
	}{
		{"transient", &flakyOutcomes{fail: 2}, nil, 3, "succeeded"},
		{"landed", &flakyOutcomes{landFirst: true}, nil, 2, "succeeded"},
		{"persistent", &flakyOutcomes{fail: 100}, nil, finishAttempts, "executing"},
		{"shutdown within the budget", &flakyOutcomes{fail: 2}, context.Background(), 3, "succeeded"},
		{"shutdown budget spent", &flakyOutcomes{fail: 100}, spent, 1, "executing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := claimedAction(t, store, connID)
			e := NewExecutor(store, nil, time.Second, time.Minute, 1)
			e.finishBackoff = time.Millisecond
			tc.flaky.Store = store
			e.outcomes = tc.flaky
			if tc.shutdown != nil {
				tc.flaky.onCall = func(call int) {
					if call == 1 {
						_ = e.Shutdown(tc.shutdown)
					}
				}
			}
			e.recordOutcome(context.Background(), id, "succeeded", st)
			if tc.flaky.calls != tc.wantCalls {
				t.Errorf("calls = %d, want %d", tc.flaky.calls, tc.wantCalls)
			}
			a, err := store.Get(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if a.ExecStatus != tc.wantStatus {
				t.Errorf("exec_status = %q, want %q", a.ExecStatus, tc.wantStatus)
			}
		})
	}
}

// TestRecordOutcomeInvalidStateMismatch proves ErrInvalidState on a
// retry is only taken as success when the stored outcome matches: an
// action that some other path finished differently is reported as a
// failure, and not retried further.
func TestRecordOutcomeInvalidStateMismatch(t *testing.T) {
	t.Parallel()
	store, _, connID := newTestStore(t, "_x_rec2")
	id := claimedAction(t, store, connID)
	if err := store.FinishExecution(context.Background(), id, "unknown", false, nil); err != nil {
		t.Fatal(err)
	}
	flaky := &flakyOutcomes{Store: store}
	e := NewExecutor(store, nil, time.Second, time.Minute, 1)
	e.outcomes = flaky
	if err := e.finishOnce(context.Background(), id, "succeeded", &runState{transactional: true}, true); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("finishOnce = %v, want ErrInvalidState", err)
	}
	flaky.calls = 0
	e.recordOutcome(context.Background(), id, "succeeded", &runState{transactional: true})
	if flaky.calls != 1 {
		t.Errorf("calls = %d, want 1", flaky.calls)
	}
}
