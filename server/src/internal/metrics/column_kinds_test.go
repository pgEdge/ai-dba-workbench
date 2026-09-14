/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package metrics

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// registerProbeKindsForTest installs entry under probe in the column kind
// registry for the duration of the calling test, restoring whatever was
// there before (or removing the entry) on cleanup. Integration fixtures
// create tables under their own names, which are not real probes, so this
// is how they obtain counter semantics for their columns; without it every
// fixture column is a gauge and _per_sec is refused.
func registerProbeKindsForTest(t testing.TB, probe string, entry probeRegistryEntry) {
	t.Helper()
	probeRegistryMu.Lock()
	prev, had := probeRegistry[probe]
	probeRegistry[probe] = entry
	probeRegistryMu.Unlock()
	t.Cleanup(func() {
		probeRegistryMu.Lock()
		defer probeRegistryMu.Unlock()
		if had {
			probeRegistry[probe] = prev
			return
		}
		delete(probeRegistry, probe)
	})
}

// countersForTest is the common fixture registration: every listed column
// is a cumulative counter and the probe has no reset marker.
func countersForTest(cols ...string) probeRegistryEntry {
	return probeRegistryEntry{kinds: columnKinds(counters(cols...))}
}

func TestColumnKindString(t *testing.T) {
	for _, tc := range []struct {
		kind ColumnKind
		want string
	}{
		{KindGauge, "gauge"},
		{KindCounter, "cumulative counter"},
		{KindTimeCounter, "cumulative time counter"},
		{KindSessionTimeCounter, "session time counter"},
		{KindWatermark, "lifetime watermark"},
		{KindRatio, "ratio"},
		{ColumnKind(99), "unknown"},
	} {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("ColumnKind(%d).String() = %q, want %q", int(tc.kind), got, tc.want)
		}
	}
}

func TestColumnKindFor(t *testing.T) {
	for _, tc := range []struct {
		probe, col string
		want       ColumnKind
	}{
		{"pg_stat_all_tables", "seq_scan", KindCounter},
		{"pg_stat_all_tables", "n_dead_tup", KindGauge},
		{"pg_stat_all_tables", "n_live_tup", KindGauge},
		{"pg_stat_all_tables", "table_size", KindGauge},
		{"pg_stat_all_indexes", "idx_blks_hit", KindCounter},
		{"pg_stat_statements", "calls", KindCounter},
		{"pg_stat_statements", "total_exec_time", KindTimeCounter},
		{"pg_stat_statements", "mean_exec_time", KindWatermark},
		{"pg_stat_statements", "stddev_exec_time", KindWatermark},
		{"pg_stat_database", "xact_commit", KindCounter},
		{"pg_stat_database", "numbackends", KindGauge},
		{"pg_stat_database", "blk_read_time", KindTimeCounter},
		{"pg_stat_database", "active_time", KindSessionTimeCounter},
		{"pg_stat_database", "sessions", KindCounter},
		{"pg_stat_database_conflicts", "confl_lock", KindCounter},
		{"pg_stat_checkpointer", "buffers_alloc", KindCounter},
		{"pg_stat_checkpointer", "write_time", KindTimeCounter},
		{"pg_stat_wal", "wal_bytes", KindCounter},
		{"pg_stat_wal", "wal_sync_time", KindTimeCounter},
		{"pg_replication_slots", "spill_bytes", KindCounter},
		{"pg_replication_slots", "retained_bytes", KindGauge},
		{"pg_stat_subscription", "apply_error_count", KindCounter},
		{"pg_stat_recovery_prefetch", "prefetch", KindCounter},
		{"pg_stat_recovery_prefetch", "io_depth", KindGauge},
		{"pg_stat_io", "reads", KindCounter},
		{"pg_stat_io", "fsync_time", KindTimeCounter},
		{"pg_stat_io", "op_bytes", KindGauge},
		{"pg_stat_user_functions", "calls", KindCounter},
		{"pg_stat_user_functions", "self_time", KindTimeCounter},
		{"pg_statio_all_sequences", "blks_read", KindCounter},
		{"pg_sys_cpu_usage_info", "idle_mode_percent", KindRatio},
		{"pg_sys_io_analysis_info", "read_bytes", KindCounter},
		{"pg_sys_io_analysis_info", "read_time_ms", KindTimeCounter},
		{"pg_sys_network_info", "rx_bytes", KindCounter},
		{"pg_sys_network_info", "link_speed_mbps", KindGauge},
		{"pg_stat_replication", "write_lag", KindGauge},
		{"no_such_probe", "anything", KindGauge},
	} {
		if got := ColumnKindFor(tc.probe, tc.col); got != tc.want {
			t.Errorf("ColumnKindFor(%q, %q) = %s, want %s",
				tc.probe, tc.col, got, tc.want)
		}
	}
}

