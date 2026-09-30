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
	"slices"
	"strings"
	"testing"
)

// legacyV1Fixture records the rows a v1 database is seeded with, so the
// migration tests can check that each one survives the upgrade.
type legacyV1Fixture struct {
	groupID int64
	privID  int64
	tokenID int64
}

// rewindToV1 rebuilds the MCP privilege tables of a fresh store into the
// shape a version 1 build created: mcp_privilege_identifiers without the
// is_public column, and group_mcp_privileges and token_mcp_scope with a
// foreign key from privilege_identifier_id to mcp_privilege_identifiers,
// which is what the v2 to v3 migration exists to remove. The rows are
// created through the store API first, so the rebuilt tables carry real
// data across the rewind. schema_version is left recording 1.
func rewindToV1(t *testing.T, store *AuthStore) legacyV1Fixture {
	t.Helper()

	var fx legacyV1Fixture
	var err error
	if fx.groupID, err = store.CreateGroup("legacy-group", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if fx.privID, err = store.RegisterMCPPrivilege(
		"legacy_tool", "tool", "a tool from v1", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}
	if err := store.GrantMCPPrivilege(fx.groupID, fx.privID); err != nil {
		t.Fatalf("GrantMCPPrivilege: %v", err)
	}
	if err := store.CreateUser("legacy-owner", "Password12345", "", "",
		"legacy.owner@example.com"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, token, err := store.CreateToken("legacy-owner", "legacy token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	fx.tokenID = token.ID
	if err := store.SetTokenMCPScope(fx.tokenID, []int64{fx.privID}); err != nil {
		t.Fatalf("SetTokenMCPScope: %v", err)
	}

	// The parent table is rebuilt first, whilst nothing references it
	// through a foreign key, so dropping it cannot trip the constraint.
	statements := []string{
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

		`CREATE TABLE group_mcp_privileges_v1 (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL
                REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(group_id, privilege_identifier_id)
        )`,
		`INSERT INTO group_mcp_privileges_v1 (id, group_id, privilege_identifier_id, created_at)
         SELECT id, group_id, privilege_identifier_id, created_at
         FROM group_mcp_privileges`,
		`DROP TABLE group_mcp_privileges`,
		`ALTER TABLE group_mcp_privileges_v1 RENAME TO group_mcp_privileges`,

		`CREATE TABLE token_mcp_scope_v1 (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            token_id INTEGER NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL
                REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(token_id, privilege_identifier_id)
        )`,
		`INSERT INTO token_mcp_scope_v1 (id, token_id, privilege_identifier_id, created_at)
         SELECT id, token_id, privilege_identifier_id, created_at
         FROM token_mcp_scope`,
		`DROP TABLE token_mcp_scope`,
		`ALTER TABLE token_mcp_scope_v1 RENAME TO token_mcp_scope`,

		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (1)`,
	}
	for _, stmt := range statements {
		if _, err := store.db.Exec(stmt); err != nil {
			t.Fatalf("rewinding to v1: %v\nstatement: %s", err, stmt)
		}
	}

	return fx
}

// foreignKeyTargets lists the tables that table's foreign keys point at.
func foreignKeyTargets(t *testing.T, store *AuthStore, table string) []string {
	t.Helper()

	rows, err := store.db.Query(
		`SELECT "table" FROM pragma_foreign_key_list(?)`, table)
	if err != nil {
		t.Fatalf("reading foreign keys of %s: %v", table, err)
	}
	defer rows.Close()

	var targets []string
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			t.Fatalf("scanning foreign key of %s: %v", table, err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating foreign keys of %s: %v", table, err)
	}
	return targets
}

func TestInitSchemaUpgradesAV1Database(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	fx := rewindToV1(t, store)

	// The fixture has to reproduce the defect the v3 migration fixes,
	// or the assertions after the upgrade prove nothing: with the old
	// foreign key in place, a wildcard grant is refused.
	if _, err := store.db.Exec(
		"INSERT INTO group_mcp_privileges (group_id, privilege_identifier_id) VALUES (?, 0)",
		fx.groupID); err == nil {
		t.Fatal("expected the v1 foreign key to refuse a wildcard grant")
	}

	if err := store.initSchema(); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	var version int
	if err := store.db.QueryRow(
		"SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("schema version = %d, want %d", version, schemaVersion)
	}

	// v1 to v2: the privilege survives and reads as not public.
	var isPublic bool
	if err := store.db.QueryRow(
		"SELECT is_public FROM mcp_privilege_identifiers WHERE id = ?",
		fx.privID).Scan(&isPublic); err != nil {
		t.Fatalf("reading is_public: %v", err)
	}
	if isPublic {
		t.Error("a privilege from before is_public existed must default to not public")
	}

	// v2 to v3: both rebuilt tables keep their rows and lose the
	// foreign key to mcp_privilege_identifiers, but keep the one to
	// their owner.
	var groupRows, tokenRows int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM group_mcp_privileges WHERE group_id = ? AND privilege_identifier_id = ?",
		fx.groupID, fx.privID).Scan(&groupRows); err != nil {
		t.Fatalf("counting group_mcp_privileges: %v", err)
	}
	if groupRows != 1 {
		t.Errorf("group_mcp_privileges rows = %d, want 1", groupRows)
	}
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ? AND privilege_identifier_id = ?",
		fx.tokenID, fx.privID).Scan(&tokenRows); err != nil {
		t.Fatalf("counting token_mcp_scope: %v", err)
	}
	if tokenRows != 1 {
		t.Errorf("token_mcp_scope rows = %d, want 1", tokenRows)
	}

	for table, owner := range map[string]string{
		"group_mcp_privileges": "user_groups",
		"token_mcp_scope":      "tokens",
	} {
		targets := foreignKeyTargets(t, store, table)
		if slices.Contains(targets, "mcp_privilege_identifiers") {
			t.Errorf("%s still has a foreign key to mcp_privilege_identifiers", table)
		}
		if !slices.Contains(targets, owner) {
			t.Errorf("%s lost its foreign key to %s; has %v", table, owner, targets)
		}
	}

	// The point of the rebuild: the wildcard grant now goes in with
	// foreign keys enforced, and so does the wildcard token scope.
	if err := store.GrantMCPPrivilegeByName(fx.groupID, "*"); err != nil {
		t.Errorf("wildcard grant after the upgrade: %v", err)
	}
	if _, err := store.db.Exec(
		"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, 0)",
		fx.tokenID); err != nil {
		t.Errorf("wildcard token scope after the upgrade: %v", err)
	}
}

func TestMigrateV1ToV2(t *testing.T) {
	t.Run("column already present from a partial run", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		// A fresh store already has is_public, which is what an
		// interrupted earlier run of this migration leaves behind.
		if err := store.migrateV1ToV2(); err != nil {
			t.Fatalf("migrateV1ToV2 on a partially migrated database: %v", err)
		}
		var version int
		if err := store.db.QueryRow(
			"SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
			t.Fatalf("reading schema_version: %v", err)
		}
		if version != 2 {
			t.Errorf("schema version = %d, want 2", version)
		}
	})

	tests := []struct {
		name    string
		setup   string
		wantErr string
	}{
		{
			name:    "privilege table missing",
			setup:   "DROP TABLE mcp_privilege_identifiers",
			wantErr: "failed to add is_public column",
		},
		{
			name:    "schema_version missing",
			setup:   "DROP TABLE schema_version",
			wantErr: "failed to clear schema version",
		},
		{
			name: "schema_version refuses version 2",
			setup: `DROP TABLE schema_version;
                CREATE TABLE schema_version (
                    version INTEGER PRIMARY KEY CHECK (version < 2)
                );`,
			wantErr: "failed to set schema version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForStore(t)
			defer cleanup()

			if _, err := store.db.Exec(tt.setup); err != nil {
				t.Fatalf("setup: %v", err)
			}
			err := store.migrateV1ToV2()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("migrateV1ToV2 error = %v, want one containing %q",
					err, tt.wantErr)
			}
		})
	}
}

func TestMigrateV2ToV3Failures(t *testing.T) {
	tests := []struct {
		name    string
		setup   string
		wantErr string
	}{
		{
			name:    "group table rebuild fails",
			setup:   "CREATE TABLE group_mcp_privileges_new (id INTEGER)",
			wantErr: "failed to rebuild group_mcp_privileges",
		},
		{
			name:    "token table rebuild fails",
			setup:   "CREATE TABLE token_mcp_scope_new (id INTEGER)",
			wantErr: "failed to rebuild token_mcp_scope",
		},
		{
			name:    "schema_version missing",
			setup:   "DROP TABLE schema_version",
			wantErr: "failed to clear schema version",
		},
		{
			name: "schema_version refuses version 3",
			setup: `DROP TABLE schema_version;
                CREATE TABLE schema_version (
                    version INTEGER PRIMARY KEY CHECK (version < 3)
                );`,
			wantErr: "failed to set schema version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForStore(t)
			defer cleanup()

			groupID, err := store.CreateGroup("rollback-group", "")
			if err != nil {
				t.Fatalf("CreateGroup: %v", err)
			}
			privID, err := store.RegisterMCPPrivilege(
				"rollback_tool", "tool", "", false)
			if err != nil {
				t.Fatalf("RegisterMCPPrivilege: %v", err)
			}
			if err := store.GrantMCPPrivilege(groupID, privID); err != nil {
				t.Fatalf("GrantMCPPrivilege: %v", err)
			}
			if _, err := store.db.Exec(tt.setup); err != nil {
				t.Fatalf("setup: %v", err)
			}

			err = store.migrateV2ToV3()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("migrateV2ToV3 error = %v, want one containing %q",
					err, tt.wantErr)
			}

			// The rebuild runs in one transaction, so a failure at any
			// step must leave the original grant where it was.
			var rows int
			if err := store.db.QueryRow(
				"SELECT COUNT(*) FROM group_mcp_privileges WHERE group_id = ? AND privilege_identifier_id = ?",
				groupID, privID).Scan(&rows); err != nil {
				t.Fatalf("counting group_mcp_privileges after rollback: %v", err)
			}
			if rows != 1 {
				t.Errorf("group_mcp_privileges rows after rollback = %d, want 1", rows)
			}
		})
	}
}

func TestNewAuthStoreReportsLegacyMigrationFailures(t *testing.T) {
	tests := []struct {
		name    string
		version int
		setup   string
		wantErr string
	}{
		{
			name:    "v1 to v2",
			version: 1,
			setup:   "DROP TABLE mcp_privilege_identifiers",
			wantErr: "failed to migrate from v1 to v2",
		},
		{
			name:    "v2 to v3",
			version: 2,
			setup:   "CREATE TABLE group_mcp_privileges_new (id INTEGER)",
			wantErr: "failed to migrate from v2 to v3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForStore(t)
			defer cleanup()

			if _, err := store.db.Exec(tt.setup); err != nil {
				t.Fatalf("setup: %v", err)
			}
			if _, err := store.db.Exec("DELETE FROM schema_version"); err != nil {
				t.Fatalf("clearing schema_version: %v", err)
			}
			if _, err := store.db.Exec(
				"INSERT INTO schema_version (version) VALUES (?)",
				tt.version); err != nil {
				t.Fatalf("setting schema_version: %v", err)
			}

			err := store.initSchema()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("initSchema error = %v, want one containing %q",
					err, tt.wantErr)
			}
		})
	}
}
