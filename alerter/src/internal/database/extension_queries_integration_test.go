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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// extensionQueriesTestSchema holds the tables GetConnectionsWithExtension
// and GetAlertRuleByID read. metrics.pg_extension mirrors the collector's
// column set; alert_rules mirrors the production definition.
const extensionQueriesTestSchema = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_extension (
    connection_id INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    extname TEXT NOT NULL,
    extversion TEXT,
    extrelocatable BOOLEAN,
    schema_name TEXT,
    collected_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (connection_id, database_name, extname, collected_at)
);

CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    category VARCHAR(100) NOT NULL DEFAULT 'general',
    metric_name VARCHAR(255) NOT NULL,
    default_operator VARCHAR(10) NOT NULL DEFAULT '>',
    default_threshold REAL NOT NULL DEFAULT 0,
    default_severity VARCHAR(20) NOT NULL DEFAULT 'warning',
    default_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    required_extension VARCHAR(100),
    is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const extensionQueriesTestTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
`

func newExtensionQueriesTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	connStr := requireLocalTestDSN(t, "the extension query integration test")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, extensionQueriesTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create extension query test schema: %v", err)
	}

	ds := &Datastore{pool: pool, config: nil}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), extensionQueriesTestTeardown); err != nil {
			t.Logf("extension query teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertExtensionSnapshot writes one pg_extension snapshot for a
// connection: every (database, extname) pair shares the same collected_at,
// which is how the collector's change-tracked probe stores it.
func insertExtensionSnapshot(t *testing.T, pool *pgxpool.Pool, connID int,
	collectedAt time.Time, exts map[string][]string) {
	t.Helper()

	for db, names := range exts {
		for _, name := range names {
			if _, err := pool.Exec(context.Background(), `
				INSERT INTO metrics.pg_extension
				    (connection_id, database_name, extname, extversion, collected_at)
				VALUES ($1, $2, $3, '1.0', $4)
			`, connID, db, name, collectedAt); err != nil {
				t.Fatalf("failed to insert pg_extension row: %v", err)
			}
		}
	}
}

// TestGetConnectionsWithExtension_NewestSnapshot verifies that only the
// newest snapshot per connection is consulted: a connection whose older
// snapshot listed the extension but whose newest one does not is
// reported absent, a connection with the extension in any database of
// its newest snapshot is present, and a connection with no rows at all
// is absent.
func TestGetConnectionsWithExtension_NewestSnapshot(t *testing.T) {
	ds, pool, cleanup := newExtensionQueriesTestDatastore(t)
	defer cleanup()

	now := time.Now().UTC()
	const (
		present   = 1
		removed   = 2
		otherDB   = 3
		neverHad  = 4
		installed = 5
	)

	// Has the extension in its newest snapshot, alongside plpgsql.
	insertExtensionSnapshot(t, pool, present, now.Add(-48*time.Hour),
		map[string][]string{"postgres": {"plpgsql", "pg_stat_statements"}})

	// Had it two days ago, dropped it an hour ago.
	insertExtensionSnapshot(t, pool, removed, now.Add(-48*time.Hour),
		map[string][]string{"postgres": {"plpgsql", "pg_stat_statements"}})
	insertExtensionSnapshot(t, pool, removed, now.Add(-1*time.Hour),
		map[string][]string{"postgres": {"plpgsql"}})

	// Newest snapshot has it only in a secondary database.
	insertExtensionSnapshot(t, pool, otherDB, now.Add(-1*time.Hour),
		map[string][]string{
			"postgres": {"plpgsql"},
			"appdb":    {"plpgsql", "pg_stat_statements"},
		})

	// Never had it in any snapshot.
	insertExtensionSnapshot(t, pool, neverHad, now.Add(-1*time.Hour),
		map[string][]string{"postgres": {"plpgsql"}})

	// Installed it an hour ago after a snapshot without it.
	insertExtensionSnapshot(t, pool, installed, now.Add(-48*time.Hour),
		map[string][]string{"postgres": {"plpgsql"}})
	insertExtensionSnapshot(t, pool, installed, now.Add(-1*time.Hour),
		map[string][]string{"postgres": {"plpgsql", "pg_stat_statements"}})

	got, err := ds.GetConnectionsWithExtension(context.Background(), "pg_stat_statements")
	if err != nil {
		t.Fatalf("GetConnectionsWithExtension failed: %v", err)
	}

	want := map[int]bool{present: true, otherDB: true, installed: true}
	if len(got) != len(want) {
		t.Errorf("connections with extension = %v, want %v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("connection %d present = %v, want %v", id, got[id], w)
		}
	}
	for _, id := range []int{removed, neverHad, 99} {
		if got[id] {
			t.Errorf("connection %d unexpectedly reported as having the extension", id)
		}
	}
}

// TestGetConnectionsWithExtension_Empty verifies that an empty table
// yields an empty, non-nil set rather than an error, so callers can index
// it directly.
func TestGetConnectionsWithExtension_Empty(t *testing.T) {
	ds, _, cleanup := newExtensionQueriesTestDatastore(t)
	defer cleanup()

	got, err := ds.GetConnectionsWithExtension(context.Background(), "spock")
	if err != nil {
		t.Fatalf("GetConnectionsWithExtension failed: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty non-nil set, got %v", got)
	}
}

// TestGetConnectionsWithExtension_QueryError verifies that a failing
// query is reported rather than swallowed; the engine relies on the
// error to fall back to evaluating without the gate.
func TestGetConnectionsWithExtension_QueryError(t *testing.T) {
	ds, pool, cleanup := newExtensionQueriesTestDatastore(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		`DROP TABLE metrics.pg_extension`); err != nil {
		t.Fatalf("failed to drop pg_extension: %v", err)
	}

	if _, err := ds.GetConnectionsWithExtension(context.Background(), "spock"); err == nil {
		t.Fatal("expected an error when metrics.pg_extension is missing")
	}
}

// TestGetConnectionsWithExtension_ScanError covers the scan-error
// branch. The fixture owns metrics.pg_extension, so it is rebuilt with a
// text connection_id holding a value that cannot be scanned into an int.
func TestGetConnectionsWithExtension_ScanError(t *testing.T) {
	ds, pool, cleanup := newExtensionQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		DROP TABLE metrics.pg_extension;
		CREATE TABLE metrics.pg_extension (
		    connection_id TEXT NOT NULL,
		    database_name TEXT NOT NULL,
		    extname TEXT NOT NULL,
		    collected_at TIMESTAMPTZ NOT NULL
		);
		INSERT INTO metrics.pg_extension VALUES ('not-a-number', 'postgres', 'spock', NOW());
	`); err != nil {
		t.Fatalf("failed to rebuild pg_extension with a text id: %v", err)
	}

	if _, err := ds.GetConnectionsWithExtension(ctx, "spock"); err == nil {
		t.Fatal("expected a scan error for a non-numeric connection_id")
	}
}

