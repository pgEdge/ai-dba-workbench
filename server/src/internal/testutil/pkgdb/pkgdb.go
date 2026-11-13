/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package pkgdb gives each database-backed server test package a
// PostgreSQL database of its own, so that `go test ./...` can run the
// package test binaries in parallel.
//
// Several packages drop and recreate the same tables. Run, called from a
// package's TestMain, creates a database named after the configured test
// database plus "_pkg_" and a tag naming the package (for example
// ai_workbench_sess12_pkg_api), points TEST_AI_WORKBENCH_SERVER at it
// for the rest of the test binary, and drops it when the tests finish.
// The tests themselves are unchanged: they read the variable as before,
// and skip when it is unset exactly as before.
//
// Only test code may import this package.
package pkgdb

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvVar is the variable that names the test database.
const EnvVar = "TEST_AI_WORKBENCH_SERVER"

// keepEnv, when set to 1 or true, keeps the package database after the
// run, as it does for the collector's per-run databases.
const keepEnv = "TEST_AI_WORKBENCH_KEEP_DB"

// Marker separates the configured database's name from the package tag.
const Marker = "_pkg_"

// MaxIdentifierLength is PostgreSQL's NAMEDATALEN - 1. A longer database
// name would be truncated, and the tests would reach the wrong database.
const MaxIdentifierLength = 63

// tagPattern is the shape of a package tag: short and lower case, so the
// derived name needs no quoting to read and stays well under the limit.
var tagPattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// statementsQuery returns the package database's name and the statements
// that drop and create it. The server quotes the name with %I, so the SQL
// text sent from Go stays constant. The package database copies the
// configured database's encoding, locale and locale provider from
// template0 rather than taking template1's: on a cluster whose template1
// is SQL_ASCII, as on the pgEdge image, a UTF8 test database would
// otherwise get SQL_ASCII package databases, where pgx refuses
// simple-protocol queries. to_jsonb reads the provider columns by name,
// as daticulocale (PostgreSQL 15 and 16) became datlocale in 17.
const statementsQuery = `
        SELECT n,
            format('DROP DATABASE IF EXISTS %I WITH (FORCE)', n),
            format('CREATE DATABASE %I TEMPLATE template0 ENCODING %L '
                || 'LC_COLLATE %L LC_CTYPE %L',
                n, pg_encoding_to_char(d.encoding), d.datcollate, d.datctype)
            || CASE to_jsonb(d)->>'datlocprovider'
                WHEN 'i' THEN format(' LOCALE_PROVIDER icu ICU_LOCALE %L',
                    coalesce(to_jsonb(d)->>'datlocale', to_jsonb(d)->>'daticulocale'))
                WHEN 'b' THEN format(' LOCALE_PROVIDER builtin BUILTIN_LOCALE %L',
                    to_jsonb(d)->>'datlocale')
                WHEN 'c' THEN ' LOCALE_PROVIDER libc'
                ELSE '' END
        FROM (SELECT current_database() || $1 AS n) s
        JOIN pg_database d ON d.datname = current_database()
    `

// Runner is the part of *testing.M that Run needs.
type Runner interface {
	Run() int
}

// Run creates the package database, runs the tests against it and drops
// it again, returning the exit code for os.Exit. When SKIP_DB_TESTS is
// set or TEST_AI_WORKBENCH_SERVER is empty it runs the tests unchanged,
// so they skip as they always have; likewise when the server cannot be
// reached, so the tests' own connection checks decide what happens.
//
// It fails the run, without connecting, when the variable names any host
// other than a loopback one, because it drops and creates databases.
//
// A database left behind by an earlier, interrupted run is dropped
// before the new one is created.
func Run(m Runner, tag string) int {
	return run(m, tag, os.Stderr)
}

