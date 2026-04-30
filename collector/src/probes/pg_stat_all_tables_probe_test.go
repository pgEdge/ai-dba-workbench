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

func newPgStatAllTablesProbeForTest() *PgStatAllTablesProbe {
	return NewPgStatAllTablesProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatAllTables,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatAllTablesProbe_Surface(t *testing.T) {
	p := newPgStatAllTablesProbeForTest()
	if p.GetName() != ProbeNamePgStatAllTables {
		t.Errorf("GetName()")
	}
	if !p.IsDatabaseScoped() {
		t.Error("should be database-scoped")
	}
	q := p.GetQuery()
	for _, s := range []string{"pg_stat_all_tables", "pg_statio_all_tables",
		"n_live_tup", "n_dead_tup", "vacuum_count"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
}

func TestPgStatAllTablesProbe_StoreEmpty(t *testing.T) {
	p := newPgStatAllTablesProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatAllTablesProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatAllTablesProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "tbl-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected catalog tables")
	}
	for _, m := range metrics {
		m["_database_name"] = "testdb"
	}

	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatAllTablesProbe_StoreMissingDatabaseName(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatAllTablesProbeForTest()

	err := p.Store(context.Background(), conn, 1, time.Now().UTC(),
		[]map[string]any{{"schemaname": "public", "relname": "t"}})
	if err == nil ||
		!strings.Contains(err.Error(), "database_name not found") {
		t.Errorf("expected database_name error, got %v", err)
	}
}
