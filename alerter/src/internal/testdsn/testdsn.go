/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package testdsn holds the guard every alerter integration test goes
// through before it connects to a database. It is a non-test package so
// that the rule has one definition rather than one per test package:
// the integration schemas drop and recreate tables and the whole
// metrics schema, so a DSN pointing anywhere but a local throwaway
// database would destroy it.
package testdsn

import (
	"os"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
)

// allowedTestDatabase matches the database names the integration tests
// may target: "ai_workbench" for local development, the per-task
// "ai_workbench_<tag>" databases that concurrent local sessions use so
// their schema resets do not collide, and "postgres" for CI, which is
// the default database the postgres Docker image creates.
//
// The tag is the shape every session actually generates, an issue, pull
// request or session number ("pr407", "issue407", "sess2"), and nothing
// else. The name check is a convenience rather than the safety net: it
// keeps an obviously production-shaped name such as "ai_workbench_prod"
// from being wiped by a typo, whilst the loopback host check is
// what actually confines these destructive tests to this machine.
var allowedTestDatabase = regexp.MustCompile(
	`^(ai_workbench(_(pr|issue|sess)[0-9]+)?|postgres)$`)

// allowedTestHosts are the only hosts the integration tests may connect
// to. The loopback-only check is the primary safety net.
var allowedTestHosts = map[string]struct{}{
	"127.0.0.1": {},
	"localhost": {},
	"":          {}, // unix socket; only reachable on this host
}

// TB is the part of *testing.T this package needs. Taking an interface
// rather than the concrete type lets the guard's own tests exercise the
// skip and failure branches with a recorder, which a real *testing.T
// would turn into a skipped or failed test.
type TB interface {
	Helper()
	Skip(args ...any)
	Skipf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Require returns the DSN every integration test in the alerter must
// use, having checked that it points at a local loopback Postgres
// holding one of the known safe test databases. The test is skipped when
// SKIP_DB_TESTS is set or TEST_AI_WORKBENCH_SERVER is empty, and failed
// outright when the DSN points anywhere else: CLAUDE.local.md is
// explicit that these tests must only target a local loopback Postgres,
// and the destructive DDL in the integration schemas, which drops and
// recreates tables and the whole metrics schema, would wipe any other
// instance the variable resolved to.
//
// Every entry point that reads TEST_AI_WORKBENCH_SERVER goes through
// this helper, so a new integration test cannot acquire a connection
// string without the guard. It lives in its own package, rather than
// once per test package, so that the rule has a single definition.
// purpose names the test in the skip message.
//
// Each skip and failure is followed by an explicit return. A real
// *testing.T never reaches them, because Skip and Fatalf end the
// goroutine, but the recorder used in this package's own tests does, and
// returning keeps it from running on with a DSN the guard just refused.
func Require(t TB, purpose string) string {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
		return ""
	}
	dsn := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if dsn == "" {
		t.Skipf("TEST_AI_WORKBENCH_SERVER not set, skipping %s", purpose)
		return ""
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
		return ""
	}
	if _, ok := allowedTestHosts[cfg.ConnConfig.Host]; !ok {
		t.Fatalf("refusing to run destructive integration tests "+
			"against non-loopback host %q; set "+
			"TEST_AI_WORKBENCH_SERVER to a "+
			"postgresql://...@127.0.0.1 DSN", cfg.ConnConfig.Host)
		return ""
	}
	if !allowedTestDatabase.MatchString(cfg.ConnConfig.Database) {
		t.Fatalf("refusing to run destructive integration tests "+
			"against database %q; expected ai_workbench, "+
			"ai_workbench_pr<n>, ai_workbench_issue<n>, "+
			"ai_workbench_sess<n> or postgres",
			cfg.ConnConfig.Database)
		return ""
	}
	return dsn
}
