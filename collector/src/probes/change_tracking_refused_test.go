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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
)

// refusingInsertHelper stands in for the datastore's ColdFront insert
// helper: it writes pg_settings rows into the plain partitioned table
// and refuses, with a data exception, any batch holding the setting
// named refused_setting, as ColdFront refuses a value it cannot convert.
const refusingInsertHelper = `
CREATE SCHEMA ` + coldfront.HelperSchema + `;
CREATE FUNCTION ` + coldfront.InsertRowsFunction + `(p_table text, p_rows jsonb)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    IF p_table <> 'pg_settings' THEN
        RAISE EXCEPTION 'unexpected table %', p_table;
    END IF;
    IF EXISTS (SELECT 1 FROM jsonb_array_elements(p_rows) r
               WHERE r->>'name' = 'refused_setting') THEN
        RAISE EXCEPTION 'refused' USING ERRCODE = '22P02';
    END IF;
    INSERT INTO metrics.pg_settings
    SELECT * FROM jsonb_populate_recordset(NULL::metrics.pg_settings, p_rows);
END $$;
`

// settingRow is a pg_settings probe row that round-trips through the
// datastore unchanged.
func settingRow(name, setting string) map[string]any {
	return map[string]any{
		"name": name, "setting": setting, "unit": nil, "category": "Test",
		"short_desc": "test setting", "extra_desc": "", "context": "user",
		"vartype": "string", "source": "default", "min_val": nil,
		"max_val": nil, "enumvals": nil, "boot_val": setting,
		"reset_val": setting, "sourcefile": nil, "sourceline": nil,
		"pending_restart": false,
	}
}

// installRefusingInsertHelper switches StoreMetrics to ColdFront with
// refusingInsertHelper as the datastore's insert helper, with no
// pg_settings rows for connID and partitions for first to last made
// beforehand, since EnsurePartition does nothing under ColdFront. All of
// it is undone when the test ends.
func installRefusingInsertHelper(t *testing.T, pool *pgxpool.Pool, conn *pgxpool.Conn, connID int, first, last time.Time) {
	t.Helper()
	ctx := context.Background()
	for _, ts := range []time.Time{first, last} {
		if err := EnsurePartition(ctx, conn, "pg_settings", ts); err != nil {
			t.Fatalf("EnsurePartition: %v", err)
		}
	}
	const deleteRows = "DELETE FROM metrics.pg_settings WHERE connection_id = $1"
	if _, err := conn.Exec(ctx, deleteRows, connID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+coldfront.HelperSchema+" CASCADE;"+refusingInsertHelper); err != nil {
		t.Fatalf("failed to install the refusing insert helper: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+coldfront.HelperSchema+" CASCADE"); err != nil {
			t.Errorf("failed to drop the insert helper: %v", err)
		}
		if _, err := pool.Exec(ctx, deleteRows, connID); err != nil {
			t.Errorf("failed to delete the test rows: %v", err)
		}
		refusedSnapshotRows.set("pg_settings", connID, nil)
	})
	enableColdFront(t)
}

