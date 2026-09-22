/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// lockoutConfig builds the minimum configuration the two start-up
// helpers under test read, with the lockout threshold set to the raw
// configured value: nil stands for a configuration file that omits
// http.auth.max_failed_attempts_before_lockout.
func lockoutConfig(raw *int) *config.Config {
	cfg := &config.Config{}
	cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockoutPtr = raw
	cfg.HTTP.Auth.RateLimitWindowMinutes = 15
	cfg.HTTP.Auth.RateLimitMaxAttempts = 10
	return cfg
}

func lockoutThreshold(n int) *int { return &n }

// TestInitAuthStoreAppliesEffectiveLockoutThreshold pins the value that
// actually reaches NewAuthStore, which is the only place the threshold
// is ever read: the store keeps its own copy for the life of the
// process. Asserting on the effective count rather than on the
// configuration field is deliberate, because the bug this covers was a
// raw value being passed straight through, and reading
// MaxFailedAttemptsBeforeLockoutPtr directly would still compile.
func TestInitAuthStoreAppliesEffectiveLockoutThreshold(t *testing.T) {
	tests := map[string]struct {
		raw *int
		// lockAfter is the number of consecutive failed passwords that
		// must disable the account, or 0 when lockout is switched off
		// and the account must survive every attempt.
		lockAfter int
	}{
		"omitted key locks out at the default of ten": {raw: nil, lockAfter: 10},
		"negative value falls back to the default":    {raw: lockoutThreshold(-1), lockAfter: 10},
		"explicit zero disables lockout":              {raw: lockoutThreshold(0), lockAfter: 0},
		"explicit value is applied as given":          {raw: lockoutThreshold(3), lockAfter: 3},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{cfg: lockoutConfig(tc.raw), dataDir: t.TempDir()}

			if out := captureStderr(t, func() {
				if err := server.initAuthStore(); err != nil {
					t.Fatalf("initAuthStore: %v", err)
				}
			}); out == "" {
				t.Error("initAuthStore reported nothing about the auth store")
			}
			defer func() {
				if err := server.authStore.Close(); err != nil {
					t.Errorf("closing auth store: %v", err)
				}
			}()

			// The default cost makes each of the failed password checks
			// below take a quarter of a second; the stored hash is
			// created after this call, so both sides are cheap.
			server.authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

			const username = "lockout-probe"
			if err := server.authStore.CreateUser(
				username, "correct-horse-battery-staple", "", "", ""); err != nil {
				t.Fatalf("CreateUser: %v", err)
			}

			attempts := tc.lockAfter
			if attempts == 0 {
				// Lockout is meant to be off, so push well past the
				// default threshold to prove nothing locks.
				attempts = 12
			}
			for i := 1; i <= attempts; i++ {
				if _, _, err := server.authStore.AuthenticateUser(
					username, fmt.Sprintf("wrong-%d", i)); err == nil {
					t.Fatalf("attempt %d with the wrong password succeeded", i)
				}

				user, err := server.authStore.GetUser(username)
				if err != nil {
					t.Fatalf("GetUser after attempt %d: %v", i, err)
				}
				wantEnabled := tc.lockAfter == 0 || i < tc.lockAfter
				if user.Enabled != wantEnabled {
					t.Fatalf("after %d failed attempts enabled = %v, want %v",
						i, user.Enabled, wantEnabled)
				}
			}
		})
	}
}

// TestInitAuthStoreReportsExistingStore covers the branch taken on
// every start after the first, where the auth store already exists and
// its user and token counts are reported instead of the creation
// notice, together with the unusable data directory that is the one
// failure this helper can hit before the store is opened.
func TestInitAuthStoreReportsExistingStore(t *testing.T) {
	dataDir := t.TempDir()
	first := &Server{cfg: lockoutConfig(nil), dataDir: dataDir}

	out := captureStderr(t, func() {
		if err := first.initAuthStore(); err != nil {
			t.Fatalf("initAuthStore: %v", err)
		}
	})
	if !strings.Contains(out, "new database created") {
		t.Errorf("first start did not report a new auth store:\n%s", out)
	}
	if err := first.authStore.Close(); err != nil {
		t.Fatalf("closing auth store: %v", err)
	}

	second := &Server{cfg: lockoutConfig(nil), dataDir: dataDir}
	out = captureStderr(t, func() {
		if err := second.initAuthStore(); err != nil {
			t.Fatalf("initAuthStore on the second start: %v", err)
		}
	})
	defer func() {
		if err := second.authStore.Close(); err != nil {
			t.Errorf("closing auth store: %v", err)
		}
	}()
	if !strings.Contains(out, "0 user(s), 0 token(s)") {
		t.Errorf("second start did not report the existing store's counts:\n%s", out)
	}
	if !strings.Contains(out, "No users or tokens configured") {
		t.Errorf("an empty store should say how to create a user:\n%s", out)
	}

	// A data directory that is really a file cannot be created, and the
	// error has to name the failure rather than reach NewAuthStore.
	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("occupied"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	blocked := &Server{cfg: lockoutConfig(nil), dataDir: filePath}
	err := blocked.initAuthStore()
	if err == nil {
		t.Fatal("expected an error for a data directory that is a file")
	}
	if !strings.Contains(err.Error(), "failed to create data directory") {
		t.Errorf("err = %v, want it to name the data directory failure", err)
	}
}

// TestInitRateLimiterReportsLockoutState covers both halves of the
// start-up report. Lockout is the only per-account protection against
// password guessing, since the rate limiter it sits beside is keyed on
// the client address, is reset by any successful login and is not
// shared between replicas, so a deployment running without it has to be
// told at every start rather than left to notice a missing line.
func TestInitRateLimiterReportsLockoutState(t *testing.T) {
	tests := map[string]struct {
		raw        *int
		want       string
		unexpected string
	}{
		"omitted key reports the default": {
			raw:        nil,
			want:       "Account lockout enabled: 10 failed attempts",
			unexpected: "WARNING",
		},
		"explicit value is reported": {
			raw:        lockoutThreshold(5),
			want:       "Account lockout enabled: 5 failed attempts",
			unexpected: "WARNING",
		},
		"disabled lockout warns": {
			raw:        lockoutThreshold(0),
			want:       "WARNING: http.auth.max_failed_attempts_before_lockout is 0",
			unexpected: "Account lockout enabled",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{cfg: lockoutConfig(tc.raw)}

			out := captureStderr(t, func() {
				if err := server.initRateLimiter(); err != nil {
					t.Fatalf("initRateLimiter: %v", err)
				}
			})

			if !strings.Contains(out, tc.want) {
				t.Errorf("start-up output missing %q:\n%s", tc.want, out)
			}
			if strings.Contains(out, tc.unexpected) {
				t.Errorf("start-up output should not contain %q:\n%s", tc.unexpected, out)
			}
			if server.rateLimiter == nil {
				t.Error("initRateLimiter left the rate limiter unset")
			}
		})
	}
}