func TestResetColumnFor(t *testing.T) {
	for _, tc := range []struct {
		probe, col, want string
	}{
		{"pg_stat_database", "xact_commit", "stats_reset"},
		{"pg_stat_statements", "calls", "stats_reset"},
		{"pg_stat_wal", "wal_records", "stats_reset"},
		{"pg_stat_wal", "archived_count", "archiver_stats_reset"},
		{"pg_stat_wal", "failed_count", "archiver_stats_reset"},
		{"pg_stat_checkpointer", "num_timed", "stats_reset"},
		{"pg_stat_checkpointer", "buffers_clean", "bgwriter_stats_reset"},
		{"pg_stat_checkpointer", "maxwritten_clean", "bgwriter_stats_reset"},
		{"pg_stat_checkpointer", "buffers_alloc", "bgwriter_stats_reset"},
		{"pg_stat_io", "reads", "stats_reset"},
		{"pg_stat_recovery_prefetch", "hit", "stats_reset"},
		{"pg_replication_slots", "spill_txns", "stats_reset"},
		{"pg_stat_subscription", "sync_error_count", "stats_reset"},
		{"pg_stat_all_tables", "seq_scan", ""},
		{"pg_sys_network_info", "tx_bytes", ""},
		{"no_such_probe", "anything", ""},
	} {
		if got := ResetColumnFor(tc.probe, tc.col); got != tc.want {
			t.Errorf("ResetColumnFor(%q, %q) = %q, want %q",
				tc.probe, tc.col, got, tc.want)
		}
	}
}

func TestProbeEntityExclusion(t *testing.T) {
	if got := ProbeEntityExclusion("pg_sys_network_info"); got != "interface_name NOT IN ('lo', 'lo0')" {
		t.Errorf("unexpected network exclusion %q", got)
	}
	if got := ProbeEntityExclusion("pg_stat_database"); got != "" {
		t.Errorf("expected no exclusion for pg_stat_database, got %q", got)
	}
	if got := ProbeEntityExclusion("no_such_probe"); got != "" {
		t.Errorf("expected no exclusion for unknown probe, got %q", got)
	}
}

func TestRegisterProbeKindsForTest(t *testing.T) {
	const probe = "registry_hook_test"
	if ColumnKindFor(probe, "hits") != KindGauge {
		t.Fatal("test probe unexpectedly registered before the hook ran")
	}

	t.Run("registers and cleans up a new probe", func(t *testing.T) {
		registerProbeKindsForTest(t, probe, probeRegistryEntry{
			kinds:           columnKinds(counters("hits")),
			resetColumn:     fixedReset("stats_reset"),
			excludeEntities: "name <> 'x'",
		})
		if ColumnKindFor(probe, "hits") != KindCounter {
			t.Error("hook did not register the counter")
		}
		if ResetColumnFor(probe, "hits") != "stats_reset" {
			t.Error("hook did not register the reset column")
		}
		if ProbeEntityExclusion(probe) != "name <> 'x'" {
			t.Error("hook did not register the exclusion")
		}
	})
	if ColumnKindFor(probe, "hits") != KindGauge {
		t.Error("cleanup did not remove the test probe")
	}

	t.Run("restores an overridden real probe", func(t *testing.T) {
		registerProbeKindsForTest(t, "pg_stat_wal", countersForTest("wal_records"))
		if ResetColumnFor("pg_stat_wal", "wal_records") != "" {
			t.Error("override still reports the real reset column")
		}
	})
	if ResetColumnFor("pg_stat_wal", "wal_records") != "stats_reset" {
		t.Error("cleanup did not restore the real pg_stat_wal entry")
	}
}

