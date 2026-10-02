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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// insertConnQueriesTestConnection stores a connection row with the given
// name, sharing flag, owner and (possibly nil) encrypted password, and
// returns its id.
func insertConnQueriesTestConnection(
	t *testing.T,
	pool *pgxpool.Pool,
	name string,
	isShared bool,
	owner *string,
	passwordEncrypted *string,
) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO connections (name, host, database_name, username,
                                 owner_username, is_shared, password_encrypted)
        VALUES ($1, 'db.example.com', 'postgres', 'postgres', $2, $3, $4)
        RETURNING id
    `, name, owner, isShared, passwordEncrypted).Scan(&id); err != nil {
		t.Fatalf("insert connection %q: %v", name, err)
	}
	return id
}

// TestGetConnectionSharingInfo covers the owned, shared, ownerless and
// missing-row cases.
func TestGetConnectionSharingInfo(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	owner := "alice"
	privateID := insertConnQueriesTestConnection(t, pool, "private", false, &owner, nil)
	sharedID := insertConnQueriesTestConnection(t, pool, "shared", true, nil, nil)

	tests := []struct {
		name       string
		id         int
		wantShared bool
		wantOwner  string
	}{
		{name: "private connection reports its owner", id: privateID, wantShared: false, wantOwner: "alice"},
		{name: "ownerless connection reports an empty owner", id: sharedID, wantShared: true, wantOwner: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shared, gotOwner, err := ds.GetConnectionSharingInfo(ctx, tc.id)
			if err != nil {
				t.Fatalf("GetConnectionSharingInfo(%d) failed: %v", tc.id, err)
			}
			if shared != tc.wantShared || gotOwner != tc.wantOwner {
				t.Fatalf("GetConnectionSharingInfo(%d) = (%v, %q), want (%v, %q)",
					tc.id, shared, gotOwner, tc.wantShared, tc.wantOwner)
			}
		})
	}

	t.Run("missing connection returns a wrapped ErrNoRows", func(t *testing.T) {
		shared, gotOwner, err := ds.GetConnectionSharingInfo(ctx, 999999)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("GetConnectionSharingInfo(999999) error = %v, want wrapped pgx.ErrNoRows", err)
		}
		if shared || gotOwner != "" {
			t.Fatalf("GetConnectionSharingInfo(999999) = (%v, %q), want zero values", shared, gotOwner)
		}
	})
}

// TestGetConnectionWithPassword covers decrypting a stored password, a
// row with no password, a missing row, a missing server secret and a
// ciphertext that will not decrypt.
func TestGetConnectionWithPassword(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	cipher, err := crypto.EncryptPassword("example-password", connUpdatePasswordTestSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}
	empty := ""
	garbage := "not-a-valid-ciphertext"
	withPasswordID := insertConnQueriesTestConnection(t, pool, "with-password", false, nil, &cipher)
	nullPasswordID := insertConnQueriesTestConnection(t, pool, "null-password", false, nil, nil)
	emptyPasswordID := insertConnQueriesTestConnection(t, pool, "empty-password", false, nil, &empty)
	garbageID := insertConnQueriesTestConnection(t, pool, "garbage-password", false, nil, &garbage)

	for _, tc := range []struct {
		name         string
		id           int
		wantPassword string
	}{
		{name: "stored password is decrypted", id: withPasswordID, wantPassword: "example-password"},
		{name: "NULL password yields an empty password", id: nullPasswordID, wantPassword: ""},
		{name: "empty password yields an empty password", id: emptyPasswordID, wantPassword: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, password, err := ds.GetConnectionWithPassword(ctx, tc.id)
			if err != nil {
				t.Fatalf("GetConnectionWithPassword(%d) failed: %v", tc.id, err)
			}
			if conn == nil || conn.ID != tc.id {
				t.Fatalf("GetConnectionWithPassword(%d) returned connection %+v", tc.id, conn)
			}
			if password != tc.wantPassword {
				t.Fatalf("GetConnectionWithPassword(%d) password = %q, want %q",
					tc.id, password, tc.wantPassword)
			}
		})
	}

	noSecret := NewTestDatastore(pool)
	for _, tc := range []struct {
		name    string
		ds      *Datastore
		id      int
		wantErr string
	}{
		{name: "missing connection", ds: ds, id: 999999, wantErr: "failed to get connection"},
		{name: "no server secret", ds: noSecret, id: withPasswordID, wantErr: "server secret is required"},
		{name: "undecryptable ciphertext", ds: ds, id: garbageID, wantErr: "failed to decrypt password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, password, err := tc.ds.GetConnectionWithPassword(ctx, tc.id)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("GetConnectionWithPassword(%d) error = %v, want one containing %q",
					tc.id, err, tc.wantErr)
			}
			if conn != nil || password != "" {
				t.Fatalf("GetConnectionWithPassword(%d) returned (%+v, %q) alongside an error",
					tc.id, conn, password)
			}
		})
	}
}

// TestUpdateConnectionFullAllFields sets every updatable column in one
// call and checks the returned row, the stored password and that the
// collector's connection_error is cleared.
func TestUpdateConnectionFullAllFields(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	id := insertConnQueriesTestConnection(t, pool, "before", false, nil, nil)
	if _, err := pool.Exec(ctx,
		`UPDATE connections SET connection_error = 'connection refused' WHERE id = $1`, id,
	); err != nil {
		t.Fatalf("seed connection_error: %v", err)
	}

	name, desc, host, hostAddr := "after", "updated", "db2.example.com", "192.0.2.10"
	port, dbName, user, password := 6432, "appdb", "app", "new-secret"
	sslMode, sslCert, sslKey, sslRoot := "verify-full", "/certs/client.crt", "/certs/client.key", "/certs/ca.crt"
	shared, monitored := true, true

	conn, err := ds.UpdateConnectionFull(ctx, id, ConnectionUpdateParams{
		Name: &name, Description: &desc, Host: &host, HostAddr: &hostAddr,
		Port: &port, DatabaseName: &dbName, Username: &user, Password: &password,
		SSLMode: &sslMode, SSLCert: &sslCert, SSLKey: &sslKey, SSLRootCert: &sslRoot,
		IsShared: &shared, IsMonitored: &monitored,
	})
	if err != nil {
		t.Fatalf("UpdateConnectionFull failed: %v", err)
	}

	got := []struct {
		field     string
		got, want any
	}{
		{"name", conn.Name, name},
		{"description", conn.Description, desc},
		{"host", conn.Host, host},
		{"hostaddr", conn.HostAddr.String, hostAddr},
		{"port", conn.Port, port},
		{"database_name", conn.DatabaseName, dbName},
		{"username", conn.Username, user},
		{"sslmode", conn.SSLMode.String, sslMode},
		{"sslcert", conn.SSLCert.String, sslCert},
		{"sslkey", conn.SSLKey.String, sslKey},
		{"sslrootcert", conn.SSLRootCert.String, sslRoot},
		{"is_shared", conn.IsShared, shared},
		{"is_monitored", conn.IsMonitored, monitored},
	}
	for _, g := range got {
		if g.got != g.want {
			t.Errorf("%s = %v, want %v", g.field, g.got, g.want)
		}
	}

	plaintext, err := crypto.DecryptPassword(conn.PasswordEncrypted.String, connUpdatePasswordTestSecret)
	if err != nil || plaintext != password {
		t.Errorf("stored password decrypts to (%q, %v), want %q", plaintext, err, password)
	}

	var connErr *string
	if err := pool.QueryRow(ctx,
		`SELECT connection_error FROM connections WHERE id = $1`, id,
	).Scan(&connErr); err != nil {
		t.Fatalf("read connection_error: %v", err)
	}
	if connErr != nil {
		t.Errorf("connection_error = %q after update, want NULL", *connErr)
	}
}

// TestUpdateConnectionFullErrorPaths covers a password with no server
// secret to encrypt it and an update of a row that does not exist.
func TestUpdateConnectionFullErrorPaths(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	id := insertConnQueriesTestConnection(t, pool, "unchanged", false, nil, nil)
	password := "new-secret"
	name := "renamed"

	if _, err := NewTestDatastore(pool).UpdateConnectionFull(ctx, id, ConnectionUpdateParams{
		Name: &name, Password: &password,
	}); err == nil || !strings.Contains(err.Error(), "server secret is required") {
		t.Errorf("UpdateConnectionFull without a server secret error = %v, want a server secret error", err)
	}

	var stored string
	if err := pool.QueryRow(ctx, `SELECT name FROM connections WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatalf("read name: %v", err)
	}
	if stored != "unchanged" {
		t.Errorf("name = %q after a rejected update, want %q", stored, "unchanged")
	}

	_, err := ds.UpdateConnectionFull(ctx, 999999, ConnectionUpdateParams{Name: &name})
	if !errors.Is(err, pgx.ErrNoRows) || !strings.Contains(err.Error(), "failed to update connection") {
		t.Errorf("UpdateConnectionFull(999999) error = %v, want a wrapped pgx.ErrNoRows", err)
	}
}

