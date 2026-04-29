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
	"strings"
	"testing"
	"time"
)

func newPgStatAllIndexesProbeForTest() *PgStatAllIndexesProbe {
	return NewPgStatAllIndexesProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatAllIndexes,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatAllIndexesProbe_Surface(t *testing.T) {
	p := newPgStatAllIndexesProbeForTest()
	if p.GetName() != ProbeNamePgStatAllIndexes {
		t.Errorf("GetName()")
	}
	if !p.IsDatabaseScoped() {
		t.Error("should be database-scoped")
	}

	pre16 := p.GetQueryForVersion(15)
	if !strings.Contains(pre16,
		"NULL::timestamptz AS last_idx_scan") {
		t.Errorf("PG15 should NULL last_idx_scan")
	}
	post16 := p.GetQueryForVersion(16)
	if !strings.Contains(post16, "s.last_idx_scan") {
		t.Errorf("PG16+ should select last_idx_scan")
	}
	if p.GetQuery() == "" {
		t.Error("default GetQuery should not be empty")
	}
}

func TestPgStatAllIndexesProbe_StoreEmpty(t *testing.T) {
	p := newPgStatAllIndexesProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatAllIndexesProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatAllIndexesProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "idx-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected catalog indexes")
	}
	for _, m := range metrics {
		m["_database_name"] = "testdb"
	}

	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatAllIndexesProbe_StoreMissingDatabaseName(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatAllIndexesProbeForTest()

	// Metrics without _database_name must surface a clear error so the
	// scheduler can recover instead of corrupting the partition.
	err := p.Store(context.Background(), conn, 1, time.Now().UTC(),
		[]map[string]any{
			{
				"relid": int64(1), "indexrelid": int64(2),
				"schemaname": "public", "relname": "t",
				"indexrelname": "t_pkey",
			},
		})
	if err == nil {
		t.Fatal("expected error for missing _database_name")
	}
	if !strings.Contains(err.Error(), "database_name not found") {
		t.Errorf("error should mention missing key: %v", err)
	}
}
