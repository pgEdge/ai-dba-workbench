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
}

func (u *unsettledClaims) claim(ctx context.Context, id int64, token string) (bool, error) {
	if u.land {
		if u.landAs != "" {
			token = u.landAs
		}
		if ok, err := u.Store.claim(ctx, id, token); err != nil || !ok {
			return false, fmt.Errorf("landing claim: ok=%v err=%w", ok, err)
		}
	}
	return false, fmt.Errorf("claim action: connection reset; %w: connection refused", errClaimUnsettled)
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

// TestStartSettlesUnsettledClaim covers a claim whose reply and
// read-back are both lost (#888). Start reports the failure and starts
// no run, and the claim is settled in the background: one that
// committed under Start's token is returned to not_started, still
// approved and claimable again, even when the first attempts fail; a
// row not held under that token is left as it is; and the attempts
// stop after settleAttempts, leaving the row for the startup sweep.
func TestStartSettlesUnsettledClaim(t *testing.T) {
	t.Parallel()
	store, pool, connID := newTestStore(t, "_x_settle")
	theirs := "6f1c2a52-6a9a-4c39-9a53-7d0f2d1f0a02"
	tests := []struct {
		name       string
		claims     *unsettledClaims
		wantCalls  int
		wantStatus string
		wantToken  string // "" for none; "theirs" for the other claim's
	}{
		{"committed, settled on a later attempt", &unsettledClaims{land: true, failRelease: 2}, 3, "not_started", ""},
		{"never committed", &unsettledClaims{}, 1, "not_started", ""},
		{"held under another token", &unsettledClaims{land: true, landAs: theirs}, 1, "executing", theirs},
		{"attempts exhausted", &unsettledClaims{land: true, failRelease: 100}, settleAttempts, "executing", "ours"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := approvedAction(t, store, connID)
			e := NewExecutor(store, refuseOpener(t), time.Second, time.Minute, 1)
			e.settleBackoff = time.Millisecond
			tc.claims.Store = store
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
				if token == "" || token == theirs {
					t.Errorf("claim_token = %q, want Start's own", token)
				}
			default:
				if token != tc.wantToken {
					t.Errorf("claim_token = %q, want %q", token, tc.wantToken)
				}
			}
			if tc.wantStatus == "not_started" {
				if ok, err := store.ClaimForExecution(context.Background(), id); err != nil || !ok {
					t.Errorf("claim after settling = %v, %v; want it claimable", ok, err)
				}
			}
		})
	}
}

// TestShutdownStopsSettlingClaim proves Shutdown ends the settling of a
// claim at once, both whilst it waits to retry and whilst an attempt is
// in flight, so it never holds Shutdown up, and the row is left for the
// startup sweep.
func TestShutdownStopsSettlingClaim(t *testing.T) {
	t.Parallel()
	store, _, connID := newTestStore(t, "_x_settle2")
	tests := []struct {
		name string
		// inFlight blocks the second attempt until its context ends;
		// otherwise every attempt fails and the next waits an hour.
		inFlight bool
	}{
		{"waiting to retry", false},
		{"attempt in flight", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := approvedAction(t, store, connID)
			e := NewExecutor(store, refuseOpener(t), time.Second, time.Minute, 1)
			e.settleBackoff = time.Hour
			reached := make(chan struct{})
			claims := &unsettledClaims{Store: store, land: true, failRelease: 100}
			if tc.inFlight {
				e.settleBackoff = time.Millisecond
				claims.onRelease = func(ctx context.Context, call int) {
					if call == 2 {
						close(reached)
						<-ctx.Done()
					}
				}
			} else {
				claims.onRelease = func(_ context.Context, call int) {
					if call == 1 {
						close(reached)
					}
				}
			}
			e.claims = claims

			if err := e.Start(context.Background(), id); !errors.Is(err, errClaimUnsettled) {
				t.Fatalf("Start = %v, want errClaimUnsettled", err)
			}
			<-reached
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := e.Shutdown(ctx); err != nil {
				t.Fatalf("Shutdown = %v, want the settling stopped", err)
			}
			want := 1
			if tc.inFlight {
				want = 2
			}
			if got := claims.releaseCalls(); got != want {
				t.Errorf("release attempts = %d, want %d", got, want)
			}
			a, err := store.Get(context.Background(), id)
			if err != nil || a.ExecStatus != "executing" {
				t.Errorf("action = %+v (%v), want it left executing", a, err)
			}
		})
	}
}
