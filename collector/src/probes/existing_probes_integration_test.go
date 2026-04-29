/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Integration tests for probes whose constructor and structural surface
// were already covered by pre-existing unit tests but whose Execute and
// Store paths were not. Each test acquires the shared integration pool
// (skipping cleanly when no test database is configured) and exercises
// the production code path against a real PostgreSQL instance.
package probes

import (
	"context"
	"testing"
	"time"
)

func TestPgConnectivityProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := NewPgConnectivityProbe(&ProbeConfig{
		Name:                      ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 30,
		RetentionDays:             7,
	})
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "ping-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected one row, got %d", len(metrics))
	}
	rt, ok := metrics[0]["response_time_ms"].(float64)
	if !ok {
		t.Fatalf("response_time_ms is not float64: %T",
			metrics[0]["response_time_ms"])
	}
	if rt < 0 {
		t.Errorf("response_time_ms negative: %v", rt)
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
	// Empty store path
	if err := p.Store(ctx, nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgSettingsProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := NewPgSettingsProbe(&ProbeConfig{
		Name:                      ProbeNamePgSettings,
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
	})
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "settings-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected pg_settings rows")
	}
	// First Store: writes; second Store: change detection skips.
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store first call: %v", err)
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC().Add(time.Minute),
		metrics); err != nil {
		t.Fatalf("Store unchanged: %v", err)
	}
	// Empty Store path
	if err := p.Store(ctx, nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgHbaFileRulesProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := NewPgHbaFileRulesProbe(&ProbeConfig{
		Name:                      ProbeNamePgHbaFileRules,
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
	})
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "hba-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected pg_hba_file_rules rows")
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store first: %v", err)
	}
	// Second store with same data exercises the change-tracking
	// no-op branch.
	if err := p.Store(ctx, conn, 1, time.Now().UTC().Add(time.Minute),
		metrics); err != nil {
		t.Fatalf("Store unchanged: %v", err)
	}
	// Empty store still goes through change detection (which sees
	// matching hashes for empty inputs and returns nil).
	if err := p.Store(ctx, conn, 999, time.Now().UTC(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgIdentFileMappingsProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := NewPgIdentFileMappingsProbe(&ProbeConfig{
		Name:                      ProbeNamePgIdentFileMappings,
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
	})
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	// Likely empty result on a default cluster, exercising the
	// view-exists-but-no-rows branch.
	metrics, err := p.Execute(ctx, "ident-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Store empty path
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store empty: %v", err)
	}
	// Cached check on second Execute
	if _, err := p.Execute(ctx, "ident-conn", conn,
		pgVersion); err != nil {
		t.Fatalf("Execute cached: %v", err)
	}
	// Synthetic row to drive the full Store path.
	synth := []map[string]any{
		{
			"map_number": int64(1), "file_name": "pg_ident.conf",
			"line_number": int64(1), "map_name": "test_map",
			"sys_name": "alice", "pg_username": "postgres",
			"error": nil,
		},
	}
	if err := p.Store(ctx, conn, 7, time.Now().UTC(),
		synth); err != nil {
		t.Fatalf("Store synthetic: %v", err)
	}
}

func TestPgServerInfoProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := NewPgServerInfoProbe(&ProbeConfig{
		Name:                      ProbeNamePgServerInfo,
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
	})
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "info-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected one row, got %d", len(metrics))
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store first: %v", err)
	}
	// Identical second store hits the unchanged path.
	if err := p.Store(ctx, conn, 1, time.Now().UTC().Add(time.Minute),
		metrics); err != nil {
		t.Fatalf("Store unchanged: %v", err)
	}
	// Different connectionID forces the "no previous data" branch.
	if err := p.Store(ctx, conn, 42, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store new connection: %v", err)
	}
	// Empty store path
	if err := p.Store(ctx, nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgNodeRoleProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := NewPgNodeRoleProbe(&ProbeConfig{
		Name:                      ProbeNamePgNodeRole,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
	})
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "role-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected one role row, got %d", len(metrics))
	}
	role, ok := metrics[0]["primary_role"].(string)
	if !ok {
		t.Fatalf("primary_role is not string: %T",
			metrics[0]["primary_role"])
	}
	if role == "" {
		t.Error("primary_role should be set")
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
	// Empty store path
	if err := p.Store(ctx, nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatReplicationProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := NewPgStatReplicationProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatReplication,
		CollectionIntervalSeconds: 30,
		RetentionDays:             30,
	})
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	// On a primary with no replicas, the receiver query also returns
	// nothing; Execute returns whatever was collected (often empty)
	// and Store walks the early-return path.
	metrics, err := p.Execute(ctx, "repl-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
	// Drive checkIsInRecovery directly so its return path is covered.
	if _, err := p.checkIsInRecovery(ctx, conn); err != nil {
		t.Errorf("checkIsInRecovery: %v", err)
	}

	// Synthetic non-empty Store path.
	now := time.Now().UTC()
	synth := []map[string]any{
		{
			"role": "primary", "pid": int64(1),
			"usesysid": int64(10), "usename": "rep",
			"application_name": "walreceiver",
			"client_addr":      nil, "client_hostname": nil,
			"client_port": nil, "backend_start": now,
			"backend_xmin":          nil,
			"state":                 "streaming",
			"sent_lsn":              "0/0",
			"write_lsn":             "0/0",
			"flush_lsn":             "0/0",
			"replay_lsn":            "0/0",
			"write_lag":             nil,
			"flush_lag":             nil,
			"replay_lag":            nil,
			"sync_priority":         int64(0),
			"sync_state":            "async",
			"reply_time":            now,
			"receiver_pid":          nil,
			"receiver_status":       nil,
			"receive_start_lsn":     nil,
			"receive_start_tli":     nil,
			"written_lsn":           nil,
			"receiver_flushed_lsn":  nil,
			"received_tli":          nil,
			"last_msg_send_time":    nil,
			"last_msg_receipt_time": nil,
			"latest_end_lsn":        nil,
			"latest_end_time":       nil,
			"receiver_slot_name":    nil,
			"sender_host":           nil,
			"sender_port":           nil,
			"conninfo":              nil,
		},
	}
	if err := p.Store(ctx, conn, 1, now,
		synth); err != nil {
		t.Fatalf("Store synthetic: %v", err)
	}
}

func TestPgStatSubscriptionProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := NewPgStatSubscriptionProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatSubscription,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
	})
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	if _, err := p.Execute(ctx, "sub-conn", conn, pgVersion); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Helpers
	if _, err := p.checkHasWorkerType(ctx, conn); err != nil {
		t.Errorf("checkHasWorkerType: %v", err)
	}
	if _, err := p.checkStatSubscriptionStatsAvailable(ctx,
		conn); err != nil {
		t.Errorf("checkStatSubscriptionStatsAvailable: %v", err)
	}

	// Synthetic Store path.
	now := time.Now().UTC()
	synth := []map[string]any{
		{
			"subid": int64(1), "subname": "test_sub",
			"worker_type": "apply", "pid": int64(2),
			"leader_pid": nil, "relid": nil,
			"received_lsn":          "0/0",
			"last_msg_send_time":    now,
			"last_msg_receipt_time": now,
			"latest_end_lsn":        "0/0",
			"latest_end_time":       now,
			"apply_error_count":     int64(0),
			"sync_error_count":      int64(0), "stats_reset": now,
		},
	}
	if err := p.Store(ctx, conn, 1, now,
		synth); err != nil {
		t.Fatalf("Store synthetic: %v", err)
	}
}
