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
	"fmt"
	"os"
	"regexp"

	"github.com/jackc/pgx/v5"
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
//
// allowedBaseDatabase is the configured name alone. allowedTestDatabase
// also admits the per-package databases RunPackage derives from an
// allowed base by appending PackageMarker and a lower-case package tag
// ("ai_workbench_sess2_pkg_engine", "postgres_pkg_alerterdatabase"), and
// nothing else: a name ending in any other suffix is still refused.
var (
	allowedBaseDatabase = regexp.MustCompile(
		`^(ai_workbench(_(pr|issue|sess)[0-9]+)?|postgres)$`)
	allowedTestDatabase = regexp.MustCompile(
		`^(ai_workbench(_(pr|issue|sess)[0-9]+)?|postgres)(_pkg_[a-z][a-z0-9]*)?$`)
)

// allowedTestHosts are the only hosts the integration tests may connect
// to. The loopback-only check is the primary safety net.
var allowedTestHosts = map[string]struct{}{
	"127.0.0.1": {},
	"::1":       {},
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

	if _, err := check(dsn, allowedTestDatabase); err != nil {
		t.Fatalf("%v", err)
		return ""
	}
	return dsn
}

// check returns dsn's parsed connection settings, or an error unless dsn
// parses, names only loopback hosts (primary and fallbacks) and names a
// database that matches names. It parses with the pool parser, which
// also accepts the pool_* parameters a plain pgx parse would reject.
func check(dsn string, names *regexp.Regexp) (*pgx.ConnConfig, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse test DSN: %w", err)
	}
	// pgx tries the fallback hosts when the primary fails, so each one
	// must be a loopback host too.
	hosts := []string{cfg.ConnConfig.Host}
	for _, fb := range cfg.ConnConfig.Fallbacks {
		hosts = append(hosts, fb.Host)
	}
	for _, host := range hosts {
		if _, ok := allowedTestHosts[host]; !ok {
			return nil, fmt.Errorf("refusing to run destructive integration tests "+
				"against non-loopback host %q; set "+
				"TEST_AI_WORKBENCH_SERVER to a "+
				"postgresql://...@127.0.0.1 DSN", host)
		}
	}
	if !names.MatchString(cfg.ConnConfig.Database) {
		return nil, fmt.Errorf("refusing to run destructive integration tests "+
			"against database %q; expected ai_workbench, "+
			"ai_workbench_pr<n>, ai_workbench_issue<n>, "+
			"ai_workbench_sess<n> or postgres, or one of those "+
			"followed by %s<package>", cfg.ConnConfig.Database, PackageMarker)
	}
	return cfg.ConnConfig, nil
}
