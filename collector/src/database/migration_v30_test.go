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

	"github.com/jackc/pgx/v5"
)

// Migration 30 requires a connection on server-scope blackouts and
// blackout schedules, rewriting a server row without one to estate as
// the alerter already treats it (issue #732).

var blackoutScopeTables = []string{"blackouts", "blackout_schedules"}

// migrationV30 returns the registered version 30 migration.
func migrationV30(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 30 {
			return m
		}
	}
	t.Fatal("migration version 30 is not registered")
	return Migration{}
}

// insertBlackoutRow inserts a row into a blackout table inside a
// savepoint and reports whether the scope check accepted it.
func insertBlackoutRow(t *testing.T, tx pgx.Tx, table, scope string,
	groupID, clusterID, connectionID *int) (int64, bool) {

	t.Helper()
	ctx := context.Background()
	sql := map[string]string{
		"blackouts": `INSERT INTO blackouts
			(scope, group_id, cluster_id, connection_id, reason, start_time, end_time, created_by)
			VALUES ($1, $2, $3, $4, 'v30', NOW(), NOW() + INTERVAL '1 hour', 'v30')
			RETURNING id`,
		"blackout_schedules": `INSERT INTO blackout_schedules
			(scope, group_id, cluster_id, connection_id, name, cron_expression, duration_minutes, reason, created_by)
			VALUES ($1, $2, $3, $4, 'v30', '0 * * * *', 30, 'v30', 'v30')
			RETURNING id`,
	}[table]
	if _, err := tx.Exec(ctx, "SAVEPOINT v30_insert"); err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	var id int64
	if err := tx.QueryRow(ctx, sql, scope, groupID, clusterID, connectionID).Scan(&id); err != nil {
		if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT v30_insert"); rbErr != nil {
			t.Fatalf("rollback to savepoint: %v", rbErr)
		}
		return 0, false
	}
	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT v30_insert"); err != nil {
		t.Fatalf("release savepoint: %v", err)
	}
	return id, true
}

