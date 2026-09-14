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
	"fmt"
	"strings"
	"testing"
	"time"
)

func newPgStatActivityProbeForTest() *PgStatActivityProbe {
	return NewPgStatActivityProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatActivity,
		CollectionIntervalSeconds: 60,
		RetentionDays:             7,
		IsEnabled:                 true,
	})
}

func TestPgStatActivityProbe_Surface(t *testing.T) {
	p := newPgStatActivityProbeForTest()
	if p.GetName() != ProbeNamePgStatActivity {
		t.Errorf("GetName()")
	}
	if p.GetTableName() != ProbeNamePgStatActivity {
		t.Errorf("GetTableName()")
	}
	if p.IsDatabaseScoped() {
		t.Error("pg_stat_activity is server-scoped")
	}
	q := p.GetQuery()
	for _, s := range []string{"pid", "datname", "query", "backend_type",
		"pg_stat_activity"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
}

// TestPgStatActivityProbe_QueryIDByVersion pins the version guard on
// pg_stat_activity.query_id (issue #384): the column exists from
// PostgreSQL 14, so older servers must get a typed NULL placeholder that
// keeps the column list, and hence Store, identical across versions.
func TestPgStatActivityProbe_QueryIDByVersion(t *testing.T) {
	p := newPgStatActivityProbeForTest()

	tests := []struct {
		name      string
		pgVersion int
		want      string
		unwanted  string
	}{
		{"pg13 uses NULL placeholder", 13,
			"NULL::bigint AS query_id", "\tquery_id,"},
		{"pg14 selects query_id", 14,
			"\tquery_id,", "NULL::bigint AS query_id"},
		{"pg18 selects query_id", 18,
			"\tquery_id,", "NULL::bigint AS query_id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := p.GetQueryForVersion(tc.pgVersion)
			if !strings.Contains(q, tc.want) {
				t.Errorf("GetQueryForVersion(%d) missing %q:\n%s",
					tc.pgVersion, tc.want, q)
			}
			if strings.Contains(q, tc.unwanted) {
				t.Errorf("GetQueryForVersion(%d) contains %q:\n%s",
					tc.pgVersion, tc.unwanted, q)
			}
			if !strings.Contains(q, "pg_stat_activity") {
				t.Errorf("GetQueryForVersion(%d) lost the source view",
					tc.pgVersion)
			}
		})
	}

	// GetQuery is the newest-version shape.
	if !strings.Contains(p.GetQuery(), "\tquery_id,") {
		t.Error("GetQuery should select query_id")
	}
}

func TestPgStatActivityProbe_StoreEmpty(t *testing.T) {
	p := newPgStatActivityProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatActivityProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatActivityProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "activity-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// At least the wal-writer / autovacuum / etc backends should exist.
	if metrics == nil {
		t.Fatal("Execute returned nil metrics slice")
	}

	// Every row must carry the query_id key, whichever version branch
	// produced it, so that Store always writes the column.
	for i, m := range metrics {
		if _, ok := m["query_id"]; !ok {
			t.Fatalf("metrics[%d] has no query_id key", i)
		}
	}

	if err := p.Store(ctx, conn, 1, time.Now().UTC(), metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

// TestPgStatActivityProbe_StoresQueryID drives the query_id round trip
// (issue #384) end to end: a second session runs a long statement so that
// the probe, which excludes its own backend and sees only NULL identifiers
// on idle backends, samples an in-flight query. The value Store writes for
// that pid must equal what pg_stat_activity itself reported, which on a
// server that does not compute identifiers (before PostgreSQL 14, or with
// compute_query_id off) is NULL on both sides.
func TestPgStatActivityProbe_StoresQueryID(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatActivityProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	sleeper := acquireConn(t, pool)
	var sleeperPID int
	if err := sleeper.QueryRow(ctx,
		"SELECT pg_backend_pid()").Scan(&sleeperPID); err != nil {
		t.Fatalf("sleeper pid: %v", err)
	}

	sleepCtx, cancelSleep := context.WithCancel(ctx)
	sleepErr := make(chan error, 1)
	go func() {
		_, err := sleeper.Exec(sleepCtx, "SELECT pg_sleep(30)")
		sleepErr <- err
	}()
	t.Cleanup(func() {
		cancelSleep()
		// The sleeper is stopped deliberately, so its error is
		// expected and only logged for diagnosis.
		if err := <-sleepErr; err != nil {
			t.Logf("sleeper session ended: %v", err)
		}
	})

	// Wait until the sleeper shows up as active, then take its query_id
	// as the expected value. The column does not exist before PG14.
	queryIDExpr := "query_id"
	if pgVersion < 14 {
		queryIDExpr = "NULL::bigint"
	}
	var expected *int64
	deadline := time.Now().Add(10 * time.Second)
	for {
		var state *string
		err := conn.QueryRow(ctx, fmt.Sprintf(
			"SELECT state, %s FROM pg_stat_activity WHERE pid = $1",
			queryIDExpr), sleeperPID).Scan(&state, &expected)
		if err == nil && state != nil && *state == "active" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sleeper pid %d never became active: %v", sleeperPID, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	metrics, err := p.Execute(ctx, "activity-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	collectedAt := time.Now().UTC()
	if err := p.Store(ctx, conn, 2, collectedAt, metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}

	var stored *int64
	if err := conn.QueryRow(ctx, `
		SELECT query_id FROM metrics.pg_stat_activity
		WHERE connection_id = 2 AND collected_at = $1 AND pid = $2`,
		collectedAt, sleeperPID).Scan(&stored); err != nil {
		t.Fatalf("read back sleeper row: %v", err)
	}

	switch {
	case expected == nil && stored != nil:
		t.Errorf("stored query_id = %d, want NULL", *stored)
	case expected != nil && stored == nil:
		t.Errorf("stored query_id = NULL, want %d", *expected)
	case expected != nil && stored != nil && *expected != *stored:
		t.Errorf("stored query_id = %d, want %d", *stored, *expected)
	}
	if pgVersion >= 14 && expected == nil {
		t.Logf("server does not compute query identifiers; NULL round trip verified")
	}
}
