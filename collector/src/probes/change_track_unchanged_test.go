/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests that exercise the "data unchanged, skip storage" branch of the
// change-tracking probes. Each probe queries the datastore for the
// most recent row, computes a hash of the input metrics, and skips the
// write when the hashes match. We drive that path explicitly with
// hand-crafted metric maps that round-trip through the datastore
// without coercion artifacts.
package probes

import (
	"context"
	"testing"
	"time"
)

// TestPgSettingsProbe_StoreUnchanged stores a synthetic pg_settings row,
// then stores the same row again and asserts that the second call
// neither errors nor writes a new partition.
func TestPgSettingsProbe_StoreUnchanged(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	p := NewPgSettingsProbe(&ProbeConfig{Name: ProbeNamePgSettings})
	connID := 1234

	// Wipe any previous test data for this synthetic connection so the
	// HasDataChanged query starts from a deterministic baseline.
	if _, err := conn.Exec(ctx,
		"DELETE FROM metrics.pg_settings WHERE connection_id=$1",
		connID); err != nil {
		t.Fatalf("clean: %v", err)
	}

	metrics := []map[string]any{
		{
			"name":            "max_connections",
			"setting":         "100",
			"unit":            nil,
			"category":        "Connections",
			"short_desc":      "max conns",
			"extra_desc":      "",
			"context":         "postmaster",
			"vartype":         "integer",
			"source":          "default",
			"min_val":         "1",
			"max_val":         "262143",
			"enumvals":        nil,
			"boot_val":        "100",
			"reset_val":       "100",
			"sourcefile":      nil,
			"sourceline":      nil,
			"pending_restart": false,
		},
	}

	now := time.Now().UTC()
	// First store writes the row.
	if err := p.Store(ctx, conn, connID, now, metrics); err != nil {
		t.Fatalf("Store first: %v", err)
	}
	// Second store with identical data should skip.
	if err := p.Store(ctx, conn, connID, now.Add(time.Minute),
		metrics); err != nil {
		t.Fatalf("Store unchanged: %v", err)
	}
	// Verify that only one collected_at exists for this connection.
	var distinctTimes int
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(DISTINCT collected_at)
		FROM metrics.pg_settings
		WHERE connection_id=$1
	`, connID).Scan(&distinctTimes); err != nil {
		t.Fatalf("count distinct times: %v", err)
	}
	if distinctTimes != 1 {
		t.Errorf("expected 1 distinct collected_at, got %d",
			distinctTimes)
	}
}

func TestPgServerInfoProbe_StoreUnchanged(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	p := NewPgServerInfoProbe(&ProbeConfig{Name: ProbeNamePgServerInfo})
	connID := 1235

	if _, err := conn.Exec(ctx,
		"DELETE FROM metrics.pg_server_info WHERE connection_id=$1",
		connID); err != nil {
		t.Fatalf("clean: %v", err)
	}

	metrics := []map[string]any{
		{
			"server_version":        "16.1",
			"server_version_num":    160001,
			"system_identifier":     int64(7000000000000000000),
			"cluster_name":          (*string)(nil),
			"data_directory":        "/var/lib/postgresql",
			"max_connections":       100,
			"max_wal_senders":       10,
			"max_replication_slots": 10,
			"installed_extensions":  []string{"plpgsql"},
		},
	}

	now := time.Now().UTC()
	if err := p.Store(ctx, conn, connID, now, metrics); err != nil {
		t.Fatalf("Store first: %v", err)
	}
	if err := p.Store(ctx, conn, connID, now.Add(time.Minute),
		metrics); err != nil {
		t.Fatalf("Store unchanged: %v", err)
	}
	var distinctTimes int
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(DISTINCT collected_at)
		FROM metrics.pg_server_info
		WHERE connection_id=$1
	`, connID).Scan(&distinctTimes); err != nil {
		t.Fatalf("count: %v", err)
	}
	if distinctTimes != 1 {
		t.Errorf("expected 1 row, got %d", distinctTimes)
	}
}

func TestPgHbaFileRulesProbe_StoreUnchanged(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	p := NewPgHbaFileRulesProbe(&ProbeConfig{Name: ProbeNamePgHbaFileRules})
	connID := 1236

	if _, err := conn.Exec(ctx,
		"DELETE FROM metrics.pg_hba_file_rules WHERE connection_id=$1",
		connID); err != nil {
		t.Fatalf("clean: %v", err)
	}

	metrics := []map[string]any{
		{
			"rule_number": int64(1),
			"file_name":   "/etc/postgresql/pg_hba.conf",
			"line_number": int64(10),
			"type":        "host",
			"database":    []string{"all"},
			"user_name":   []string{"all"},
			"address":     "127.0.0.1/32",
			"netmask":     (*string)(nil),
			"auth_method": "trust",
			"options":     []string{},
			"error":       (*string)(nil),
		},
	}

	now := time.Now().UTC()
	if err := p.Store(ctx, conn, connID, now, metrics); err != nil {
		t.Fatalf("Store first: %v", err)
	}
	if err := p.Store(ctx, conn, connID, now.Add(time.Minute),
		metrics); err != nil {
		t.Fatalf("Store unchanged: %v", err)
	}
}

func TestPgIdentFileMappingsProbe_StoreUnchanged(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	p := NewPgIdentFileMappingsProbe(&ProbeConfig{
		Name: ProbeNamePgIdentFileMappings,
	})
	connID := 1237

	if _, err := conn.Exec(ctx,
		"DELETE FROM metrics.pg_ident_file_mappings WHERE connection_id=$1",
		connID); err != nil {
		t.Fatalf("clean: %v", err)
	}

	// Empty input matches empty stored data; the second call exercises
	// the "no-change" branch via HasDataChanged with two empty
	// snapshots.
	if err := p.Store(ctx, conn, connID, time.Now().UTC(),
		nil); err != nil {
		t.Fatalf("Store first: %v", err)
	}
	if err := p.Store(ctx, conn, connID,
		time.Now().UTC().Add(time.Minute), nil); err != nil {
		t.Fatalf("Store unchanged empty: %v", err)
	}
}

func TestPgExtensionProbe_StoreUnchanged(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	p := NewPgExtensionProbe(&ProbeConfig{Name: ProbeNamePgExtension})
	connID := 1238
	if _, err := conn.Exec(ctx,
		"DELETE FROM metrics.pg_extension WHERE connection_id=$1",
		connID); err != nil {
		t.Fatalf("clean: %v", err)
	}

	metrics := []map[string]any{
		{
			"_database_name": "testdb",
			"extname":        "plpgsql",
			"extversion":     "1.0",
			"extrelocatable": false,
			"schema_name":    "pg_catalog",
		},
	}
	now := time.Now().UTC()
	if err := p.Store(ctx, conn, connID, now, metrics); err != nil {
		t.Fatalf("Store first: %v", err)
	}
	if err := p.Store(ctx, conn, connID, now.Add(time.Minute),
		metrics); err != nil {
		t.Fatalf("Store unchanged: %v", err)
	}
}
