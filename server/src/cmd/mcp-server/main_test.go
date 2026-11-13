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
	"io"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/testutil/pkgdb"
)

// TestMain substitutes a fixed audit chain key for the whole test
// binary. Every command-line command opens the auth store, and the
// store refuses to open without a key, which in production comes from
// the server secret file. A test host either has that file or does not,
// and letting the suite depend on which would make it pass or fail for
// reasons that have nothing to do with the code under test.
//
// The tests that care about the resolution itself override cliAuditKey
// again for their own scope and restore it afterwards.
func TestMain(m *testing.M) {
	cliAuditKey = func() ([]byte, error) {
		return auth.AuditKeyForTesting(), nil
	}
	// The tests answer the re-anchor's question from a string, not a
	// terminal; TestAuditInputIsTerminal checks the real test.
	auditInputIsTerminal = func(io.Reader) bool { return true }
	// Every store a command opens hashes at bcrypt.MinCost; at the
	// production cost a single hash takes seconds under -race, and the
	// command tests create many users and tokens. No test here asserts
	// on the cost itself; internal/auth covers that.
	configureCLIStoreForTesting = func(store *auth.AuthStore) {
		store.SetBcryptCostForTesting(testMainHelper{}, bcrypt.MinCost)
	}

	// The package's database tests run in a database of their own; see
	// pkgdb.
	os.Exit(pkgdb.Run(m, "mcpserver"))
}

// newTestAuthStore is auth.NewAuthStore for the tests' own seeding and
// inspection, applying the same lowered bcrypt cost as the stores the
// commands open, so seeding users and tokens is cheap too.
func newTestAuthStore(dataDir string, maxUserTokenDays, maxFailedAttempts int,
	auditKey []byte) (*auth.AuthStore, error) {

	store, err := auth.NewAuthStore(dataDir, maxUserTokenDays,
		maxFailedAttempts, auditKey)
	if err != nil {
		return nil, err
	}
	configureCLIStoreForTesting(store)
	return store, nil
}

// testMainHelper satisfies SetBcryptCostForTesting's non-nil test
// handle where TestMain has no *testing.T to pass.
type testMainHelper struct{}

func (testMainHelper) Helper() {}
