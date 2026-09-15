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
	"sort"
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
		if r == "" || cols[r] {
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

// ddlNumericColumns returns the columns of metrics.<probe> whose declared
// type in the collector DDL can carry a counter: the integer, numeric and
// floating-point types. Text, boolean, timestamp and OID columns are
// dimensions or identifiers and can never be differenced, so they are left
// out. ok is false when the DDL has no such table.
func ddlNumericColumns(ddl, probe string) (map[string]bool, bool) {
	numeric := map[string]bool{
		"BIGINT": true, "INTEGER": true, "SMALLINT": true, "NUMERIC": true,
		"REAL": true, "DOUBLE": true, "FLOAT8": true, "BIGSERIAL": true,
	}

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
	keep := func(name, dataType string) {
		if numeric[strings.ToUpper(strings.Trim(dataType, ","))] {
			cols[name] = true
		}
	}
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "PRIMARY", "UNIQUE", "CONSTRAINT", "FOREIGN", "CHECK", ")":
			continue
		}
		keep(fields[0], fields[1])
	}

	added := regexp.MustCompile(
		`ALTER TABLE metrics\.` + regexp.QuoteMeta(probe) +
			`\s+ADD COLUMN IF NOT EXISTS (\w+)\s+(\w+)`)
	for _, m := range added.FindAllStringSubmatch(ddl, -1) {
		keep(m[1], m[2])
	}
	return cols, true
}

// knownGaugeColumns pins the numeric columns of a registered probe that
// are deliberately left out of the registry, and so are gauges. Every
// numeric column of a registered probe must appear either in its registry
// entry or here, which is what stops a counter column added to the
// collector DDL from being silently classified as a gauge: the developer
// adding it has to decide which list it belongs in, rather than leaving a
// user to find the 400 that a _per_sec request on it returns.
//
// connection_id is the probe tables' own bookkeeping column and is
// excluded everywhere rather than repeated per probe.
var knownGaugeColumns = map[string][]string{
	"pg_replication_slots":      {"retained_bytes", "safe_wal_size"},
	"pg_stat_all_indexes":       {"index_size"},
	"pg_stat_all_tables":        {"n_dead_tup", "n_live_tup", "n_mod_since_analyze", "table_size"},
	"pg_stat_database":          {"numbackends"},
	"pg_stat_io":                {"op_bytes"},
	"pg_stat_recovery_prefetch": {"block_distance", "io_depth", "wal_distance"},
	"pg_stat_statements":        {"queryid"},
	"pg_stat_subscription":      {"leader_pid", "pid"},
	"pg_sys_network_info":       {"link_speed_mbps"},
}

// unregisteredProbeTables pins the metrics tables the collector creates
// that have no registry entry at all, and whose every column is therefore
// a gauge. A new probe table has to be added here (or to the registry)
// before this test passes, so a table arriving with counters in it cannot
// go unnoticed either.
var unregisteredProbeTables = []string{
	"pg_connectivity",
	"pg_database",
	"pg_extension",
	"pg_hba_file_rules",
	"pg_ident_file_mappings",
	"pg_node_role",
	"pg_server_info",
	"pg_settings",
	"pg_stat_activity",
	"pg_stat_connection_security",
	"pg_stat_replication",
	"pg_sys_cpu_info",
	"pg_sys_cpu_memory_by_process",
	"pg_sys_disk_info",
	"pg_sys_load_avg_info",
	"pg_sys_memory_info",
	"pg_sys_os_info",
	"pg_sys_process_info",
	"spock_exception_log",
	"spock_resolutions",
}

// unregisteredColumnProblems returns a description of every numeric column
// of probe in the collector DDL that entry neither classifies nor lists as
// a known gauge: the reverse of registryEntryProblems, which only checks
// that what the registry names exists.
func unregisteredColumnProblems(
	ddl, probe string, entry probeRegistryEntry, knownGauges []string,
) []string {
	cols, ok := ddlNumericColumns(ddl, probe)
	if !ok {
		return []string{"probe " + probe + " has no CREATE TABLE in the collector DDL"}
	}
	known := map[string]bool{"connection_id": true}
	for _, c := range knownGauges {
		known[c] = true
		if !cols[c] {
			return []string{probe + "." + c +
				" is listed as a known gauge but is not a numeric column of the collector DDL"}
		}
	}

	var problems []string
	var missing []string
	for col := range cols {
		if known[col] {
			continue
		}
		if _, ok := entry.kinds[col]; !ok {
			missing = append(missing, col)
		}
	}
	sort.Strings(missing)
	for _, col := range missing {
		problems = append(problems, probe+"."+col+
			" is a numeric collector column that is neither registered nor a known gauge;"+
			" register its kind or add it to knownGaugeColumns")
	}
	return problems
}

