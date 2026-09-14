/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"os"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// allowedTestDatabase matches the database names the integration tests
// may target. The list is deliberately tiny: "ai_workbench" for local
// development, the per-task "ai_workbench_pr<number>" databases that
// concurrent local sessions use so their schema resets do not collide,
// and "postgres" for CI, which is the default database the postgres
// Docker image creates. Anything that merely looks plausible, such as
// "ai_workbench_prod" or "ai_workbench_live", is refused.
var allowedTestDatabase = regexp.MustCompile(`^(ai_workbench(_pr[0-9]+)?|postgres)$`)

// allowedTestHosts are the only hosts the integration tests may connect
// to. The loopback-only check is the primary safety net.
var allowedTestHosts = map[string]struct{}{
	"127.0.0.1": {},
	"localhost": {},
	"":          {}, // unix socket; only reachable on this host
}

// requireLocalTestDSN returns the DSN every integration test in this
// package must use, having checked that it points at a local loopback
// Postgres holding one of the known safe test databases. The test is
// skipped when SKIP_DB_TESTS is set or TEST_AI_WORKBENCH_SERVER is
// empty, and failed outright when the DSN points anywhere else:
// CLAUDE.local.md is explicit that these tests must only target a local
// loopback Postgres, and the destructive DDL in the integration schemas,
// which drops and recreates tables and the whole metrics schema, would
// wipe any other instance the variable resolved to.
//
// Every entry point that reads TEST_AI_WORKBENCH_SERVER goes through
// this helper, so a new integration test cannot acquire a connection
// string without the guard. purpose names the test in the skip message.
func requireLocalTestDSN(t *testing.T, purpose string) string {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	dsn := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if dsn == "" {
		t.Skipf("TEST_AI_WORKBENCH_SERVER not set, skipping %s", purpose)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	if _, ok := allowedTestHosts[cfg.ConnConfig.Host]; !ok {
		t.Fatalf("refusing to run destructive integration tests "+
			"against non-loopback host %q; set "+
			"TEST_AI_WORKBENCH_SERVER to a "+
			"postgresql://...@127.0.0.1 DSN", cfg.ConnConfig.Host)
	}
	if !allowedTestDatabase.MatchString(cfg.ConnConfig.Database) {
		t.Fatalf("refusing to run destructive integration tests "+
			"against database %q; expected ai_workbench, "+
			"ai_workbench_pr<number> or postgres",
			cfg.ConnConfig.Database)
	}
	return dsn
}

// TestAllowedTestDatabase pins the allowlist. It matters that plausible
// real database names are refused, because the loopback host check alone
// would happily let a test wipe a production schema reached over an SSH
// tunnel or a local port forward.
func TestAllowedTestDatabase(t *testing.T) {
	allowed := []string{"ai_workbench", "ai_workbench_pr407", "postgres"}
	refused := []string{
		"ai_workbench_prod", "ai_workbench_live", "ai_workbench_pr",
		"ai_workbench_staging", "workbench", "ai_workbench_pr407x",
		"postgres_prod", "", "ai_workbench_PR407",
	}
	for _, name := range allowed {
		if !allowedTestDatabase.MatchString(name) {
			t.Errorf("database %q should be allowed", name)
		}
	}
	for _, name := range refused {
		if allowedTestDatabase.MatchString(name) {
			t.Errorf("database %q should be refused", name)
		}
	}
}
