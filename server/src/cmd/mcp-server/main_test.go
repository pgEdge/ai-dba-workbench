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
	"os"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
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

	os.Exit(m.Run())
}
