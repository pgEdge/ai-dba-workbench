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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// unsettledClaims wraps a Store so that every claim reports
// errClaimUnsettled, as when both the claim's reply and its read-back
// are lost. With land set, the claim is written first under Start's
// token, or under landAs when that is set; releaseClaim fails its
// first failRelease calls.
type unsettledClaims struct {
	*Store
	land        bool
	landAs      string
	failRelease int
	// onRelease, when set, runs at the start of each releaseClaim call
	// with its context and number, before any failure is decided.
	onRelease func(ctx context.Context, call int)

	mu    sync.Mutex
	calls int
	token string // Start's token, once claimed
}

func (u *unsettledClaims) claim(ctx context.Context, id int64, token string) (bool, error) {
	u.mu.Lock()
	u.token = token
	u.mu.Unlock()
	if u.land {
		if u.landAs != "" {
			token = u.landAs
		}
		if ok, err := u.Store.claim(ctx, id, token); err != nil || !ok {
			return false, fmt.Errorf("landing claim: ok=%v err=%w", ok, err)
		}
	}
	return false, fmt.Errorf("claim action: %w", errClaimUnsettled)
}

// startToken returns the token Start claimed under.
func (u *unsettledClaims) startToken() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.token
}

func (u *unsettledClaims) releaseClaim(ctx context.Context, id int64, token string) (bool, error) {
	u.mu.Lock()
	u.calls++
	call := u.calls
	u.mu.Unlock()
	if u.onRelease != nil {
		u.onRelease(ctx, call)
	}
	if call <= u.failRelease {
		return false, errors.New("connection refused")
	}
	return u.Store.releaseClaim(ctx, id, token)
}

func (u *unsettledClaims) releaseCalls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

// approvedAction creates an action that has passed review and been
// approved, ready to claim.
func approvedAction(t *testing.T, store *Store, connID int) int64 {
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
	return a.ID
}

// claimToken reads the action's claim token, or "" when it has none.
func claimToken(t *testing.T, pool *pgxpool.Pool, id int64) string {
	t.Helper()
	var token *string
	if err := pool.QueryRow(context.Background(),
		`SELECT claim_token::text FROM proposed_actions WHERE id = $1`, id).Scan(&token); err != nil {
		t.Fatal(err)
	}
	if token == nil {
		return ""
	}
	return *token
}

// refuseOpener fails the test if a run is started.
func refuseOpener(t *testing.T) Opener {
	return func(context.Context, int, *string) (*pgxpool.Pool, error) {
		t.Error("a run was started for an unsettled claim")
		return nil, errors.New("no run expected")
	}
}

// TestStartSettlesUnsettledClaim covers a claim whose outcome is lost
// (#888). Start reports the failure and starts no run, and the claim is
// settled in the background: one that committed under Start's token is
// returned to not_started, still approved and claimable again, even
// when the first attempts fail or when its COMMIT lands only after an
// attempt found nothing; a row not held under that token is left as it
// is; and failed attempts stop after settleAttempts, leaving the row
// for the startup sweep.
func TestStartSettlesUnsettledClaim(t *testing.T) {
	t.Parallel()
	store, pool, connID := newTestStore(t, "_x_settle")
	theirs := "6f1c2a52-6a9a-4c39-9a53-7d0f2d1f0a02"
	// commitsLate lands Start's claim just before the second attempt, as
	// a COMMIT that reaches the server after the first attempt does.
	commitsLate := func(u *unsettledClaims, id int64) func(context.Context, int) {
		return func(ctx context.Context, call int) {
			if call != 2 {
				return
			}
			if ok, err := u.Store.claim(ctx, id, u.startToken()); err != nil || !ok {
				t.Errorf("late claim = %v, %v", ok, err)
			}
		}
	}
	tests := []struct {
		name         string
		claims       *unsettledClaims
		late         bool          // land the claim with commitsLate
		settledAfter time.Duration // the executor's claimSettledAfter
		wantCalls    int
		wantStatus   string
		wantToken    string // "" for none, "ours" for Start's, else that token
	}{
		{"committed, settled on a later attempt", &unsettledClaims{land: true, failRelease: 2}, false, 0, 3, "not_started", ""},
		{"committed after the first attempt", &unsettledClaims{}, true, time.Hour, 2, "not_started", ""},
		{"never committed", &unsettledClaims{}, false, 0, 1, "not_started", ""},
		{"held under another token", &unsettledClaims{land: true, landAs: theirs}, false, 0, 1, "executing", theirs},
		{"attempts exhausted", &unsettledClaims{land: true, failRelease: 100}, false, 0, settleAttempts, "executing", "ours"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := approvedAction(t, store, connID)
			e := NewExecutor(store, refuseOpener(t), time.Second, time.Minute, 1)
			e.settleBackoff = time.Millisecond
			e.claimSettledAfter = tc.settledAfter
			tc.claims.Store = store
			if tc.late {
				tc.claims.onRelease = commitsLate(tc.claims, id)
			}
			e.claims = tc.claims

			err := e.Start(context.Background(), id)
			if !errors.Is(err, errClaimUnsettled) {
				t.Fatalf("Start = %v, want errClaimUnsettled", err)
			}
			e.wg.Wait() // the settling goroutine holds Start's slot
			if got := tc.claims.releaseCalls(); got != tc.wantCalls {
				t.Errorf("release attempts = %d, want %d", got, tc.wantCalls)
			}
			a, err := store.Get(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if a.ExecStatus != tc.wantStatus || a.Approval != "approved" {
				t.Errorf("exec_status = %q, approval = %q; want %q, approved", a.ExecStatus, a.Approval, tc.wantStatus)
			}
			token := claimToken(t, pool, id)
			switch tc.wantToken {
			case "":
				if token != "" || a.ExecutedAt != nil {
					t.Errorf("claim_token = %q, executed_at = %v; want neither", token, a.ExecutedAt)
				}
			case "ours":
				if token != tc.claims.startToken() {
					t.Errorf("claim_token = %q, want Start's own", token)
				}
			default:
				if token != tc.wantToken {
					t.Errorf("claim_token = %q, want %q", token, tc.wantToken)
				}
			}
			if len(e.pending) != 0 {
				t.Errorf("pending = %v after settling, want empty", e.pending)
			}
			if tc.wantStatus == "not_started" {
				if ok, err := store.claimNew(context.Background(), id); err != nil || !ok {
					t.Errorf("claim after settling = %v, %v; want it claimable", ok, err)
				}
			}
		})
	}
}

