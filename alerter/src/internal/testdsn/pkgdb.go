/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package testdsn

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// envVar is the variable that names the test database.
const envVar = "TEST_AI_WORKBENCH_SERVER"

// keepEnv, when set to 1 or true, keeps the package database after the
// run, as it does for the collector's per-run databases.
const keepEnv = "TEST_AI_WORKBENCH_KEEP_DB"

// PackageMarker separates the configured database's name from the
// package tag in a per-package database name.
const PackageMarker = "_pkg_"

// maxIdentifierLength is PostgreSQL's NAMEDATALEN - 1. A longer database
// name would be truncated, and the tests would reach the wrong database.
const maxIdentifierLength = 63

// tagPattern is the shape of a package tag; allowedTestDatabase admits
// exactly this shape after PackageMarker.
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

// Runner is the part of *testing.M that RunPackage needs.
type Runner interface {
	Run() int
}

// RunPackage, called from a package's TestMain, gives the package's
// integration tests a database of their own so that `go test ./...` can
// run the package test binaries in parallel: several packages drop and
// recreate the same tables. It creates a database named after the
// configured one plus PackageMarker and tag, points
// TEST_AI_WORKBENCH_SERVER at it while the tests run, drops it
// afterwards and returns the exit code for os.Exit.
//
// It touches nothing unless the configured DSN passes the same loopback
// and name checks as Require, with the name being a base name rather
// than an already derived one. When SKIP_DB_TESTS is set, the variable
// is empty, the DSN is refused or the server cannot be reached, the tests
// run unchanged, so Require skips or refuses them exactly as before.
func RunPackage(m Runner, tag string) int {
	return runPackage(m, tag, os.Stderr)
}

func runPackage(m Runner, tag string, log io.Writer) int {
	if !tagPattern.MatchString(tag) {
		fmt.Fprintf(log, "testdsn: invalid package tag %q\n", tag)
		return 1
	}
	if os.Getenv("SKIP_DB_TESTS") != "" {
		return m.Run()
	}
	base := os.Getenv(envVar)
	if base == "" {
		return m.Run()
	}
	// A test that re-executes its own binary hands the child the
	// rewritten variable; the child must use the parent's database, which
	// Require accepts, rather than derive another from it.
	if derived, err := check(base, allowedTestDatabase); err == nil &&
		strings.HasSuffix(derived.Database, PackageMarker+tag) {
		return m.Run()
	}
	cfg, err := check(base, allowedBaseDatabase)
	if err != nil {
		fmt.Fprintf(log, "testdsn: %v; running the tests unchanged\n", err)
		return m.Run()
	}

	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		fmt.Fprintf(log, "testdsn: cannot connect to the test database (%v); running the tests unchanged\n", err)
		return m.Run()
	}
	name, dropSQL, err := createPackageDatabase(ctx, conn, tag)
	_ = conn.Close(ctx)
	if err != nil {
		fmt.Fprintf(log, "testdsn: %v\n", err)
		return 1
	}

	dsn, err := withDatabase(base, name)
	if err != nil {
		fmt.Fprintf(log, "testdsn: %v\n", err)
		dropPackageDatabase(ctx, cfg, dropSQL, name, log)
		return 1
	}

	// Setenv fails only on an invalid key, and envVar is a constant.
	_ = os.Setenv(envVar, dsn)
	code := m.Run()
	_ = os.Setenv(envVar, base)

	if keep := os.Getenv(keepEnv); keep == "1" || strings.EqualFold(keep, "true") {
		fmt.Fprintf(log, "testdsn: keeping test database %s\n", name)
		return code
	}
	dropPackageDatabase(ctx, cfg, dropSQL, name, log)
	return code
}

// createPackageDatabase names the package database, drops any stale copy
// and creates it, returning its name and the statement that drops it.
func createPackageDatabase(ctx context.Context, conn *pgx.Conn, tag string) (name, dropSQL string, err error) {
	var createSQL string
	if err := conn.QueryRow(ctx, statementsQuery, PackageMarker+tag).
		Scan(&name, &dropSQL, &createSQL); err != nil {
		return "", "", fmt.Errorf("name the package database: %w", err)
	}
	if len(name) > maxIdentifierLength {
		return "", "", fmt.Errorf("package database name %q is longer than %d bytes", name, maxIdentifierLength)
	}
	if _, err := conn.Exec(ctx, dropSQL); err != nil {
		return "", "", fmt.Errorf("drop stale %s: %w", name, err)
	}
	if _, err := conn.Exec(ctx, createSQL); err != nil {
		return "", "", fmt.Errorf("create %s: %w", name, err)
	}
	return name, dropSQL, nil
}

// dropPackageDatabase drops the package database over a fresh connection
// to the configured one. A failure is reported but does not fail the run:
// the next run drops the stale database before creating its own.
func dropPackageDatabase(ctx context.Context, cfg *pgx.ConnConfig, dropSQL, name string, log io.Writer) {
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		fmt.Fprintf(log, "testdsn: drop %s: %v\n", name, err)
		return
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, dropSQL); err != nil {
		fmt.Fprintf(log, "testdsn: drop %s: %v\n", name, err)
	}
}

// withDatabase returns dsn with its database replaced by name. It accepts
// both URL and keyword/value connection strings, and checks that the
// result still passes Require's checks and names the intended database.
func withDatabase(dsn, name string) (string, error) {
	var out string
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", envVar, err)
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

	got, err := check(out, allowedTestDatabase)
	if err != nil {
		return "", err
	}
	if got.Database != name {
		return "", fmt.Errorf("package database URL names %q, want %q", got.Database, name)
	}
	return out, nil
}
