/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Direct tests for the per-aspect collection helpers on
// PgNodeRoleProbe. Calling each helper individually lets us drive
// branches that the full Execute path cannot reach on a single-node
// test cluster: the in-recovery WAL receiver lookup, the subscription
// connection-info parsing, and the spock-extension-installed flow.
package probes

import (
	"context"
	"strings"
	"testing"
)

func TestPgNodeRoleProbe_GetBinaryReplicationStatus_Recovery(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
	info := &NodeRoleInfo{
		IsInRecovery: true, // Force the receiver-lookup branch.
		RoleDetails:  map[string]any{},
	}
	// On a primary, pg_stat_wal_receiver is empty, so the LIMIT 1
	// scan returns ErrNoRows. The helper logs and continues; we just
	// require it not to fail outright.
	if err := p.getBinaryReplicationStatus(ctx, conn, info); err != nil {
		t.Fatalf("getBinaryReplicationStatus: %v", err)
	}
	if info.IsStreamingStandby {
		t.Error("primary should not report IsStreamingStandby")
	}
}

func TestPgNodeRoleProbe_GetLogicalReplicationStatus_WithSubscription(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Insert a synthetic row directly into pg_subscription so the
	// helper exercises the "subscription_count > 0" path that pulls
	// publisher conninfo. pg_subscription has no default for oid
	// because in normal use the catalog allocates it; we generate a
	// fresh OID using nextval on a temporary sequence.
	if _, err := conn.Exec(ctx,
		"CREATE TEMP SEQUENCE IF NOT EXISTS test_sub_oid_seq "+
			"START 90000"); err != nil {
		t.Fatalf("create temp seq: %v", err)
	}
	_, err := conn.Exec(ctx, `
		INSERT INTO pg_subscription (
			oid, subdbid, subskiplsn, subname, subowner,
			subenabled, subbinary, substream, subtwophasestate,
			subdisableonerr, subpasswordrequired, subrunasowner,
			subconninfo, subslotname, subsynccommit,
			subpublications, suborigin)
		SELECT
			nextval('test_sub_oid_seq')::oid,
			(SELECT oid FROM pg_database
				WHERE datname = current_database()),
			'0/0'::pg_lsn,
			'test_node_role_sub',
			10,
			false, false, 'p', 'd',
			false, false, false,
			'host=publisher.example port=5444 dbname=foo',
			'test_node_role_slot', 'off',
			ARRAY['test_pub']::text[], 'any'
		WHERE NOT EXISTS (
			SELECT 1 FROM pg_subscription
			WHERE subname='test_node_role_sub')
	`)
	if err != nil {
		// Some PG versions lack one or more of these columns; if
		// the synthetic insert is unsupported, skip rather than
		// fail.
		t.Skipf("cannot synthesize pg_subscription row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(ctx,
			"DELETE FROM pg_subscription "+
				"WHERE subname='test_node_role_sub'")
	})

	p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
	info := &NodeRoleInfo{RoleDetails: map[string]any{}}
	if err := p.getLogicalReplicationStatus(ctx, conn,
		info); err != nil {
		t.Fatalf("getLogicalReplicationStatus: %v", err)
	}
	if info.SubscriptionCount < 1 {
		t.Errorf("expected subscription_count >= 1, got %d",
			info.SubscriptionCount)
	}
	if info.PublisherHost == nil ||
		!strings.Contains(*info.PublisherHost, "publisher") {
		t.Errorf("expected publisher host parsed, got %v",
			info.PublisherHost)
	}
}

func TestPgNodeRoleProbe_GetSpockStatus_NoExtension(t *testing.T) {
	// On a default cluster the spock extension is not installed,
	// so this exercises the early-return path of getSpockStatus.
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
	info := &NodeRoleInfo{RoleDetails: map[string]any{}}
	if err := p.getSpockStatus(ctx, "x", conn, info); err != nil {
		t.Fatalf("getSpockStatus: %v", err)
	}
	if info.HasSpock {
		t.Error("default cluster has no spock extension")
	}
}

