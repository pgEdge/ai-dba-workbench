/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"testing"
	"time"
)

func newPgReplicationSlotsProbeForTest() *PgReplicationSlotsProbe {
	return NewPgReplicationSlotsProbe(&ProbeConfig{
		Name:                      ProbeNamePgReplicationSlots,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgReplicationSlotsProbe_Surface(t *testing.T) {
	p := newPgReplicationSlotsProbeForTest()
	if p.GetName() != ProbeNamePgReplicationSlots {
		t.Errorf("GetName() = %v", p.GetName())
	}
	if p.GetTableName() != ProbeNamePgReplicationSlots {
		t.Errorf("GetTableName() = %v", p.GetTableName())
	}
	if p.IsDatabaseScoped() {
		t.Error("pg_replication_slots is server-scoped")
	}
	// GetQuery deliberately returns "" because Execute composes the
	// query at runtime based on detected catalog features.
	if q := p.GetQuery(); q != "" {
		t.Errorf("GetQuery() = %q, want empty", q)
	}
	if p.GetConfig() == nil {
		t.Error("GetConfig() should not be nil")
	}
}

func TestPgReplicationSlotsProbe_StoreEmpty(t *testing.T) {
	p := newPgReplicationSlotsProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgReplicationSlotsProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgReplicationSlotsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	// On a fresh server with no slots configured, Execute returns an
	// empty slice without error. We still exercise both the stat-view
	// branches via the cached check helpers.
	metrics, err := p.Execute(ctx, "slots-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Re-issuing exercises the cached path.
	if _, err := p.Execute(ctx, "slots-conn", conn, pgVersion); err != nil {
		t.Fatalf("Execute second call: %v", err)
	}

	// Store with the (likely empty) result still walks the early return.
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Synthetic non-empty Store path so we exercise the full body.
	synth := []map[string]any{
		{
			"slot_name": "test_slot", "slot_type": "physical",
			"active": false, "wal_status": nil, "safe_wal_size": nil,
			"retained_bytes": nil, "spill_txns": nil, "spill_count": nil,
			"spill_bytes": nil, "stream_txns": nil, "stream_count": nil,
			"stream_bytes": nil, "total_txns": nil, "total_count": nil,
			"total_bytes": nil, "stats_reset": nil,
		},
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		synth); err != nil {
		t.Fatalf("Store synthetic: %v", err)
	}
}

func TestPgReplicationSlotsProbe_CheckHelpers(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgReplicationSlotsProbeForTest()
	ctx := context.Background()

	// On any supported PG version (14+) pg_stat_replication_slots
	// exists, but the helpers themselves are robust against either
	// outcome. We just confirm they do not error.
	if _, err := p.checkStatReplicationSlotsAvailable(ctx,
		conn); err != nil {
		t.Errorf("checkStatReplicationSlotsAvailable: %v", err)
	}
	if _, err := p.checkHasTotalCount(ctx, conn); err != nil {
		t.Errorf("checkHasTotalCount: %v", err)
	}
}
