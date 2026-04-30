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

	"github.com/jackc/pgx/v5/pgxpool"
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
		if _, cleanupErr := conn.Exec(ctx,
			"DELETE FROM pg_subscription "+
				"WHERE subname='test_node_role_sub'"); cleanupErr != nil {
			t.Logf("cleanup pg_subscription: %v", cleanupErr)
		}
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
	// so this exercises the early-return path of getSpockStatus. The
	// integration test database is created fresh from `template1`
	// each process by requireIntegrationPool, so spock is never
	// pre-installed in practice. We still precheck pg_extension and
	// skip if a future setup change ends up seeding it; that keeps
	// the test correct regardless of the environment.
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	if spockAlreadyInstalled(t, ctx, conn) {
		t.Skip("spock extension already installed; this test " +
			"requires an environment without spock")
	}

	p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
	info := &NodeRoleInfo{RoleDetails: map[string]any{}}
	if err := p.getSpockStatus(ctx, "x", conn, info); err != nil {
		t.Fatalf("getSpockStatus: %v", err)
	}
	if info.HasSpock {
		t.Error("default cluster has no spock extension")
	}
}

// spockAlreadyInstalled reports whether the spock extension is already
// registered in pg_extension. The integration database is created
// fresh per process, so this should always return false in CI; the
// guard is defensive against future setup changes that pre-seed the
// extension (which would otherwise turn the destructive cleanup in
// the spock-stub tests into data loss).
func spockAlreadyInstalled(t *testing.T, ctx context.Context,
	conn *pgxpool.Conn) bool {
	t.Helper()
	var present bool
	err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_extension WHERE extname = 'spock'
		)
	`).Scan(&present)
	if err != nil {
		t.Fatalf("check pg_extension for spock: %v", err)
	}
	return present
}

// TestPgNodeRoleProbe_GetSpockStatus_FakeExtension drives the
// spock-installed branch of getSpockStatus by registering a dummy
// spock extension and creating the spock.local_node, spock.node, and
// spock.subscription objects the probe queries. The dummy entries are
// removed before other tests run.
//
// The cleanup is gated on `wePreInstalledSpock` so the test never
// removes a real spock install if one happens to be present. The
// integration database is created fresh per process, so the guard is
// defensive against future setup changes; today the precheck always
// returns false here.
func TestPgNodeRoleProbe_GetSpockStatus_FakeExtension(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	if spockAlreadyInstalled(t, ctx, conn) {
		t.Skip("spock extension already installed; this test " +
			"creates a stub spock and would otherwise need to " +
			"destroy a real one to clean up")
	}

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
		// Only tear down what this test created. If a future change
		// causes spock to be pre-installed, the precheck above would
		// have skipped the test and we would not get here.
		if _, cleanupErr := conn.Exec(ctx,
			"DELETE FROM pg_extension "+
				"WHERE extname='spock'"); cleanupErr != nil {
			t.Logf("cleanup pg_extension spock row: %v", cleanupErr)
		}
		if _, cleanupErr := conn.Exec(ctx,
			"DROP SCHEMA spock CASCADE"); cleanupErr != nil {
			t.Logf("cleanup spock schema: %v", cleanupErr)
		}
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
//
// The cleanup is gated on the precheck so we never tear down a real
// spock install. See the comment on TestPgNodeRoleProbe_GetSpockStatus_
// FakeExtension for the full rationale.
func TestPgNodeRoleProbe_GetSpockStatus_NoLocalNode(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	if spockAlreadyInstalled(t, ctx, conn) {
		t.Skip("spock extension already installed; this test " +
			"creates a stub spock and would otherwise need to " +
			"destroy a real one to clean up")
	}

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
		if _, cleanupErr := conn.Exec(ctx,
			"DELETE FROM pg_extension "+
				"WHERE extname='spock'"); cleanupErr != nil {
			t.Logf("cleanup pg_extension spock row: %v", cleanupErr)
		}
		if _, cleanupErr := conn.Exec(ctx,
			"DROP SCHEMA spock CASCADE"); cleanupErr != nil {
			t.Logf("cleanup spock schema: %v", cleanupErr)
		}
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
