/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestMain lowers the bcrypt cost of every AuthStore this test binary
// opens to bcrypt.MinCost. Under the race detector a single hash at the
// production cost takes about five seconds, so tests that create users
// without calling SetBcryptCostForTesting would otherwise dominate the
// package runtime. Tests that assert on the production cost escape it
// for their own scope: TestNewAuthStoreUsesProductionBcryptCost resets
// testBcryptCost, and TestNewAuthStoreComputesTimingHashInServer
// overrides inTestBinary to take the server's path. Tests that assert on
// cost errors set store.bcryptCost directly after construction, which
// this does not affect.
func TestMain(m *testing.M) {
	testBcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

// hashCost returns the cost a bcrypt hash was generated at.
func hashCost(t *testing.T, hash []byte) int {
	t.Helper()
	cost, err := bcrypt.Cost(hash)
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	return cost
}

// TestNewAuthStoreLowersCostUnderTestMain pins the effect of TestMain:
// a fresh store hashes, and compares unknown users, at the lowered
// cost, so the timing equalizer stays matched to real comparisons.
func TestNewAuthStoreLowersCostUnderTestMain(t *testing.T) {
	store, err := NewAuthStore(t.TempDir(), 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer store.Close()

	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.bcryptCost != bcrypt.MinCost {
		t.Errorf("bcryptCost = %d, want %d", store.bcryptCost, bcrypt.MinCost)
	}
	if got := hashCost(t, store.timingDummyHash()); got != bcrypt.MinCost {
		t.Errorf("timing dummy hash cost = %d, want %d", got, bcrypt.MinCost)
	}
}

// TestNewAuthStoreComputesTimingHashInServer checks what newAuthStore
// does outside a test binary: it computes the shared cost-12
// timing-equalizer hash while the store opens, so the first failed
// login for an unknown user is no slower than later ones. The hash is
// cached per cost, so this test and the next share one production-cost
// hash: whichever runs first generates it. It mutates inTestBinary, so
// it must not run in parallel with tests that open a store.
func TestNewAuthStoreComputesTimingHashInServer(t *testing.T) {
	saved := inTestBinary
	inTestBinary = func() bool { return false }
	defer func() { inTestBinary = saved }()

	store, err := NewAuthStore(t.TempDir(), 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer store.Close()

	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.bcryptCost != DefaultBcryptCost {
		t.Errorf("bcryptCost = %d, want %d", store.bcryptCost, DefaultBcryptCost)
	}
	if store.dummyHash == nil {
		t.Fatal("a server store must compute the timing-equalizer hash at start-up")
	}
	if got := hashCost(t, store.dummyHash); got != DefaultBcryptCost {
		t.Errorf("timing dummy hash cost = %d, want %d", got, DefaultBcryptCost)
	}
}

// TestNewAuthStoreUsesProductionBcryptCost checks that, without the
// test-only lowering, a store hashes at DefaultBcryptCost and its
// timing equalizer compares against a hash of that same cost. With the
// previous test it shares the package's one production-cost hash
// generation, deliberately, and pays one production-cost compare. It
// mutates testBcryptCost, so it must not run in parallel with tests
// that open a store.
func TestNewAuthStoreUsesProductionBcryptCost(t *testing.T) {
	saved := testBcryptCost
	testBcryptCost = 0
	defer func() { testBcryptCost = saved }()

	store, err := NewAuthStore(t.TempDir(), 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer store.Close()

	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.bcryptCost != DefaultBcryptCost {
		t.Errorf("bcryptCost = %d, want %d", store.bcryptCost, DefaultBcryptCost)
	}
	if store.dummyHash != nil {
		t.Error("a test binary should leave dummyHash unset until first use")
	}
	hash := store.timingDummyHash()
	if got := hashCost(t, hash); got != DefaultBcryptCost {
		t.Errorf("timing dummy hash cost = %d, want %d", got, DefaultBcryptCost)
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(timingEqualizerInput)); err != nil {
		t.Errorf("timing dummy hash does not match its input: %v", err)
	}
}