// collectorSchemaPath locates the collector DDL relative to this package.
const collectorSchemaPath = "../../../../collector/src/database/schema.go"

// ddlColumns returns the column names of metrics.<probe> as declared in the
// collector DDL: the CREATE TABLE body plus any later ADD COLUMN on the
// same table. ok is false when the DDL has no such table.
func ddlColumns(ddl, probe string) (map[string]bool, bool) {
	marker := "CREATE TABLE IF NOT EXISTS metrics." + probe + " ("
	start := strings.Index(ddl, marker)
	if start < 0 {
		return nil, false
	}
	body := ddl[start+len(marker):]
	end := strings.Index(body, "PARTITION BY RANGE")
	if end < 0 {
		return nil, false
	}
	body = body[:end]

	cols := make(map[string]bool)
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "PRIMARY", "UNIQUE", "CONSTRAINT", "FOREIGN", "CHECK", ")":
			continue
		}
		cols[fields[0]] = true
	}

	added := regexp.MustCompile(
		`ALTER TABLE metrics\.` + regexp.QuoteMeta(probe) +
			`\s+ADD COLUMN IF NOT EXISTS (\w+)`)
	for _, m := range added.FindAllStringSubmatch(ddl, -1) {
		cols[m[1]] = true
	}
	return cols, true
}

// pendingDDLColumns lists reset columns the registry names ahead of the
// collector migration that adds them, so this test passes whether or not
// that migration has landed in the checkout. Remove an entry once its
// migration is in schema.go.
var pendingDDLColumns = map[string]map[string]bool{
	// Collector migration 10, issue #402, section 5 of the design spec.
	"pg_stat_statements": {"stats_reset": true},
}

// registryEntryProblems returns a description of every way entry, filed
// under probe, disagrees with the collector DDL; nil means it agrees.
func registryEntryProblems(ddl, probe string, entry probeRegistryEntry) []string {
	var problems []string
	cols, ok := ddlColumns(ddl, probe)
	if !ok {
		return []string{"probe " + probe + " has no CREATE TABLE in the collector DDL"}
	}
	if len(entry.kinds) == 0 {
		problems = append(problems, probe+" lists no column kinds")
	}
	for col, kind := range entry.kinds {
		if kind == KindGauge {
			problems = append(problems,
				probe+"."+col+" is registered as a gauge; leave gauges unlisted")
		}
		if !cols[col] {
			problems = append(problems,
				probe+"."+col+" is registered but not in the collector DDL")
		}
		if entry.resetColumn == nil {
			continue
		}
		r := entry.resetColumn(col)
		if r == "" || cols[r] || pendingDDLColumns[probe][r] {
			continue
		}
		problems = append(problems,
			probe+"."+col+" names reset column "+r+", which is not in the collector DDL")
	}
	if entry.excludeEntities != "" {
		// The fragment must reference a real column; take the first
		// identifier as the column name.
		first := strings.Fields(entry.excludeEntities)[0]
		if !cols[first] {
			problems = append(problems,
				probe+" exclusion references "+first+", which is not in the collector DDL")
		}
	}
	return problems
}

// TestProbeRegistryMatchesCollectorDDL fails when the registry names a
// probe or column the collector does not create, which is the failure
// mode a mistyped entry would otherwise hide behind (an unknown column is
// silently a gauge).
func TestProbeRegistryMatchesCollectorDDL(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(collectorSchemaPath))
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("collector DDL not found at %s", collectorSchemaPath)
	}
	if err != nil {
		t.Fatalf("read collector DDL: %v", err)
	}
	ddl := string(raw)

	probeRegistryMu.RLock()
	defer probeRegistryMu.RUnlock()
	if len(probeRegistry) == 0 {
		t.Fatal("registry is empty")
	}
	for probe, entry := range probeRegistry {
		for _, p := range registryEntryProblems(ddl, probe, entry) {
			t.Error(p)
		}
	}
}

