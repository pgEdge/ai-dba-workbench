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

func newPgStatioAllSequencesProbeForTest() *PgStatioAllSequencesProbe {
	return NewPgStatioAllSequencesProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatioAllSequences,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatioAllSequencesProbe_Surface(t *testing.T) {
	p := newPgStatioAllSequencesProbeForTest()
	if p.GetName() != ProbeNamePgStatioAllSequences {
		t.Errorf("GetName()")
	}
	if !p.IsDatabaseScoped() {
		t.Error("pg_statio_all_sequences is database-scoped")
	}
	q := p.GetQuery()
	for _, s := range []string{"relid", "schemaname", "relname",
		"blks_read", "blks_hit", "pg_statio_all_sequences"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
}

func TestPgStatioAllSequencesProbe_StoreEmpty(t *testing.T) {
	p := newPgStatioAllSequencesProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatioAllSequencesProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatioAllSequencesProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "seq-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, m := range metrics {
		m["_database_name"] = "testdb"
	}

	// Add a synthetic row so Store walks the body even on a server with
	// no user sequences.
	metrics = append(metrics, map[string]any{
		"_database_name": "testdb",
		"relid":          int64(99), "schemaname": "public",
		"relname": "seq", "blks_read": int64(0),
		"blks_hit": int64(0),
	})
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatioAllSequencesProbe_StoreMissingDatabaseName(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatioAllSequencesProbeForTest()

	err := p.Store(context.Background(), conn, 1, time.Now().UTC(),
		[]map[string]any{{"relname": "seq"}})
	if err == nil ||
		!strings.Contains(err.Error(), "database_name not found") {
		t.Errorf("expected database_name error, got %v", err)
	}
}
