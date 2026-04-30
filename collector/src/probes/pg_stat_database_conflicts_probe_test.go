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

func newPgStatDatabaseConflictsProbeForTest() *PgStatDatabaseConflictsProbe {
	return NewPgStatDatabaseConflictsProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatDatabaseConflicts,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatDatabaseConflictsProbe_Surface(t *testing.T) {
	p := newPgStatDatabaseConflictsProbeForTest()
	if p.GetName() != ProbeNamePgStatDatabaseConflicts {
		t.Errorf("GetName()")
	}
	if !p.IsDatabaseScoped() {
		t.Error("should be database-scoped")
	}

	pre16 := p.GetQueryForVersion(15)
	if !strings.Contains(pre16,
		"NULL::bigint AS confl_active_logicalslot") {
		t.Error("PG15 should NULL confl_active_logicalslot")
	}
	post16 := p.GetQueryForVersion(16)
	if strings.Contains(post16, "NULL::bigint AS confl_active_logicalslot") {
		t.Error("PG16+ should select confl_active_logicalslot directly")
	}
	if p.GetQuery() == "" {
		t.Error("default GetQuery should not be empty")
	}
}

func TestPgStatDatabaseConflictsProbe_StoreEmpty(t *testing.T) {
	p := newPgStatDatabaseConflictsProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatDatabaseConflictsProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatDatabaseConflictsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "conflicts-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, m := range metrics {
		m["_database_name"] = "testdb"
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatDatabaseConflictsProbe_StoreMissingDatabaseName(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatDatabaseConflictsProbeForTest()

	err := p.Store(context.Background(), conn, 1, time.Now().UTC(),
		[]map[string]any{{"datid": int64(1), "datname": "x"}})
	if err == nil ||
		!strings.Contains(err.Error(), "database_name not found") {
		t.Errorf("expected database_name error, got %v", err)
	}
}