// TestPgNodeRoleProbe_GetSpockStatus_FakeExtension drives the
// spock-installed branch of getSpockStatus by registering a dummy
// spock extension and creating the spock.local_node, spock.node, and
// spock.subscription objects the probe queries. The dummy entries are
// removed before other tests run.
func TestPgNodeRoleProbe_GetSpockStatus_FakeExtension(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	stmts := []string{
		`CREATE SCHEMA IF NOT EXISTS spock`,
		`CREATE TABLE IF NOT EXISTS spock.node (
			node_id INTEGER PRIMARY KEY,
			node_name TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS spock.local_node (
			node_id INTEGER PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS spock.subscription (
			sub_id INTEGER PRIMARY KEY,
			sub_enabled BOOLEAN NOT NULL DEFAULT TRUE)`,
		`INSERT INTO spock.node (node_id, node_name)
			VALUES (1, 'node-a') ON CONFLICT DO NOTHING`,
		`INSERT INTO spock.local_node (node_id)
			VALUES (1) ON CONFLICT DO NOTHING`,
		`INSERT INTO spock.subscription (sub_id, sub_enabled)
			VALUES (1, TRUE) ON CONFLICT DO NOTHING`,
		`INSERT INTO pg_extension (oid, extname, extowner,
			extnamespace, extrelocatable, extversion,
			extconfig, extcondition)
			SELECT (SELECT MAX(oid::oid::int)
				FROM pg_extension) + 1, 'spock', 10,
				(SELECT oid FROM pg_namespace
					WHERE nspname='spock'),
				TRUE, '1.0', NULL, NULL
			WHERE NOT EXISTS (SELECT 1 FROM pg_extension
				WHERE extname='spock')`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("setup spock: %v\n%s", err, s)
		}
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(ctx,
			"DELETE FROM pg_extension WHERE extname='spock'")
		_, _ = conn.Exec(ctx, "DROP SCHEMA spock CASCADE")
	})

	p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
	info := &NodeRoleInfo{RoleDetails: map[string]any{}}
	if err := p.getSpockStatus(ctx, "with-spock", conn,
		info); err != nil {
		t.Fatalf("getSpockStatus: %v", err)
	}
	if !info.HasSpock {
		t.Error("expected HasSpock=true with stub extension")
	}
	if info.SpockNodeName == nil ||
		*info.SpockNodeName != "node-a" {
		t.Errorf("SpockNodeName: got %v", info.SpockNodeName)
	}
	if info.SpockSubscriptionCount != 1 {
		t.Errorf("SpockSubscriptionCount: got %d",
			info.SpockSubscriptionCount)
	}
}

// TestPgNodeRoleProbe_GetSpockStatus_NoLocalNode drives the
// "spock installed but local_node has no row" branch which logs and
// returns nil without populating SpockNodeID.
func TestPgNodeRoleProbe_GetSpockStatus_NoLocalNode(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	stmts := []string{
		`CREATE SCHEMA IF NOT EXISTS spock`,
		`CREATE TABLE IF NOT EXISTS spock.node (
			node_id INTEGER PRIMARY KEY,
			node_name TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS spock.local_node (
			node_id INTEGER PRIMARY KEY)`,
		`INSERT INTO pg_extension (oid, extname, extowner,
			extnamespace, extrelocatable, extversion,
			extconfig, extcondition)
			SELECT (SELECT MAX(oid::oid::int)
				FROM pg_extension) + 1, 'spock', 10,
				(SELECT oid FROM pg_namespace
					WHERE nspname='spock'),
				TRUE, '1.0', NULL, NULL
			WHERE NOT EXISTS (SELECT 1 FROM pg_extension
				WHERE extname='spock')`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("setup spock no local_node: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(ctx,
			"DELETE FROM pg_extension WHERE extname='spock'")
		_, _ = conn.Exec(ctx, "DROP SCHEMA spock CASCADE")
	})

	p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
	info := &NodeRoleInfo{RoleDetails: map[string]any{}}
	if err := p.getSpockStatus(ctx, "spock-no-local", conn,
		info); err != nil {
		t.Fatalf("getSpockStatus: %v", err)
	}
	if !info.HasSpock {
		t.Error("expected HasSpock=true")
	}
	if info.SpockNodeID != nil {
		t.Errorf("SpockNodeID should be nil when local_node empty")
	}
}

func TestPgNodeRoleProbe_GetFundamentalStatus(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
	info := &NodeRoleInfo{RoleDetails: map[string]any{}}
	if err := p.getFundamentalStatus(ctx, conn, info); err != nil {
		t.Fatalf("getFundamentalStatus: %v", err)
	}
	if info.PostmasterStartTime == nil {
		t.Error("postmaster start time should be populated")
	}
}