func TestRegistryEntryProblems(t *testing.T) {
	const ddl = `
        CREATE TABLE IF NOT EXISTS metrics.probe_a (
            connection_id INTEGER NOT NULL,
            iface TEXT NOT NULL,
            hits BIGINT,
            stats_reset TIMESTAMPTZ,
            PRIMARY KEY (connection_id)
        ) PARTITION BY RANGE (collected_at);
    `
	for _, tc := range []struct {
		name  string
		probe string
		entry probeRegistryEntry
		want  []string
	}{
		{
			name:  "valid entry",
			probe: "probe_a",
			entry: probeRegistryEntry{
				kinds:           columnKinds(counters("hits")),
				resetColumn:     fixedReset("stats_reset"),
				excludeEntities: "iface <> 'lo'",
			},
		},
		{
			name:  "unknown probe",
			probe: "probe_b",
			entry: countersForTest("hits"),
			want:  []string{"no CREATE TABLE"},
		},
		{
			name:  "empty kinds",
			probe: "probe_a",
			entry: probeRegistryEntry{},
			want:  []string{"lists no column kinds"},
		},
		{
			name:  "mistyped column",
			probe: "probe_a",
			entry: countersForTest("hist"),
			want:  []string{"probe_a.hist is registered but not"},
		},
		{
			name:  "explicit gauge",
			probe: "probe_a",
			entry: probeRegistryEntry{kinds: map[string]ColumnKind{"hits": KindGauge}},
			want:  []string{"registered as a gauge"},
		},
		{
			name:  "mistyped reset column",
			probe: "probe_a",
			entry: probeRegistryEntry{
				kinds:       columnKinds(counters("hits")),
				resetColumn: fixedReset("stat_reset"),
			},
			want: []string{"names reset column stat_reset"},
		},
		{
			name:  "empty reset column is fine",
			probe: "probe_a",
			entry: probeRegistryEntry{
				kinds:       columnKinds(counters("hits")),
				resetColumn: fixedReset(""),
			},
		},
		{
			name:  "exclusion on unknown column",
			probe: "probe_a",
			entry: probeRegistryEntry{
				kinds:           columnKinds(counters("hits")),
				excludeEntities: "interface_name <> 'lo'",
			},
			want: []string{"exclusion references interface_name"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := registryEntryProblems(ddl, tc.probe, tc.entry)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d problems %v, want %d", len(got), got, len(tc.want))
			}
			for i, w := range tc.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}

func TestDDLColumnsHelper(t *testing.T) {
	const ddl = `
        CREATE TABLE IF NOT EXISTS metrics.t1 (
            connection_id INTEGER NOT NULL,
            a BIGINT,
            PRIMARY KEY (connection_id)
        ) PARTITION BY RANGE (collected_at);
        ALTER TABLE metrics.t1
            ADD COLUMN IF NOT EXISTS b TIMESTAMPTZ;
        CREATE TABLE IF NOT EXISTS metrics.t2 (
            x INTEGER
        );
    `
	cols, ok := ddlColumns(ddl, "t1")
	if !ok {
		t.Fatal("expected t1 to be found")
	}
	for _, c := range []string{"connection_id", "a", "b"} {
		if !cols[c] {
			t.Errorf("expected column %q", c)
		}
	}
	if cols["PRIMARY"] || cols["KEY"] {
		t.Error("constraint lines must not register as columns")
	}
	if _, ok := ddlColumns(ddl, "t2"); ok {
		t.Error("a table without PARTITION BY RANGE should not parse")
	}
	if _, ok := ddlColumns(ddl, "t3"); ok {
		t.Error("a missing table should not parse")
	}
}