// TestProbeRegistryMatchesCollectorDDL fails when the registry names a
// probe or column the collector does not create, which is the failure
// mode a mistyped entry would otherwise hide behind (an unknown column is
// silently a gauge), and in the reverse direction when the collector DDL
// carries a numeric column the registry neither classifies nor records as
// a deliberate gauge, which would otherwise be found by a user hitting a
// 400 on its _per_sec rather than by CI.
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
		for _, p := range unregisteredColumnProblems(
			ddl, probe, entry, knownGaugeColumns[probe]) {
			t.Error(p)
		}
	}

	// Every known-gauge list must belong to a registered probe, so a
	// probe removed from the registry cannot leave a stale list behind.
	for probe := range knownGaugeColumns {
		if _, ok := probeRegistry[probe]; !ok {
			t.Errorf("knownGaugeColumns names %s, which is not a registered probe", probe)
		}
	}

	// And every metrics table the collector creates is either registered
	// or pinned as gauge-only, so a new probe table forces the choice.
	accounted := make(map[string]bool, len(probeRegistry)+len(unregisteredProbeTables))
	for probe := range probeRegistry {
		accounted[probe] = true
	}
	for _, probe := range unregisteredProbeTables {
		if _, ok := probeRegistry[probe]; ok {
			t.Errorf("%s is both registered and listed as gauge-only", probe)
		}
		accounted[probe] = true
	}
	tables := regexp.MustCompile(
		`CREATE TABLE IF NOT EXISTS metrics\.(\w+)`).FindAllStringSubmatch(ddl, -1)
	if len(tables) == 0 {
		t.Fatal("no metrics tables found in the collector DDL")
	}
	for _, m := range tables {
		if !accounted[m[1]] {
			t.Errorf("collector table metrics.%s is neither in probeRegistry nor in"+
				" unregisteredProbeTables; classify its counter columns or record"+
				" that it has none", m[1])
		}
	}
}

func TestDDLNumericColumns(t *testing.T) {
	const ddl = `
        CREATE TABLE IF NOT EXISTS metrics.probe_n (
            connection_id INTEGER NOT NULL,
            iface TEXT NOT NULL,
            hits BIGINT,
            ratio DOUBLE PRECISION,
            seen BOOLEAN,
            relid OID,
            stats_reset TIMESTAMPTZ,
            PRIMARY KEY (connection_id)
        ) PARTITION BY RANGE (collected_at);
        ALTER TABLE metrics.probe_n
            ADD COLUMN IF NOT EXISTS misses BIGINT;
    `
	cols, ok := ddlNumericColumns(ddl, "probe_n")
	if !ok {
		t.Fatal("probe_n not found")
	}
	want := map[string]bool{
		"connection_id": true, "hits": true, "ratio": true, "misses": true,
	}
	if len(cols) != len(want) {
		t.Fatalf("got %v, want %v", cols, want)
	}
	for c := range want {
		if !cols[c] {
			t.Errorf("%s missing from %v", c, cols)
		}
	}
	if _, ok := ddlNumericColumns(ddl, "probe_absent"); ok {
		t.Error("probe_absent reported as present")
	}
}

func TestUnregisteredColumnProblems(t *testing.T) {
	const ddl = `
        CREATE TABLE IF NOT EXISTS metrics.probe_u (
            connection_id INTEGER NOT NULL,
            hits BIGINT,
            depth BIGINT,
            misses BIGINT,
            name TEXT,
            PRIMARY KEY (connection_id)
        ) PARTITION BY RANGE (collected_at);
    `
	for _, tc := range []struct {
		name        string
		probe       string
		entry       probeRegistryEntry
		knownGauges []string
		want        []string
	}{
		{
			name:        "every numeric column accounted for",
			probe:       "probe_u",
			entry:       countersForTest("hits", "misses"),
			knownGauges: []string{"depth"},
		},
		{
			name:        "unaccounted counter column",
			probe:       "probe_u",
			entry:       countersForTest("hits"),
			knownGauges: []string{"depth"},
			want:        []string{"probe_u.misses is a numeric collector column"},
		},
		{
			name:  "two unaccounted columns are reported in order",
			probe: "probe_u",
			entry: countersForTest("hits"),
			want:  []string{"probe_u.depth is a", "probe_u.misses is a"},
		},
		{
			name:        "known gauge that is not a numeric column",
			probe:       "probe_u",
			entry:       countersForTest("hits", "misses", "depth"),
			knownGauges: []string{"name"},
			want:        []string{"probe_u.name is listed as a known gauge"},
		},
		{
			name:  "unknown probe",
			probe: "probe_absent",
			entry: countersForTest("hits"),
			want:  []string{"no CREATE TABLE"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := unregisteredColumnProblems(ddl, tc.probe, tc.entry, tc.knownGauges)
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