// TestChangeTrackingIgnoresRefusedRows shows that a change-tracked probe
// with a row ColdFront refuses every time stores its snapshot once and
// then not again while nothing else changes, and stores again when
// another row changes. Before, the stored snapshot lacked the refused
// row, so every comparison saw a change and the whole snapshot was
// written again on every run.
func TestChangeTrackingIgnoresRefusedRows(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	const connID = 98761
	p := NewPgSettingsProbe(&ProbeConfig{Name: ProbeNamePgSettings})

	base := time.Now().UTC().Truncate(time.Minute)
	at := func(i int) time.Time { return base.Add(time.Duration(i) * time.Minute) }
	installRefusingInsertHelper(t, pool, conn, connID, at(0), at(5))

	snapshots := func() int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(DISTINCT collected_at)
            FROM metrics.pg_settings WHERE connection_id = $1`, connID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	store := func(i int, metrics []map[string]any) {
		t.Helper()
		if err := p.Store(ctx, conn, connID, at(i), metrics); err != nil {
			t.Fatalf("Store at run %d: %v", i, err)
		}
	}

	metrics := []map[string]any{settingRow("work_mem", "4MB"), settingRow("refused_setting", "x")}
	store(0, metrics)
	if got := snapshots(); got != 1 {
		t.Fatalf("after the first run %d snapshots are stored, want 1", got)
	}

	// Nothing changed but the refused row is still collected: no store.
	store(1, metrics)
	store(2, metrics)
	if got := snapshots(); got != 1 {
		t.Fatalf("unchanged runs stored %d snapshots, want still 1", got)
	}

	// Another row changes: the snapshot is stored again, the refused row
	// still dropped, and then it settles once more.
	changed := []map[string]any{settingRow("work_mem", "8MB"), settingRow("refused_setting", "x")}
	store(3, changed)
	if got := snapshots(); got != 2 {
		t.Fatalf("a changed row stored %d snapshots in all, want 2", got)
	}
	store(4, changed)
	if got := snapshots(); got != 2 {
		t.Fatalf("an unchanged run after the change stored %d snapshots in all, want 2", got)
	}

	// The refused row itself changing is a change too.
	store(5, []map[string]any{settingRow("work_mem", "8MB"), settingRow("refused_setting", "y")})
	if got := snapshots(); got != 3 {
		t.Fatalf("a changed refused row stored %d snapshots in all, want 3", got)
	}
}

// TestChangeTrackedProbesReportStoreFailure shows each change-tracked
// probe returns the failure of storeSnapshot: under ColdFront, without
// the datastore's insert helper, the first snapshot of a connection
// cannot be written.
func TestChangeTrackedProbesReportStoreFailure(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+coldfront.HelperSchema+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	enableColdFront(t)

	cfg := func(name string) *ProbeConfig { return &ProbeConfig{Name: name} }
	cases := []struct {
		name  string
		store func(connID int) error
	}{
		{ProbeNamePgSettings, func(id int) error {
			return NewPgSettingsProbe(cfg(ProbeNamePgSettings)).Store(ctx, conn, id, time.Now(),
				[]map[string]any{settingRow("work_mem", "4MB")})
		}},
		{ProbeNamePgExtension, func(id int) error {
			return NewPgExtensionProbe(cfg(ProbeNamePgExtension)).Store(ctx, conn, id, time.Now(),
				[]map[string]any{{"_database_name": "postgres", "extname": "plpgsql",
					"extversion": "1.0", "extrelocatable": false, "schema_name": "pg_catalog"}})
		}},
		{ProbeNamePgHbaFileRules, func(id int) error {
			return NewPgHbaFileRulesProbe(cfg(ProbeNamePgHbaFileRules)).Store(ctx, conn, id, time.Now(),
				[]map[string]any{{"rule_number": int32(1), "type": "local", "auth_method": "trust"}})
		}},
		{ProbeNamePgIdentFileMappings, func(id int) error {
			return NewPgIdentFileMappingsProbe(cfg(ProbeNamePgIdentFileMappings)).Store(ctx, conn, id, time.Now(),
				[]map[string]any{{"map_number": int32(1), "map_name": "m", "sys_name": "s", "pg_username": "u"}})
		}},
	}
	for i, p := range cases {
		t.Run(p.name, func(t *testing.T) {
			err := p.store(98770 + i)
			if err == nil || !strings.Contains(err.Error(), "failed to store metrics") {
				t.Errorf("Store error = %v, want a failure to store metrics", err)
			}
		})
	}
}

func TestRefusedRowSet(t *testing.T) {
	s := &refusedRowSet{rows: map[refusedRowKey]map[string]struct{}{}}
	a, b := map[string]any{"name": "a"}, map[string]any{"name": "b"}
	rows := []map[string]any{a, b}

	// Nothing recorded: the input is returned as it is.
	if got := s.exclude("p", 1, rows); len(got) != 2 {
		t.Fatalf("exclude with nothing recorded = %v", got)
	}

	// A row that cannot be hashed is never recorded.
	s.set("p", 1, []map[string]any{b, {"name": make(chan int)}})
	if got := s.exclude("p", 1, rows); len(got) != 1 || got[0]["name"] != "a" {
		t.Errorf("exclude = %v, want only a", got)
	}
	// Another probe or connection is not affected.
	if got := s.exclude("p", 2, rows); len(got) != 2 {
		t.Errorf("exclude for another connection = %v", got)
	}
	if got := s.exclude("q", 1, rows); len(got) != 2 {
		t.Errorf("exclude for another probe = %v", got)
	}

	// A store that drops nothing clears the record.
	s.set("p", 1, nil)
	if len(s.rows) != 0 {
		t.Errorf("rows after clearing = %v", s.rows)
	}
	s.set("p", 1, []map[string]any{{"name": make(chan int)}})
	if len(s.rows) != 0 {
		t.Errorf("an unhashable row was recorded: %v", s.rows)
	}
}