// TestGetAlertRuleByID verifies the lookup mirrors GetAlertRuleByName,
// including the nullable required_extension column and the not-found
// error for an unknown id.
func TestGetAlertRuleByID(t *testing.T) {
	ds, pool, cleanup := newExtensionQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	var withExt, withoutExt int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alert_rules (name, description, category, metric_name,
		    default_operator, default_threshold, default_severity,
		    default_enabled, required_extension, is_built_in)
		VALUES ('spock_rule', 'needs spock', 'replication', 'spock.metric',
		    '>', 5, 'critical', TRUE, 'spock', TRUE)
		RETURNING id
	`).Scan(&withExt); err != nil {
		t.Fatalf("failed to insert rule: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO alert_rules (name, metric_name)
		VALUES ('plain_rule', 'plain.metric')
		RETURNING id
	`).Scan(&withoutExt); err != nil {
		t.Fatalf("failed to insert rule: %v", err)
	}

	rule, err := ds.GetAlertRuleByID(ctx, withExt)
	if err != nil {
		t.Fatalf("GetAlertRuleByID(%d) failed: %v", withExt, err)
	}
	if rule.ID != withExt || rule.Name != "spock_rule" || rule.MetricName != "spock.metric" ||
		rule.DefaultOperator != ">" || rule.DefaultThreshold != 5 ||
		rule.DefaultSeverity != "critical" || !rule.DefaultEnabled || !rule.IsBuiltIn ||
		rule.Category != "replication" || rule.Description != "needs spock" {
		t.Errorf("unexpected rule fields: %+v", rule)
	}
	if rule.RequiredExtension == nil || *rule.RequiredExtension != "spock" {
		t.Errorf("required_extension = %v, want spock", rule.RequiredExtension)
	}

	byName, err := ds.GetAlertRuleByName(ctx, "spock_rule")
	if err != nil {
		t.Fatalf("GetAlertRuleByName failed: %v", err)
	}
	if byName.ID != rule.ID || byName.CreatedAt != rule.CreatedAt {
		t.Errorf("GetAlertRuleByID and GetAlertRuleByName disagree: %+v vs %+v", rule, byName)
	}

	plain, err := ds.GetAlertRuleByID(ctx, withoutExt)
	if err != nil {
		t.Fatalf("GetAlertRuleByID(%d) failed: %v", withoutExt, err)
	}
	if plain.RequiredExtension != nil {
		t.Errorf("required_extension = %q, want nil", *plain.RequiredExtension)
	}

	if _, err := ds.GetAlertRuleByID(ctx, withoutExt+1000); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("unknown id error = %v, want pgx.ErrNoRows", err)
	}
}
