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

func newPgStatUserFunctionsProbeForTest() *PgStatUserFunctionsProbe {
	return NewPgStatUserFunctionsProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatUserFunctions,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatUserFunctionsProbe_Surface(t *testing.T) {
	p := newPgStatUserFunctionsProbeForTest()
	if p.GetName() != ProbeNamePgStatUserFunctions {
		t.Errorf("GetName()")
	}
	if !p.IsDatabaseScoped() {
		t.Error("user_functions is database-scoped")
	}
	q := p.GetQuery()
	for _, s := range []string{"funcid", "schemaname", "funcname",
		"pg_stat_user_functions"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
}

func TestPgStatUserFunctionsProbe_StoreEmpty(t *testing.T) {
	p := newPgStatUserFunctionsProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatUserFunctionsProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatUserFunctionsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	// pg_stat_user_functions only contains rows when track_functions is
	// enabled. The empty result still drives the Execute path.
	metrics, err := p.Execute(ctx, "fn-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Store with synthetic data so we walk the full body.
	synth := []map[string]any{
		{
			"_database_name": "testdb",
			"funcid":         int64(123),
			"schemaname":     "public",
			"funcname":       "test_fn",
			"calls":          int64(1),
			"total_time":     1.0, "self_time": 1.0,
		},
	}
	combined := append([]map[string]any{}, metrics...)
	combined = append(combined, synth...)
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		combined); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatUserFunctionsProbe_StoreMissingDatabaseName(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatUserFunctionsProbeForTest()

	err := p.Store(context.Background(), conn, 1, time.Now().UTC(),
		[]map[string]any{{"funcname": "x"}})
	if err == nil ||
		!strings.Contains(err.Error(), "database_name not found") {
		t.Errorf("expected database_name error, got %v", err)
	}
}
