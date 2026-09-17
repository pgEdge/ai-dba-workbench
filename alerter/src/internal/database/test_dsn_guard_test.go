/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/testdsn"
)

// requireLocalTestDSN returns the DSN every integration test in this
// package must use, having checked that it points at a local loopback
// Postgres holding one of the known safe test databases. The rule itself
// lives in internal/testdsn, which holds the only copy and its test; this
// wrapper keeps the call sites in this package short.
func requireLocalTestDSN(t *testing.T, purpose string) string {
	t.Helper()
	return testdsn.Require(t, purpose)
}
