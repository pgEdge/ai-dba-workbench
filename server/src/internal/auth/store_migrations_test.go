/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"strings"
	"testing"
	"time"
)

// execAll runs each statement against the store, failing the test on
// the first error.
func execAll(t *testing.T, store *AuthStore, stmts ...string) {
	t.Helper()
	for _, stmt := range stmts {
		if _, err := store.db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

// setSchemaVersion records version as the store's schema version.
func setSchemaVersion(t *testing.T, store *AuthStore, version int) {
	t.Helper()
	if _, err := store.db.Exec("DELETE FROM schema_version"); err != nil {
		t.Fatalf("clearing schema_version: %v", err)
	}
	if _, err := store.db.Exec(
		"INSERT INTO schema_version (version) VALUES (?)", version); err != nil {
		t.Fatalf("setting schema_version: %v", err)
	}
}

// newTestStoreAtVersion2 returns a store whose MCP privilege tables have
// the version 2 shape: the foreign key on privilege_identifier_id that
// the wildcard sentinel (0) cannot satisfy.
func newTestStoreAtVersion2(t *testing.T) *AuthStore {
	t.Helper()
	store := newTestStoreAtVersion(t, 3)
	execAll(t, store,
		`DROP TABLE group_mcp_privileges`,
		`CREATE TABLE group_mcp_privileges (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL
                REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(group_id, privilege_identifier_id)
        )`,
		`DROP TABLE token_mcp_scope`,
		`CREATE TABLE token_mcp_scope (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            token_id INTEGER NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL
                REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(token_id, privilege_identifier_id)
        )`,
	)
	setSchemaVersion(t, store, 2)
	return store
}

// newTestStoreAtVersion1 goes one step further back, removing the
// is_public column that version 2 added to mcp_privilege_identifiers.
// SQLite cannot drop the column in place, so the table is rebuilt.
func newTestStoreAtVersion1(t *testing.T) *AuthStore {
	t.Helper()
	store := newTestStoreAtVersion2(t)
	execAll(t, store,
		`CREATE TABLE mcp_privilege_identifiers_v1 (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            identifier TEXT UNIQUE NOT NULL,
            item_type TEXT NOT NULL CHECK (item_type IN ('tool', 'resource', 'prompt')),
            description TEXT DEFAULT '',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )`,
		`INSERT INTO mcp_privilege_identifiers_v1 (id, identifier, item_type, description, created_at)
         SELECT id, identifier, item_type, description, created_at
         FROM mcp_privilege_identifiers`,
		`DROP TABLE mcp_privilege_identifiers`,
		`ALTER TABLE mcp_privilege_identifiers_v1 RENAME TO mcp_privilege_identifiers`,
	)
	setSchemaVersion(t, store, 1)
	return store
}

// TestInitSchemaUpgradesAVersion1Store runs the version 1 to 2 and
// version 2 to 3 migrations through the dispatcher and checks what each
// was for: the is_public column exists, and a wildcard grant, which the
// old foreign key refused, can now be written.
func TestInitSchemaUpgradesAVersion1Store(t *testing.T) {
	store := newTestStoreAtVersion1(t)

	if err := store.initSchema(); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	var version int
	if err := store.db.QueryRow(
		"SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("schema_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}

	var columns int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('mcp_privilege_identifiers') WHERE name = 'is_public'",
	).Scan(&columns); err != nil {
		t.Fatalf("reading columns: %v", err)
	}
	if columns != 1 {
		t.Fatalf("is_public columns = %d, want 1", columns)
	}

	execAll(t, store, `INSERT INTO user_groups (name) VALUES ('wildcard-group')`)
	if _, err := store.db.Exec(
		`INSERT INTO group_mcp_privileges (group_id, privilege_identifier_id)
         SELECT id, 0 FROM user_groups WHERE name = 'wildcard-group'`); err != nil {
		t.Fatalf("wildcard grant after the migration: %v", err)
	}

	// A migration that added the column but failed before recording
	// its version runs again at the next start, and must tolerate the
	// column already being there.
	if err := store.migrateV1ToV2(); err != nil {
		t.Fatalf("second migrateV1ToV2: %v", err)
	}
}

// TestMigrateV1ToV2ReportsFailures checks that each statement's failure
// is reported rather than swallowed.
func TestMigrateV1ToV2ReportsFailures(t *testing.T) {
	cases := map[string]struct {
		sabotage string
		wantErr  string
	}{
		"privilege table missing": {
			sabotage: "DROP TABLE mcp_privilege_identifiers",
			wantErr:  "failed to add is_public column",
		},
		"schema_version table missing": {
			sabotage: "DROP TABLE schema_version",
			wantErr:  "failed to clear schema version",
		},
		"schema_version refuses the new version": {
			sabotage: `DROP TABLE schema_version;
				CREATE TABLE schema_version (version INTEGER PRIMARY KEY CHECK (version < 2))`,
			wantErr: "failed to set schema version",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := newTestStoreAtVersion1(t)
			execAll(t, store, tc.sabotage)
			err := store.migrateV1ToV2()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestMigrateV2ToV3ReportsFailures does the same for the table rebuild,
// which runs in one transaction.
func TestMigrateV2ToV3ReportsFailures(t *testing.T) {
	cases := map[string]struct {
		sabotage string
		wantErr  string
	}{
		"group privileges missing": {
			sabotage: "DROP TABLE group_mcp_privileges",
			wantErr:  "failed to rebuild group_mcp_privileges",
		},
		"token scope missing": {
			sabotage: "DROP TABLE token_mcp_scope",
			wantErr:  "failed to rebuild token_mcp_scope",
		},
		"schema_version table missing": {
			sabotage: "DROP TABLE schema_version",
			wantErr:  "failed to clear schema version",
		},
		"schema_version refuses the new version": {
			sabotage: `DROP TABLE schema_version;
				CREATE TABLE schema_version (version INTEGER PRIMARY KEY CHECK (version < 3))`,
			wantErr: "failed to set schema version",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := newTestStoreAtVersion2(t)
			execAll(t, store, tc.sabotage)
			err := store.migrateV2ToV3()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}

	t.Run("closed database", func(t *testing.T) {
		store := newTestStoreAtVersion2(t)
		if err := store.db.Close(); err != nil {
			t.Fatalf("closing the database: %v", err)
		}
		err := store.migrateV2ToV3()
		if err == nil || !strings.Contains(err.Error(), "failed to disable foreign_keys") {
			t.Fatalf("err = %v, want a foreign_keys failure", err)
		}
	})
}

// TestInitSchemaStopsOnAnEarlyMigrationFailure checks that the
// dispatcher reports a failed version 1 or version 2 migration, so the
// server does not start against a schema it cannot use.
func TestInitSchemaStopsOnAnEarlyMigrationFailure(t *testing.T) {
	t.Run("version 1", func(t *testing.T) {
		store := newTestStoreAtVersion1(t)
		execAll(t, store, "DROP TABLE mcp_privilege_identifiers")
		err := store.initSchema()
		if err == nil || !strings.Contains(err.Error(), "migrate from v1 to v2") {
			t.Fatalf("err = %v, want a v1 to v2 failure", err)
		}
	})
	t.Run("version 2", func(t *testing.T) {
		store := newTestStoreAtVersion2(t)
		execAll(t, store, "DROP TABLE token_mcp_scope")
		err := store.initSchema()
		if err == nil || !strings.Contains(err.Error(), "migrate from v2 to v3") {
			t.Fatalf("err = %v, want a v2 to v3 failure", err)
		}
	})
}

// TestSessionCleanupSweepsExpiredSessions checks that the background
// sweep removes expired sessions, keeps live ones and ignores entries
// that are not sessions, that a second start is a no-op, and that the
// stop is safe to repeat.
func TestSessionCleanupSweepsExpiredSessions(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.sessions.Store("expired", &SessionInfo{
		Username: "old", ExpiresAt: time.Now().Add(-time.Minute)})
	store.sessions.Store("live", &SessionInfo{
		Username: "new", ExpiresAt: time.Now().Add(time.Hour)})
	store.sessions.Store("not-a-session", "garbage")

	store.StartSessionCleanup(time.Millisecond)
	store.mu.Lock()
	first := store.sessionCleanupStop
	store.mu.Unlock()
	store.StartSessionCleanup(time.Millisecond)
	store.mu.Lock()
	second := store.sessionCleanupStop
	store.mu.Unlock()
	if first == nil || first != second {
		t.Fatal("a second StartSessionCleanup replaced the running sweep")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := store.sessions.Load("expired"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sweep did not remove the expired session")
		}
		time.Sleep(time.Millisecond)
	}

	store.StopSessionCleanup()
	store.StopSessionCleanup()

	if _, ok := store.sessions.Load("live"); !ok {
		t.Error("the sweep removed a live session")
	}
	if _, ok := store.sessions.Load("not-a-session"); !ok {
		t.Error("the sweep removed an entry that is not a session")
	}
}
