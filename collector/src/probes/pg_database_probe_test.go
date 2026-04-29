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

func newPgDatabaseProbeForTest() *PgDatabaseProbe {
	return NewPgDatabaseProbe(&ProbeConfig{
		Name:                      ProbeNamePgDatabase,
		Description:               "test",
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgDatabaseProbe_Surface(t *testing.T) {
	p := newPgDatabaseProbeForTest()

	if p.GetName() != ProbeNamePgDatabase {
		t.Errorf("GetName() = %v", p.GetName())
	}
	if p.GetTableName() != ProbeNamePgDatabase {
		t.Errorf("GetTableName() = %v", p.GetTableName())
	}
	if p.IsDatabaseScoped() {
		t.Error("pg_database is server-scoped; IsDatabaseScoped should be false")
	}
	if cfg := p.GetConfig(); cfg == nil || cfg.Name != ProbeNamePgDatabase {
		t.Errorf("GetConfig() = %+v", cfg)
	}
	if q := p.GetQuery(); !strings.Contains(q, "FROM pg_database") {
		t.Errorf("GetQuery() missing pg_database: %q", q)
	}
}

func TestPgDatabaseProbe_GetQueryForVersion(t *testing.T) {
	p := newPgDatabaseProbeForTest()

	pre16 := p.GetQueryForVersion(15)
	if !strings.Contains(pre16, "NULL AS datlocprovider") {
		t.Errorf("PG15 query should NULL out datlocprovider, got %q", pre16)
	}

	post16 := p.GetQueryForVersion(16)
	if strings.Contains(post16, "NULL AS datlocprovider") {
		t.Errorf("PG16+ query should not NULL datlocprovider")
	}
	if !strings.Contains(post16, "datlocprovider") {
		t.Errorf("PG16+ query should select datlocprovider")
	}

	if !strings.Contains(p.GetQuery(), "datlocprovider") {
		t.Error("default GetQuery() should include datlocprovider")
	}
}

func TestPgDatabaseProbe_StoreEmpty(t *testing.T) {
	p := newPgDatabaseProbeForTest()
	// nil conn is safe because the early return triggers on len(metrics)==0
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil metrics) = %v, want nil", err)
	}
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		[]map[string]any{}); err != nil {
		t.Errorf("Store([]) = %v, want nil", err)
	}
}

func TestPgDatabaseProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)

	p := newPgDatabaseProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "test-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected at least one database row")
	}
	for _, m := range metrics {
		if m["datname"] == nil {
			t.Errorf("datname should not be nil: %+v", m)
		}
	}

	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}