// insertListDatabasesTestConnection stores a connection pointing at the
// Postgres instance named by TEST_AI_WORKBENCH_SERVER, with the port and
// sslmode overridable so the error paths can be reached, and returns its
// id. The test DSN's password, if any, is stored encrypted so that
// ListDatabases goes through the decrypt path as it does in production.
func insertListDatabasesTestConnection(
	t *testing.T,
	ds *Datastore,
	pool *pgxpool.Pool,
	name string,
	port int,
	sslMode string,
) int {
	t.Helper()
	parsed, err := pgx.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	if port == 0 {
		port = int(parsed.Port)
	}
	params := ConnectionCreateParams{
		Name:          name,
		Host:          parsed.Host,
		Port:          port,
		DatabaseName:  parsed.Database,
		Username:      parsed.User,
		Password:      parsed.Password,
		OwnerUsername: "test-user",
	}
	if sslMode != "" {
		params.SSLMode = &sslMode
	}
	conn, err := ds.CreateConnection(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateConnection(%q) failed: %v", name, err)
	}
	return conn.ID
}

// TestListDatabases connects to the local test Postgres through a stored
// connection and lists its databases, then covers the failures before and
// during that connection.
func TestListDatabases(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	var currentDB string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&currentDB); err != nil {
		t.Fatalf("read current_database: %v", err)
	}

	t.Run("lists the databases on the server", func(t *testing.T) {
		id := insertListDatabasesTestConnection(t, ds, pool, "local", 0, "")
		databases, err := ds.ListDatabases(ctx, id)
		if err != nil {
			t.Fatalf("ListDatabases failed: %v", err)
		}
		var found bool
		for _, db := range databases {
			if db.Name == "template0" || db.Name == "template1" {
				t.Errorf("ListDatabases returned excluded database %q", db.Name)
			}
			if db.Name == currentDB {
				found = true
				if db.Owner == "" || db.Encoding == "" || db.Size == "" {
					t.Errorf("ListDatabases entry for %q is incomplete: %+v", currentDB, db)
				}
			}
		}
		if !found {
			t.Fatalf("ListDatabases did not include the test database %q: %+v", currentDB, databases)
		}
	})

	for _, tc := range []struct {
		name    string
		port    int
		sslMode string
		wantErr string
	}{
		{
			name:    "invalid sslmode fails to build the pool",
			sslMode: "not-a-mode",
			wantErr: "failed to connect to server",
		},
		{
			// Port 1 is tcpmux, which nothing on a test host listens on,
			// so the connection is refused at the first query.
			name:    "unreachable server fails the query",
			port:    1,
			wantErr: "failed to query databases",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := insertListDatabasesTestConnection(t, ds, pool, tc.name, tc.port, tc.sslMode)
			databases, err := ds.ListDatabases(ctx, id)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ListDatabases error = %v, want one containing %q", err, tc.wantErr)
			}
			if databases != nil {
				t.Fatalf("ListDatabases returned %+v alongside an error", databases)
			}
		})
	}

	t.Run("missing connection", func(t *testing.T) {
		if _, err := ds.ListDatabases(ctx, 999999); err == nil ||
			!strings.Contains(err.Error(), "failed to get connection") {
			t.Fatalf("ListDatabases(999999) error = %v, want a get-connection failure", err)
		}
	})
}
