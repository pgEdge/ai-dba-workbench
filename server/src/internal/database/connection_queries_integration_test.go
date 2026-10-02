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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// insertTestConnection adds a connection row with the given target and
// optional ciphertext, returning its ID.
func insertTestConnection(t *testing.T, pool *pgxpool.Pool, host string,
	port int, dbName, user string, encrypted *string, sslmode string,
	isShared bool, owner string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO connections
            (name, host, port, database_name, username, password_encrypted,
             sslmode, is_shared, owner_username)
        VALUES ('queries-test', $1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''))
        RETURNING id
    `, host, port, dbName, user, encrypted, sslmode, isShared,
		owner).Scan(&id); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	return id
}

// TestGetConnectionSharingInfo covers the found and missing cases.
func TestGetConnectionSharingInfo(t *testing.T) {
	ds, pool, cleanup := newConnUpdatePasswordTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	id := insertTestConnection(t, pool, "192.0.2.1", 5432, "app", "u",
		nil, "disable", true, "test-owner")

	shared, owner, err := ds.GetConnectionSharingInfo(ctx, id)
	if err != nil {
		t.Fatalf("GetConnectionSharingInfo: %v", err)
	}
	if !shared || owner != "test-owner" {
		t.Errorf("got shared=%v owner=%q, want true, test-owner", shared,
			owner)
	}

	if _, _, err := ds.GetConnectionSharingInfo(ctx, id+1000); err == nil {
		t.Error("expected an error for a missing connection")
	}
}

// TestGetConnectionWithPassword covers decryption and each failure path.
func TestGetConnectionWithPassword(t *testing.T) {
	ds, pool, cleanup := newConnUpdatePasswordTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	encrypted, err := crypto.EncryptPassword("s3cret",
		connUpdatePasswordTestSecret)
	if err != nil {
		t.Fatalf("EncryptPassword: %v", err)
	}
	garbage := "not-a-ciphertext"

	withPassword := insertTestConnection(t, pool, "192.0.2.1", 5432, "app",
		"u", &encrypted, "disable", false, "")
	noPassword := insertTestConnection(t, pool, "192.0.2.1", 5432, "app",
		"u", nil, "disable", false, "")
	badPassword := insertTestConnection(t, pool, "192.0.2.1", 5432, "app",
		"u", &garbage, "disable", false, "")

	t.Run("decrypts the stored password", func(t *testing.T) {
		conn, password, err := ds.GetConnectionWithPassword(ctx, withPassword)
		if err != nil {
			t.Fatalf("GetConnectionWithPassword: %v", err)
		}
		if conn.ID != withPassword || password != "s3cret" {
			t.Errorf("got id=%d password=%q", conn.ID, password)
		}
	})

	t.Run("no password stored", func(t *testing.T) {
		_, password, err := ds.GetConnectionWithPassword(ctx, noPassword)
		if err != nil || password != "" {
			t.Errorf("got password=%q err=%v, want empty and nil",
				password, err)
		}
	})

	t.Run("missing connection", func(t *testing.T) {
		if _, _, err := ds.GetConnectionWithPassword(ctx,
			withPassword+1000); err == nil {
			t.Error("expected an error for a missing connection")
		}
	})

	t.Run("no server secret", func(t *testing.T) {
		noSecret := NewTestDatastore(pool)
		_, _, err := noSecret.GetConnectionWithPassword(ctx, withPassword)
		if err == nil || !strings.Contains(err.Error(), "server secret") {
			t.Errorf("err = %v, want a server secret error", err)
		}
	})

	t.Run("undecryptable ciphertext", func(t *testing.T) {
		_, _, err := ds.GetConnectionWithPassword(ctx, badPassword)
		if err == nil || !strings.Contains(err.Error(), "decrypt") {
			t.Errorf("err = %v, want a decrypt error", err)
		}
	})
}

// TestUpdateConnectionFullAllFields sets every optional field at once and
// checks each one lands, then covers the missing-row and missing-secret
// errors.
func TestUpdateConnectionFullAllFields(t *testing.T) {
	ds, pool, cleanup := newConnUpdatePasswordTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	id := insertTestConnection(t, pool, "192.0.2.1", 5432, "app", "u", nil,
		"disable", false, "")

	str := func(s string) *string { return &s }
	port := 6543
	shared := true
	monitored := false
	params := ConnectionUpdateParams{
		Name:         str("renamed"),
		Description:  str("updated description"),
		Host:         str("db.example.com"),
		HostAddr:     str("192.0.2.10"),
		Port:         &port,
		DatabaseName: str("other"),
		Username:     str("test_user"),
		Password:     str("new-secret"),
		SSLMode:      str("verify-full"),
		SSLCert:      str("/etc/ssl/client.crt"),
		SSLKey:       str("/etc/ssl/client.key"),
		SSLRootCert:  str("/etc/ssl/root.crt"),
		IsShared:     &shared,
		IsMonitored:  &monitored,
	}

	conn, err := ds.UpdateConnectionFull(ctx, id, params)
	if err != nil {
		t.Fatalf("UpdateConnectionFull: %v", err)
	}
	if conn.Name != "renamed" || conn.Description != "updated description" ||
		conn.Host != "db.example.com" || conn.HostAddr.String != "192.0.2.10" ||
		conn.Port != 6543 || conn.DatabaseName != "other" ||
		conn.Username != "test_user" || conn.SSLMode.String != "verify-full" ||
		conn.SSLCert.String != "/etc/ssl/client.crt" ||
		conn.SSLKey.String != "/etc/ssl/client.key" ||
		conn.SSLRootCert.String != "/etc/ssl/root.crt" ||
		!conn.IsShared || conn.IsMonitored {
		t.Errorf("update did not apply every field: %+v", conn)
	}
	_, password, err := ds.GetConnectionWithPassword(ctx, id)
	if err != nil || password != "new-secret" {
		t.Errorf("password = %q, err = %v, want new-secret", password, err)
	}

	if _, err := ds.UpdateConnectionFull(ctx, id+1000,
		ConnectionUpdateParams{Name: str("x")}); err == nil {
		t.Error("expected an error for a missing connection")
	}

	noSecret := NewTestDatastore(pool)
	_, err = noSecret.UpdateConnectionFull(ctx, id,
		ConnectionUpdateParams{Password: str("p")})
	if err == nil || !strings.Contains(err.Error(), "server secret") {
		t.Errorf("err = %v, want a server secret error", err)
	}
}

// TestListDatabases lists the databases on the test server through a
// connection row that points back at it, and covers the failure paths.
func TestListDatabases(t *testing.T) {
	ds, pool, cleanup := newConnUpdatePasswordTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	cfg := pool.Config().ConnConfig
	var encrypted *string
	if cfg.Password != "" {
		value, err := crypto.EncryptPassword(cfg.Password,
			connUpdatePasswordTestSecret)
		if err != nil {
			t.Fatalf("EncryptPassword: %v", err)
		}
		encrypted = &value
	}

	t.Run("lists the server's databases", func(t *testing.T) {
		id := insertTestConnection(t, pool, cfg.Host, int(cfg.Port),
			cfg.Database, cfg.User, encrypted, "disable", false, "")
		databases, err := ds.ListDatabases(ctx, id)
		if err != nil {
			t.Fatalf("ListDatabases: %v", err)
		}
		found := false
		for _, db := range databases {
			if db.Name == "template1" || db.Name == "template0" {
				t.Errorf("template database %q listed", db.Name)
			}
			if db.Name == cfg.Database {
				found = true
				if db.Owner == "" || db.Encoding == "" || db.Size == "" {
					t.Errorf("incomplete entry: %+v", db)
				}
			}
		}
		if !found {
			t.Errorf("database %q not listed in %+v", cfg.Database,
				databases)
		}
	})

	t.Run("missing connection", func(t *testing.T) {
		if _, err := ds.ListDatabases(ctx, 1_000_000); err == nil {
			t.Error("expected an error for a missing connection")
		}
	})

	t.Run("unparseable connection settings", func(t *testing.T) {
		id := insertTestConnection(t, pool, cfg.Host, int(cfg.Port),
			cfg.Database, cfg.User, encrypted, "bogus", false, "")
		_, err := ds.ListDatabases(ctx, id)
		if err == nil || !strings.Contains(err.Error(), "connect") {
			t.Errorf("err = %v, want a connect error", err)
		}
	})

	t.Run("unreachable server", func(t *testing.T) {
		// Port 1 on loopback has no listener, so the query fails.
		id := insertTestConnection(t, pool, "127.0.0.1", 1, "postgres",
			cfg.User, nil, "disable", false, "")
		_, err := ds.ListDatabases(ctx, id)
		if err == nil || !strings.Contains(err.Error(), "query databases") {
			t.Errorf("err = %v, want a query error", err)
		}
	})
}

// TestNewDatastoreConfigErrors covers the configuration checks that run
// before, and the connection check that runs after, the pool is built.
func TestNewDatastoreConfigErrors(t *testing.T) {
	if _, err := NewDatastore(nil, "s"); err == nil {
		t.Error("expected an error for a nil configuration")
	}

	cfg := testDatastoreConfig(t)

	bad := *cfg
	bad.SSLMode = "bogus"
	if _, err := NewDatastore(&bad, "s"); err == nil ||
		!strings.Contains(err.Error(), "parse") {
		t.Errorf("err = %v, want a parse error", err)
	}

	bad = *cfg
	bad.PoolMaxConnIdleTime = "soon"
	if _, err := NewDatastore(&bad, "s"); err == nil ||
		!strings.Contains(err.Error(), "pool_max_conn_idle_time") {
		t.Errorf("err = %v, want an idle time error", err)
	}

	bad = *cfg
	bad.Host = "127.0.0.1"
	bad.Port = 1
	if _, err := NewDatastore(&bad, "s"); err == nil ||
		!strings.Contains(err.Error(), "connect to datastore") {
		t.Errorf("err = %v, want a connection error", err)
	}

	good := *cfg
	good.PoolMaxConnIdleTime = "5m"
	ds, err := NewDatastore(&good, "s")
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	defer ds.Close()
	if got := ds.GetPool().Config().MaxConnIdleTime.String(); got != "5m0s" {
		t.Errorf("MaxConnIdleTime = %s, want 5m0s", got)
	}
}