// seedV30Targets inserts a group, a cluster and a connection for
// blackout rows to name.
func seedV30Targets(t *testing.T, tx pgx.Tx) (groupID, clusterID, connectionID int) {
	t.Helper()
	ctx := context.Background()
	if err := tx.QueryRow(ctx, `INSERT INTO cluster_groups (name) VALUES ('v30 group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatalf("insert group: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO clusters (group_id, name) VALUES ($1, 'v30 cluster') RETURNING id`,
		groupID).Scan(&clusterID); err != nil {
		t.Fatalf("insert cluster: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, password_encrypted, owner_username)
		VALUES ('v30-test', '192.0.2.1', 5432, 'postgres', 'postgres', '', 'v30-test-owner')
		RETURNING id`).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	return groupID, clusterID, connectionID
}

// assertV30Constraint checks that each table's scope check rejects a
// server row without a connection, or with another ID, accepts every
// well-formed row, and is commented.
func assertV30Constraint(t *testing.T, tx pgx.Tx, groupID, clusterID, connectionID int) {
	t.Helper()
	for _, table := range blackoutScopeTables {
		cases := []struct {
			name                string
			scope               string
			group, cluster, con *int
			ok                  bool
		}{
			{"estate", "estate", nil, nil, nil, true},
			{"group", "group", &groupID, nil, nil, true},
			{"cluster", "cluster", nil, &clusterID, nil, true},
			{"server", "server", nil, nil, &connectionID, true},
			{"server without a connection", "server", nil, nil, nil, false},
			{"server with a group", "server", &groupID, nil, &connectionID, false},
			{"server with a cluster", "server", nil, &clusterID, &connectionID, false},
		}
		for _, tc := range cases {
			if _, ok := insertBlackoutRow(t, tx, table, tc.scope, tc.group, tc.cluster, tc.con); ok != tc.ok {
				t.Errorf("%s %s: accepted %v, want %v", table, tc.name, ok, tc.ok)
			}
		}
		var constraintComment, columnComment string
		if err := tx.QueryRow(context.Background(), `
			SELECT
			    COALESCE((SELECT obj_description(oid, 'pg_constraint') FROM pg_constraint
			              WHERE conname = $1 || '_scope_ids_check'
			                AND conrelid = $1::regclass), ''),
			    COALESCE(col_description($1::regclass,
			        (SELECT attnum FROM pg_attribute
			         WHERE attrelid = $1::regclass AND attname = 'connection_id')), '')
		`, table).Scan(&constraintComment, &columnComment); err != nil {
			t.Fatalf("%s: read comments: %v", table, err)
		}
		if constraintComment == "" || columnComment == "" {
			t.Errorf("%s: constraint comment %q, connection_id comment %q, want both set",
				table, constraintComment, columnComment)
		}
	}
}

// restoreV29Check puts back the check migration 30 replaces, which let a
// server row name any IDs or none.
func restoreV29Check(t *testing.T, tx pgx.Tx) {
	t.Helper()
	ctx := context.Background()
	if _, err := tx.Exec(ctx, `
		ALTER TABLE blackouts DROP CONSTRAINT blackouts_scope_ids_check;
		ALTER TABLE blackouts ADD CONSTRAINT blackouts_scope_ids_check CHECK (
			(scope = 'estate' AND connection_id IS NULL AND group_id IS NULL AND cluster_id IS NULL)
			OR (scope = 'group' AND group_id IS NOT NULL AND connection_id IS NULL AND cluster_id IS NULL)
			OR (scope = 'cluster' AND cluster_id IS NOT NULL AND connection_id IS NULL AND group_id IS NULL)
			OR (scope = 'server'));
		ALTER TABLE blackout_schedules DROP CONSTRAINT blackout_schedules_scope_ids_check;
		ALTER TABLE blackout_schedules ADD CONSTRAINT blackout_schedules_scope_ids_check CHECK (
			(scope = 'estate' AND connection_id IS NULL AND group_id IS NULL AND cluster_id IS NULL)
			OR (scope = 'group' AND group_id IS NOT NULL AND connection_id IS NULL AND cluster_id IS NULL)
			OR (scope = 'cluster' AND cluster_id IS NOT NULL AND connection_id IS NULL AND group_id IS NULL)
			OR (scope = 'server'));
	`); err != nil {
		t.Fatalf("failed to restore the earlier check: %v", err)
	}
}

// TestMigrationV30_Registered verifies the migration is wired into the
// schema manager with a description.
func TestMigrationV30_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 30 {
		t.Errorf("LatestVersion() = %d, want at least 30", got)
	}
	if desc := migrationV30(t).Description; desc == "" {
		t.Error("migration 30 has an empty description")
	}
}

// TestMigrationV30_FreshInstall verifies a freshly migrated datastore
// refuses a server-scope row without a connection.
func TestMigrationV30_FreshInstall(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	groupID, clusterID, connectionID := seedV30Targets(t, tx)
	assertV30Constraint(t, tx, groupID, clusterID, connectionID)
}

// TestMigrationV30_UpgradeAndRerun restores the earlier, looser check,
// stores the server rows it allowed, runs the migration as an upgrade
// would, and runs it again to prove it is idempotent.
func TestMigrationV30_UpgradeAndRerun(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	ctx := context.Background()
	groupID, clusterID, connectionID := seedV30Targets(t, tx)
	restoreV29Check(t, tx)

	// Each legacy row and the scope and IDs the alerter already treats it
	// as having.
	type legacy struct {
		name                string
		group, cluster, con *int
		wantScope           string
		wantConnection      *int
	}
	rows := []legacy{
		{"no connection", nil, nil, nil, "estate", nil},
		{"group, no connection", &groupID, nil, nil, "estate", nil},
		{"cluster, no connection", nil, &clusterID, nil, "estate", nil},
		{"connection and group", &groupID, nil, &connectionID, "server", &connectionID},
		{"connection and cluster", nil, &clusterID, &connectionID, "server", &connectionID},
		{"connection only", nil, nil, &connectionID, "server", &connectionID},
	}
	ids := map[string]map[string]int64{}
	for _, table := range blackoutScopeTables {
		ids[table] = map[string]int64{}
		for _, row := range rows {
			id, ok := insertBlackoutRow(t, tx, table, "server", row.group, row.cluster, row.con)
			if !ok {
				t.Fatalf("%s %s: the earlier check refused a legacy row", table, row.name)
			}
			ids[table][row.name] = id
		}
	}

	readBack := map[string]string{
		"blackouts":          `SELECT scope, group_id, cluster_id, connection_id FROM blackouts WHERE id = $1`,
		"blackout_schedules": `SELECT scope, group_id, cluster_id, connection_id FROM blackout_schedules WHERE id = $1`,
	}
	for run := 1; run <= 2; run++ {
		if err := migrationV30(t).Up(tx); err != nil {
			t.Fatalf("migration 30 run %d failed: %v", run, err)
		}
		for _, table := range blackoutScopeTables {
			for _, row := range rows {
				var scope string
				var group, cluster, con *int
				if err := tx.QueryRow(ctx, readBack[table], ids[table][row.name]).Scan(&scope, &group, &cluster, &con); err != nil {
					t.Fatalf("%s %s: read back: %v", table, row.name, err)
				}
				if scope != row.wantScope || group != nil || cluster != nil ||
					(con == nil) != (row.wantConnection == nil) ||
					(con != nil && *con != *row.wantConnection) {
					t.Errorf("run %d %s %s: got scope %s group %v cluster %v connection %v, want %s connection %v",
						run, table, row.name, scope, group, cluster, con, row.wantScope, row.wantConnection)
				}
			}
		}
	}
	assertV30Constraint(t, tx, groupID, clusterID, connectionID)
}

// TestMigrationV30_DatabaseSpecificRows verifies that a server row with
// no connection but a database_name, which matched that database on
// every connection, stops suppressing anything before it becomes
// estate, which ignores database_name.
func TestMigrationV30_DatabaseSpecificRows(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	ctx := context.Background()
	_, _, connectionID := seedV30Targets(t, tx)
	restoreV29Check(t, tx)

	insertBlackout := func(connection *int, start, end string) int64 {
		t.Helper()
		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO blackouts
				(scope, connection_id, database_name, reason, start_time, end_time, created_by)
			VALUES ('server', $1, 'appdb', 'v30', NOW() + $2::interval, NOW() + $3::interval, 'v30')
			RETURNING id`, connection, start, end).Scan(&id); err != nil {
			t.Fatalf("insert blackout: %v", err)
		}
		return id
	}
	past := insertBlackout(nil, "-2 hours", "-1 hour")
	active := insertBlackout(nil, "-1 hour", "1 hour")
	future := insertBlackout(nil, "1 hour", "2 hours")
	server := insertBlackout(&connectionID, "-1 hour", "1 hour")

	var schedule, serverSchedule int64
	for _, s := range []struct {
		connection *int
		id         *int64
	}{{nil, &schedule}, {&connectionID, &serverSchedule}} {
		if err := tx.QueryRow(ctx, `
			INSERT INTO blackout_schedules
				(scope, connection_id, database_name, name, cron_expression, duration_minutes, reason, created_by)
			VALUES ('server', $1, 'appdb', 'v30', '0 * * * *', 30, 'v30', 'v30')
			RETURNING id`, s.connection).Scan(s.id); err != nil {
			t.Fatalf("insert schedule: %v", err)
		}
	}

	if err := migrationV30(t).Up(tx); err != nil {
		t.Fatalf("migration 30 failed: %v", err)
	}

	var gone bool
	if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM blackouts WHERE id = $1)`,
		future).Scan(&gone); err != nil || !gone {
		t.Errorf("future database-specific blackout: deleted %v (err %v), want deleted", gone, err)
	}
	for _, b := range []struct {
		name      string
		id        int64
		wantScope string
		wantEnded string
	}{
		{"past", past, "estate", "NOW() - INTERVAL '1 hour'"},
		{"active", active, "estate", "NOW()"},
		{"connection", server, "server", "NOW() + INTERVAL '1 hour'"},
	} {
		var scope string
		var endMatches bool
		if err := tx.QueryRow(ctx, `SELECT scope, end_time = `+b.wantEnded+` FROM blackouts WHERE id = $1`,
			b.id).Scan(&scope, &endMatches); err != nil {
			t.Fatalf("%s blackout: read back: %v", b.name, err)
		}
		if scope != b.wantScope || !endMatches {
			t.Errorf("%s blackout: scope %s, end_time = %s %v; want scope %s and true",
				b.name, scope, b.wantEnded, endMatches, b.wantScope)
		}
	}
	for _, s := range []struct {
		name        string
		id          int64
		wantScope   string
		wantEnabled bool
	}{
		{"database-specific", schedule, "estate", false},
		{"connection", serverSchedule, "server", true},
	} {
		var scope string
		var enabled bool
		if err := tx.QueryRow(ctx, `SELECT scope, enabled FROM blackout_schedules WHERE id = $1`,
			s.id).Scan(&scope, &enabled); err != nil {
			t.Fatalf("%s schedule: read back: %v", s.name, err)
		}
		if scope != s.wantScope || enabled != s.wantEnabled {
			t.Errorf("%s schedule: scope %s enabled %v, want %s %v",
				s.name, scope, enabled, s.wantScope, s.wantEnabled)
		}
	}
}

// TestMigrationV30_ReportsFailure verifies a failed statement is
// returned with context.
func TestMigrationV30_ReportsFailure(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	if _, err := tx.Exec(context.Background(), `
		ALTER TABLE blackouts RENAME TO blackouts_moved;
	`); err != nil {
		t.Fatalf("failed to move blackouts aside: %v", err)
	}
	err := migrationV30(t).Up(tx)
	if err == nil {
		t.Fatal("migration 30 succeeded without a blackouts table")
	}
	if !strings.Contains(err.Error(), "server-scope blackouts") {
		t.Errorf("migration 30 error = %q, want it to name the change", err.Error())
	}
}