// TestStartRefusesWhilstClaimPending proves Start refuses an action
// whose claim is still being settled, without claiming it again, and
// refuses every action once maxPendingClaims are pending; it accepts
// the action again once the settling is done.
func TestStartRefusesWhilstClaimPending(t *testing.T) {
	t.Parallel()
	store, _, connID := newTestStore(t, "_x_settle3")
	id := approvedAction(t, store, connID)
	e := NewExecutor(store, refuseOpener(t), time.Second, time.Minute, 1)
	e.settleBackoff = time.Millisecond
	e.claimSettledAfter = 0
	reached, release := make(chan struct{}), make(chan struct{})
	claims := &unsettledClaims{Store: store, land: true,
		onRelease: func(_ context.Context, call int) {
			if call == 1 {
				close(reached)
				<-release
			}
		}}
	e.claims = claims
	if err := e.Start(context.Background(), id); !errors.Is(err, errClaimUnsettled) {
		t.Fatalf("Start = %v, want errClaimUnsettled", err)
	}
	<-reached
	first := claims.startToken()
	if err := e.Start(context.Background(), id); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("Start whilst settling = %v, want ErrAlreadyStarted", err)
	}
	if claims.startToken() != first {
		t.Error("Start whilst settling claimed the action again")
	}

	e.mu.Lock()
	for i := range int64(maxPendingClaims - 1) {
		e.pending[-1-i] = struct{}{}
	}
	e.mu.Unlock()
	if err := e.Start(context.Background(), id+1); !errors.Is(err, errTooManyPending) {
		t.Errorf("Start at the cap = %v, want errTooManyPending", err)
	}
	e.mu.Lock()
	for i := range int64(maxPendingClaims - 1) {
		delete(e.pending, -1-i)
	}
	e.mu.Unlock()

	close(release)
	e.wg.Wait()
	a, err := store.Get(context.Background(), id)
	if err != nil || a.ExecStatus != "not_started" {
		t.Fatalf("action = %+v (%v), want not_started", a, err)
	}
	e.opener = func(context.Context, int, *string) (*pgxpool.Pool, error) {
		return nil, errors.New("no target in this test")
	}
	e.claims = store
	if err := e.Start(context.Background(), id); err != nil {
		t.Errorf("Start after settling = %v, want it started", err)
	}
	e.wg.Wait()
}

// TestSettleClaimShutdown proves settling follows outcome writes at
// Shutdown: retries go on whilst Shutdown's context lasts, so a pending
// release still lands within a graceful shutdown, and a spent budget
// abandons them at once, leaving the row for the startup sweep.
func TestSettleClaimShutdown(t *testing.T) {
	t.Parallel()
	store, _, connID := newTestStore(t, "_x_settle2")
	spent, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name       string
		shutdown   context.Context
		fail       int
		wantCalls  int
		wantStatus string
	}{
		{"within the budget", context.Background(), 2, 3, "not_started"},
		{"budget spent", spent, 100, 1, "executing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := approvedAction(t, store, connID)
			token := uuid.NewString()
			if ok, err := store.claim(context.Background(), id, token); err != nil || !ok {
				t.Fatalf("claim = %v, %v", ok, err)
			}
			e := NewExecutor(store, nil, time.Second, time.Minute, 1)
			e.settleBackoff = time.Millisecond
			claims := &unsettledClaims{Store: store, failRelease: tc.fail}
			claims.onRelease = func(_ context.Context, call int) {
				if call == 1 {
					_ = e.Shutdown(tc.shutdown)
				}
			}
			e.claims = claims
			e.settleClaim(id, token, time.Now())
			if got := claims.releaseCalls(); got != tc.wantCalls {
				t.Errorf("release attempts = %d, want %d", got, tc.wantCalls)
			}
			a, err := store.Get(context.Background(), id)
			if err != nil || a.ExecStatus != tc.wantStatus {
				t.Errorf("action = %+v (%v), want %s", a, err, tc.wantStatus)
			}
		})
	}
}
