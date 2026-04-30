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

func newPgStatDatabaseProbeForTest() *PgStatDatabaseProbe {
	return NewPgStatDatabaseProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatDatabase,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatDatabaseProbe_Surface(t *testing.T) {
	p := newPgStatDatabaseProbeForTest()
	if p.GetName() != ProbeNamePgStatDatabase {
		t.Errorf("GetName()")
	}
	if !p.IsDatabaseScoped() {
		t.Error("should be database-scoped")
	}
	q := p.GetQuery()
	for _, s := range []string{"datid", "numbackends", "xact_commit",
		"pg_stat_database", "current_database()"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
}

func TestPgStatDatabaseProbe_StoreEmpty(t *testing.T) {
	p := newPgStatDatabaseProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatDatabaseProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatDatabaseProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "db-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 row (current DB), got %d", len(metrics))
	}
	for _, m := range metrics {
		m["_database_name"] = "testdb"
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatDatabaseProbe_StoreMissingDatabaseName(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatDatabaseProbeForTest()

	err := p.Store(context.Background(), conn, 1, time.Now().UTC(),
		[]map[string]any{{"datname": "x"}})
	if err == nil ||
		!strings.Contains(err.Error(), "database_name not found") {
		t.Errorf("expected database_name error, got %v", err)
	}
}
