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
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// nullDescriptionTestSchema reproduces the connections table as it stood
// before collector schema migration #17, with description nullable, so the
// tests below can store the NULL that an older server wrote for a
// connection created without a description (GitHub issue #540).
const nullDescriptionTestSchema = `
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    host VARCHAR(255) NOT NULL,
    hostaddr VARCHAR(255),
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    password_encrypted TEXT,
    sslmode VARCHAR(50),
    sslcert TEXT,
    sslkey TEXT,
    sslrootcert TEXT,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    membership_source VARCHAR(20) NOT NULL DEFAULT 'auto',
    cluster_id INTEGER,
    connection_error TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const nullDescriptionTestTeardown = `
DROP TABLE IF EXISTS connections CASCADE;
`

// insertNullDescriptionSQL stores a connection whose description is NULL,
// the row shape issue #540 left behind.
const insertNullDescriptionSQL = `
INSERT INTO connections (name, description, host, database_name, username, owner_username)
VALUES ($1, NULL, 'db.example.com', 'postgres', 'postgres', 'test-user')
RETURNING id
`

// newNullDescriptionTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with the pre-migration
// connections table. The caller receives a cleanup that drops the table
// and closes the pool.
func newNullDescriptionTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping NULL description integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, nullDescriptionTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create NULL description test schema: %v", err)
	}

	ds := NewTestDatastoreWithSecret(pool, connUpdatePasswordTestSecret)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), nullDescriptionTestTeardown); err != nil {
			t.Logf("NULL description teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// TestCreateConnectionWithoutDescriptionStoresEmptyString is the
// regression test for issue #540: creating a connection with no
// description must succeed and store the empty string, not NULL, even on
// a datastore whose column still accepts NULL.
func TestCreateConnectionWithoutDescriptionStoresEmptyString(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	conn, err := ds.CreateConnection(ctx, ConnectionCreateParams{
		Name:          "no-description",
		Host:          "db.example.com",
		Port:          5432,
		DatabaseName:  "postgres",
		Username:      "postgres",
		OwnerUsername: "test-user",
	})
	if err != nil {
		t.Fatalf("CreateConnection without a description: %v", err)
	}
	if conn.Description != "" {
		t.Errorf("returned description = %q, want the empty string", conn.Description)
	}

	var isNull bool
	if err := pool.QueryRow(ctx,
		`SELECT description IS NULL FROM connections WHERE id = $1`,
		conn.ID).Scan(&isNull); err != nil {
		t.Fatalf("read stored description: %v", err)
	}
	if isNull {
		t.Error("stored description is NULL, want the empty string")
	}

	desc := "given description"
	withDesc, err := ds.CreateConnection(ctx, ConnectionCreateParams{
		Name:          "with-description",
		Description:   &desc,
		Host:          "db.example.com",
		Port:          5432,
		DatabaseName:  "postgres",
		Username:      "postgres",
		OwnerUsername: "test-user",
	})
	if err != nil {
		t.Fatalf("CreateConnection with a description: %v", err)
	}
	if withDesc.Description != desc {
		t.Errorf("returned description = %q, want %q", withDesc.Description, desc)
	}
}

// TestConnectionReadsTolerateNullDescription verifies that a row already
// holding a NULL description, left by an older server, no longer breaks
// the list, the single-connection read or the update paths, and reads
// back as the empty string.
func TestConnectionReadsTolerateNullDescription(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	var id int
	if err := pool.QueryRow(ctx, insertNullDescriptionSQL, "legacy-null").Scan(&id); err != nil {
		t.Fatalf("seed NULL description row: %v", err)
	}

	list, err := ds.GetAllConnections(ctx)
	if err != nil {
		t.Fatalf("GetAllConnections with a NULL description row: %v", err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("GetAllConnections = %+v, want the one seeded row", list)
	}
	if list[0].Description != "" {
		t.Errorf("list description = %q, want the empty string", list[0].Description)
	}

	got, err := ds.GetConnection(ctx, id)
	if err != nil {
		t.Fatalf("GetConnection with a NULL description: %v", err)
	}
	if got.Description != "" {
		t.Errorf("GetConnection description = %q, want the empty string", got.Description)
	}

	renamed, err := ds.UpdateConnectionName(ctx, id, "legacy-renamed")
	if err != nil {
		t.Fatalf("UpdateConnectionName with a NULL description: %v", err)
	}
	if renamed.Name != "legacy-renamed" || renamed.Description != "" {
		t.Errorf("UpdateConnectionName = (%q, %q), want (legacy-renamed, empty)",
			renamed.Name, renamed.Description)
	}

	host := "db2.example.com"
	updated, err := ds.UpdateConnectionFull(ctx, id, ConnectionUpdateParams{Host: &host})
	if err != nil {
		t.Fatalf("UpdateConnectionFull with a NULL description: %v", err)
	}
	if updated.Host != host || updated.Description != "" {
		t.Errorf("UpdateConnectionFull = (%q, %q), want (%q, empty)",
			updated.Host, updated.Description, host)
	}
}

// failingScanner is a row whose Scan always fails, used to drive the scan
// helpers' error returns.
type failingScanner struct{ err error }

func (f failingScanner) Scan(...any) error { return f.err }

// TestConnectionScanHelpersPropagateErrors verifies that a failing Scan is
// returned to the caller rather than yielding a half-populated row.
func TestConnectionScanHelpersPropagateErrors(t *testing.T) {
	scanErr := errors.New("scan failed")

	conn, err := scanFullConnection(failingScanner{err: scanErr})
	if !errors.Is(err, scanErr) || conn != nil {
		t.Errorf("scanFullConnection = (%v, %v), want (nil, %v)", conn, err, scanErr)
	}

	var item ConnectionListItem
	if err := scanConnectionListItem(&item, failingScanner{err: scanErr}); !errors.Is(err, scanErr) {
		t.Errorf("scanConnectionListItem error = %v, want %v", err, scanErr)
	}
}

// TestCreateConnectionAndListErrorPaths covers the failures CreateConnection
// and GetAllConnections report: a password with no server secret to
// encrypt it, a row the table rejects, a row that cannot be scanned, and a
// missing table.
func TestCreateConnectionAndListErrorPaths(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	base := ConnectionCreateParams{
		Name:          "error-paths",
		Host:          "db.example.com",
		Port:          5432,
		DatabaseName:  "postgres",
		Username:      "postgres",
		OwnerUsername: "test-user",
	}

	noSecret := NewTestDatastore(pool)
	withPassword := base
	withPassword.Password = "example-password"
	if _, err := noSecret.CreateConnection(ctx, withPassword); err == nil ||
		!strings.Contains(err.Error(), "server secret") {
		t.Errorf("CreateConnection without a server secret error = %v, want a server secret error", err)
	}

	tooLong := base
	tooLong.Name = strings.Repeat("n", 300)
	if _, err := ds.CreateConnection(ctx, tooLong); err == nil ||
		!strings.Contains(err.Error(), "failed to create connection") {
		t.Errorf("CreateConnection with an over-long name error = %v, want a create failure", err)
	}

	if _, err := pool.Exec(ctx, `
		ALTER TABLE connections ALTER COLUMN cluster_id TYPE TEXT;
		INSERT INTO connections (name, host, database_name, username, owner_username, cluster_id)
		VALUES ('bad-cluster', 'db.example.com', 'postgres', 'postgres', 'test-user', 'not-a-number');
	`); err != nil {
		t.Fatalf("seed an unscannable row: %v", err)
	}
	if _, err := ds.GetAllConnections(ctx); err == nil ||
		!strings.Contains(err.Error(), "failed to read connections") {
		t.Errorf("GetAllConnections over an unscannable row error = %v, want a read failure", err)
	}

	if _, err := pool.Exec(ctx, nullDescriptionTestTeardown); err != nil {
		t.Fatalf("drop connections: %v", err)
	}
	if _, err := ds.GetAllConnections(ctx); err == nil ||
		!strings.Contains(err.Error(), "failed to query connections") {
		t.Errorf("GetAllConnections without a table error = %v, want a query failure", err)
	}
}
