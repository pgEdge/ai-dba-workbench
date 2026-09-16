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
)

// The v12 migration adds Telegram as a notification channel type, as
// described in GitHub issue #475. It adds the two columns a Telegram
// channel needs and widens the channel_type CHECK constraint to accept
// 'telegram'. These tests cover the fresh-install path, where the v1
// migration already creates the table in its widened form, and the
// upgrade path, where an install carrying the original inline-named
// constraint and neither column is brought forward.

// telegramColumns are the columns migration 12 adds.
var telegramColumns = []string{
	"telegram_bot_token_encrypted",
	"telegram_chat_id",
}

// migrationV12 returns the registered version 12 migration.
func migrationV12(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 12 {
			return m
		}
	}
	t.Fatal("migration version 12 is not registered")
	return Migration{}
}

// TestMigrationV12_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade. LatestVersion is
// asserted to be at least 12 rather than exactly 12 because later
// migrations are expected to follow.
func TestMigrationV12_Registered(t *testing.T) {
	sm := NewSchemaManager()
	if got := sm.LatestVersion(); got < 12 {
		t.Errorf("LatestVersion() = %d, want at least 12", got)
	}
	if desc := migrationV12(t).Description; desc == "" {
		t.Error("migration 12 has an empty description")
	}
}

// assertTelegramColumns checks both columns exist, are TEXT and carry a
// COMMENT, which CLAUDE.md requires of every object a migration creates.
func assertTelegramColumns(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	for _, column := range telegramColumns {
		var dataType string
		err := pool.QueryRow(ctx, `
			SELECT data_type
			FROM information_schema.columns
			WHERE table_name = 'notification_channels'
			  AND column_name = $1
		`, column).Scan(&dataType)
		if err != nil {
			t.Errorf("column notification_channels.%s is missing: %v", column, err)
			continue
		}
		if dataType != "text" {
			t.Errorf("notification_channels.%s is %s, want text", column, dataType)
		}

		var comment *string
		err = pool.QueryRow(ctx, `
			SELECT col_description(
				'notification_channels'::regclass, a.attnum)
			FROM pg_attribute a
			WHERE a.attrelid = 'notification_channels'::regclass
			  AND a.attname = $1
		`, column).Scan(&comment)
		if err != nil {
			t.Errorf("failed to read comment on %s: %v", column, err)
			continue
		}
		if comment == nil || *comment == "" {
			t.Errorf("notification_channels.%s has no COMMENT", column)
		}
	}

	var tableComment *string
	err := pool.QueryRow(ctx, `
		SELECT obj_description('notification_channels'::regclass, 'pg_class')
	`).Scan(&tableComment)
	if err != nil {
		t.Fatalf("failed to read the table comment: %v", err)
	}
	if tableComment == nil || !strings.Contains(*tableComment, "Telegram") {
		t.Errorf("notification_channels table comment does not mention Telegram: %v",
			tableComment)
	}
}

// assertChannelTypeCheck inserts one row per accepted channel type and
// one row with a type that must be refused, so the CHECK is exercised
// rather than merely read out of the catalog.
func assertChannelTypeCheck(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	accepted := []string{"slack", "mattermost", "telegram", "webhook", "email"}
	for _, channelType := range accepted {
		_, err := pool.Exec(ctx, `
			INSERT INTO notification_channels (channel_type, name, owner_username)
			VALUES ($1, $2, 'v12-test-owner')
		`, channelType, "v12-"+channelType)
		if err != nil {
			t.Errorf("channel_type %q was rejected: %v", channelType, err)
		}
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO notification_channels (channel_type, name, owner_username)
		VALUES ('carrier_pigeon', 'v12-invalid', 'v12-test-owner')
	`)
	if err == nil {
		t.Error("an unknown channel_type was accepted; the CHECK is too wide")
	}

	// Leave the table as it was found.
	if _, err := pool.Exec(ctx, `
		DELETE FROM notification_channels WHERE owner_username = 'v12-test-owner'
	`); err != nil {
		t.Fatalf("failed to clean up seeded channels: %v", err)
	}
}

// TestMigrationV12_FreshInstall verifies that a freshly migrated
// datastore carries both columns and the widened constraint.
func TestMigrationV12_FreshInstall(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	assertTelegramColumns(t, pool)
	assertChannelTypeCheck(t, pool)

	// The widened constraint must be present under its explicit name, so
	// a later migration can find it without guessing.
	ctx := context.Background()
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_constraint
		WHERE conname = 'chk_notification_channels_channel_type'
		  AND conrelid = 'notification_channels'::regclass
	`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to read pg_constraint: %v", err)
	}
	if count != 1 {
		t.Errorf("chk_notification_channels_channel_type present %d times, want 1",
			count)
	}
}