func run(m Runner, tag string, log io.Writer) int {
	if !tagPattern.MatchString(tag) {
		fmt.Fprintf(log, "pkgdb: invalid package tag %q\n", tag)
		return 1
	}
	if os.Getenv("SKIP_DB_TESTS") != "" {
		return m.Run()
	}
	base := os.Getenv(EnvVar)
	if base == "" {
		return m.Run()
	}

	// The pool parser also accepts the pool_* parameters a plain pgx
	// parse would reject.
	poolCfg, err := pgxpool.ParseConfig(base)
	if err != nil {
		fmt.Fprintf(log, "pkgdb: cannot parse %s (%v); running the tests unchanged\n", EnvVar, err)
		return m.Run()
	}
	cfg := poolCfg.ConnConfig
	// A test that re-executes its own binary hands the child the
	// rewritten variable; the child must use the parent's database
	// rather than derive another from it, which it would never drop if
	// it ends in os.Exit.
	if strings.HasSuffix(cfg.Database, Marker+tag) {
		return m.Run()
	}
	if err := requireLoopback(cfg); err != nil {
		fmt.Fprintf(log, "pkgdb: %v\n", err)
		return 1
	}

	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		fmt.Fprintf(log, "pkgdb: cannot connect to the test database (%v); running the tests unchanged\n", err)
		return m.Run()
	}
	name, dropSQL, err := create(ctx, conn, tag)
	_ = conn.Close(ctx)
	if err != nil {
		fmt.Fprintf(log, "pkgdb: %v\n", err)
		return 1
	}

	dsn, err := withDatabase(base, name)
	if err != nil {
		fmt.Fprintf(log, "pkgdb: %v\n", err)
		dropDatabase(ctx, cfg, dropSQL, name, log)
		return 1
	}

	// Setenv fails only on an invalid key, and EnvVar is a constant.
	_ = os.Setenv(EnvVar, dsn)
	code := m.Run()
	_ = os.Setenv(EnvVar, base)

	if keep := os.Getenv(keepEnv); keep == "1" || strings.EqualFold(keep, "true") {
		fmt.Fprintf(log, "pkgdb: keeping test database %s\n", name)
		return code
	}
	dropDatabase(ctx, cfg, dropSQL, name, log)
	return code
}

// requireLoopback returns an error unless every host cfg may connect to,
// the primary and each fallback, is a loopback one. The rule matches the
// one the mcp-server lifecycle tests apply to the same variable:
// "localhost" or a loopback IP address.
func requireLoopback(cfg *pgx.ConnConfig) error {
	hosts := []string{cfg.Host}
	for _, fb := range cfg.Fallbacks {
		hosts = append(hosts, fb.Host)
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("refusing to create or drop test databases on "+
				"non-loopback host %q; set %s to a postgresql://...@127.0.0.1 URL",
				host, EnvVar)
		}
	}
	return nil
}

// create names the package database, drops any stale copy and creates
// it, returning its name and the statement that drops it.
func create(ctx context.Context, conn *pgx.Conn, tag string) (name, dropSQL string, err error) {
	var createSQL string
	if err := conn.QueryRow(ctx, statementsQuery, Marker+tag).
		Scan(&name, &dropSQL, &createSQL); err != nil {
		return "", "", fmt.Errorf("name the package database: %w", err)
	}
	if len(name) > MaxIdentifierLength {
		return "", "", fmt.Errorf("package database name %q is longer than %d bytes", name, MaxIdentifierLength)
	}
	if _, err := conn.Exec(ctx, dropSQL); err != nil {
		return "", "", fmt.Errorf("drop stale %s: %w", name, err)
	}
	if _, err := conn.Exec(ctx, createSQL); err != nil {
		return "", "", fmt.Errorf("create %s: %w", name, err)
	}
	return name, dropSQL, nil
}

// dropDatabase drops the package database over a fresh connection to the
// configured one. A failure is reported but does not fail the run: the
// next run drops the stale database before creating its own.
func dropDatabase(ctx context.Context, cfg *pgx.ConnConfig, dropSQL, name string, log io.Writer) {
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		fmt.Fprintf(log, "pkgdb: drop %s: %v\n", name, err)
		return
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, dropSQL); err != nil {
		fmt.Fprintf(log, "pkgdb: drop %s: %v\n", name, err)
	}
}

// withDatabase returns dsn with its database replaced by name. It accepts
// both URL and keyword/value connection strings, and checks the result
// parses to the intended database.
func withDatabase(dsn, name string) (string, error) {
	var out string
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", EnvVar, err)
		}
		u.Path = "/" + name
		u.RawPath = ""
		if q := u.Query(); q.Has("dbname") {
			q.Del("dbname")
			u.RawQuery = q.Encode()
		}
		out = u.String()
	} else {
		quoted := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(name)
		out = dsn + " dbname='" + quoted + "'"
	}

	cfg, err := pgxpool.ParseConfig(out)
	if err != nil {
		return "", fmt.Errorf("parse the package database URL: %w", err)
	}
	if cfg.ConnConfig.Database != name {
		return "", fmt.Errorf("package database URL names %q, want %q", cfg.ConnConfig.Database, name)
	}
	return out, nil
}