// TestMigrationV12_UpgradesLegacyInstall rewinds a migrated datastore to
// look like one built before Telegram support - neither column, and the
// original auto-named inline CHECK - and re-runs the migration over it.
func TestMigrationV12_UpgradesLegacyInstall(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Rewind to the pre-Telegram shape.
	_, err := pool.Exec(ctx, `
		ALTER TABLE notification_channels
			DROP COLUMN telegram_bot_token_encrypted;
		ALTER TABLE notification_channels
			DROP COLUMN telegram_chat_id;
		ALTER TABLE notification_channels
			DROP CONSTRAINT chk_notification_channels_channel_type;
		ALTER TABLE notification_channels
			ADD CONSTRAINT notification_channels_channel_type_check
			CHECK (channel_type IN ('slack', 'mattermost', 'webhook', 'email'));
		COMMENT ON TABLE notification_channels IS
			'Notification channels for delivering alerts (Slack, Mattermost, webhook, email)';
	`)
	if err != nil {
		t.Fatalf("failed to rewind notification_channels: %v", err)
	}

	// Seed a channel of an existing type so the re-added constraint is
	// validated against real data.
	_, err = pool.Exec(ctx, `
		INSERT INTO notification_channels (channel_type, name, owner_username)
		VALUES ('slack', 'v12-legacy', 'v12-legacy-owner')
	`)
	if err != nil {
		t.Fatalf("failed to seed a legacy channel: %v", err)
	}

	// A Telegram channel must be refused before the migration runs.
	_, err = pool.Exec(ctx, `
		INSERT INTO notification_channels (channel_type, name, owner_username)
		VALUES ('telegram', 'v12-too-early', 'v12-legacy-owner')
	`)
	if err == nil {
		t.Fatal("the rewound constraint accepted 'telegram'; the test setup is wrong")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil &&
			!strings.Contains(err.Error(), "closed") {
			t.Logf("rollback failed: %v", err)
		}
	}()

	if err := migrationV12(t).Up(tx); err != nil {
		t.Fatalf("migration 12 failed on a legacy install: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit migration 12: %v", err)
	}

	assertTelegramColumns(t, pool)

	// The pre-existing row survives the constraint swap.
	var surviving int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM notification_channels
		WHERE owner_username = 'v12-legacy-owner'
	`).Scan(&surviving)
	if err != nil {
		t.Fatalf("failed to count legacy channels: %v", err)
	}
	if surviving != 1 {
		t.Errorf("legacy channel count = %d, want 1", surviving)
	}

	// The old auto-named constraint is gone, so it cannot reject a
	// Telegram channel behind the new one's back.
	var oldCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_constraint
		WHERE conname = 'notification_channels_channel_type_check'
		  AND conrelid = 'notification_channels'::regclass
	`).Scan(&oldCount)
	if err != nil {
		t.Fatalf("failed to read pg_constraint: %v", err)
	}
	if oldCount != 0 {
		t.Errorf("the original constraint is still present %d times, want 0", oldCount)
	}

	if _, err := pool.Exec(ctx, `
		DELETE FROM notification_channels WHERE owner_username = 'v12-legacy-owner'
	`); err != nil {
		t.Fatalf("failed to clean up the legacy channel: %v", err)
	}

	assertChannelTypeCheck(t, pool)
}

// TestMigrationV12_IsIdempotent re-runs the migration over an install
// that already has it applied. Every statement is guarded, so a second
// run must be a no-op rather than an error.
func TestMigrationV12_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	for i := 0; i < 2; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("failed to begin transaction: %v", err)
		}
		if err := migrationV12(t).Up(tx); err != nil {
			_ = tx.Rollback(ctx) //nolint:errcheck
			t.Fatalf("re-run %d of migration 12 failed: %v", i+1, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("failed to commit re-run %d: %v", i+1, err)
		}
	}

	assertTelegramColumns(t, pool)
	assertChannelTypeCheck(t, pool)
}

// TestMigrationV12_ReportsStatementFailure drives the migration's error
// path by renaming the table it alters inside a transaction that is
// rolled back afterwards, so a failing statement is reported with
// context rather than swallowed.
func TestMigrationV12_ReportsStatementFailure(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback failed: %v", err)
		}
	}()

	if _, err := tx.Exec(ctx,
		`ALTER TABLE notification_channels RENAME TO notification_channels_hidden`); err != nil {
		t.Fatalf("failed to hide notification_channels: %v", err)
	}

	err = migrationV12(t).Up(tx)
	if err == nil {
		t.Fatal("migration 12 succeeded without a notification_channels table")
	}
	if !strings.Contains(err.Error(), "Telegram notification channel support") {
		t.Errorf("migration 12 error = %q, want it to name the failure", err.Error())
	}
}
