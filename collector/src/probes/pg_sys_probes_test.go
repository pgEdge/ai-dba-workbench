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

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgSysProbe is the shared interface for the system_stats-backed probes.
// All ten probes embed BaseMetricsProbe and gate on the system_stats
// extension before issuing the probe-specific function call. Coverage
// for them therefore exercises identical surface and Execute branches,
// so we drive them through a single table-driven harness.
type pgSysProbe interface {
	GetName() string
	GetTableName() string
	GetQuery() string
	GetExtensionName() string
	IsDatabaseScoped() bool
	GetConfig() *ProbeConfig
	Execute(context.Context, string, *pgxpool.Conn, int) (
		[]map[string]any, error)
	Store(context.Context, *pgxpool.Conn, int, time.Time,
		[]map[string]any) error
}

type pgSysCase struct {
	name         string
	probe        pgSysProbe
	tableName    string
	queryHint    string
	syntheticRow map[string]any
}

func newSysCases() []pgSysCase {
	return []pgSysCase{
		{
			name:      "os_info",
			probe:     NewPgSysOsInfoProbe(&ProbeConfig{Name: ProbeNamePgSysOsInfo}),
			tableName: ProbeNamePgSysOsInfo,
			queryHint: "pg_sys_os_info()",
			syntheticRow: map[string]any{
				"name": "Linux", "version": "1",
				"host_name": "h", "domain_name": "d",
				"handle_count": int64(1), "process_count": int64(1),
				"thread_count": int64(1), "architecture": "x86_64",
				"last_bootup_time":    time.Now().UTC(),
				"os_up_since_seconds": int64(60),
			},
		},
		{
			name:      "cpu_info",
			probe:     NewPgSysCPUInfoProbe(&ProbeConfig{Name: ProbeNamePgSysCPUInfo}),
			tableName: ProbeNamePgSysCPUInfo,
			queryHint: "pg_sys_cpu_info()",
			syntheticRow: map[string]any{
				"vendor": "intel", "description": "d",
				"model_name": "m", "processor_type": "p",
				"logical_processor":  int64(8),
				"physical_processor": int64(4),
				"no_of_cores":        int64(4),
				"architecture":       "x86_64",
				"clock_speed_hz":     int64(1000000),
				"cpu_type":           "type", "cpu_family": "fam",
				"byte_order":    "le",
				"l1dcache_size": int64(1), "l1icache_size": int64(1),
				"l2cache_size": int64(1), "l3cache_size": int64(1),
			},
		},
		{
			name:      "cpu_usage",
			probe:     NewPgSysCPUUsageInfoProbe(&ProbeConfig{Name: ProbeNamePgSysCPUUsageInfo}),
			tableName: ProbeNamePgSysCPUUsageInfo,
			queryHint: "pg_sys_cpu_usage_info()",
			syntheticRow: map[string]any{
				"usermode_normal_process_percent": 0.0,
				"usermode_niced_process_percent":  0.0,
				"kernelmode_process_percent":      0.0,
				"idle_mode_percent":               99.0,
				"io_completion_percent":           0.0,
				"servicing_irq_percent":           0.0,
				"servicing_softirq_percent":       0.0,
				"user_time_percent":               0.0,
				"processor_time_percent":          0.0,
				"privileged_time_percent":         0.0,
				"interrupt_time_percent":          0.0,
			},
		},
		{
			name:      "memory_info",
			probe:     NewPgSysMemoryInfoProbe(&ProbeConfig{Name: ProbeNamePgSysMemoryInfo}),
			tableName: ProbeNamePgSysMemoryInfo,
			queryHint: "pg_sys_memory_info()",
			syntheticRow: map[string]any{
				"total_memory": int64(1), "used_memory": int64(1),
				"free_memory": int64(1), "swap_total": int64(0),
				"swap_used":   int64(0),
				"swap_free":   int64(0),
				"cache_total": int64(0), "kernel_total": int64(0),
				"kernel_paged": int64(0), "kernel_non_paged": int64(0),
				"total_page_file": int64(0), "avail_page_file": int64(0),
			},
		},
		{
			name:      "io_analysis",
			probe:     NewPgSysIoAnalysisInfoProbe(&ProbeConfig{Name: ProbeNamePgSysIoAnalysisInfo}),
			tableName: ProbeNamePgSysIoAnalysisInfo,
			queryHint: "pg_sys_io_analysis_info()",
			syntheticRow: map[string]any{
				"device_name":  "sda",
				"total_reads":  int64(1),
				"total_writes": int64(1),
				"read_bytes":   int64(1),
				"write_bytes":  int64(1),
				"read_time_ms": int64(1), "write_time_ms": int64(1),
			},
		},
		{
			name:      "disk_info",
			probe:     NewPgSysDiskInfoProbe(&ProbeConfig{Name: ProbeNamePgSysDiskInfo}),
			tableName: ProbeNamePgSysDiskInfo,
			queryHint: "pg_sys_disk_info()",
			syntheticRow: map[string]any{
				"mount_point": "/", "file_system": "ext4",
				"drive_letter": "", "drive_type": "ssd",
				"file_system_type": "ext4", "total_space": int64(1),
				"used_space": int64(1), "free_space": int64(0),
				"total_inodes": int64(1), "used_inodes": int64(1),
				"free_inodes": int64(0),
			},
		},
		{
			name:      "load_avg",
			probe:     NewPgSysLoadAvgInfoProbe(&ProbeConfig{Name: ProbeNamePgSysLoadAvgInfo}),
			tableName: ProbeNamePgSysLoadAvgInfo,
			queryHint: "pg_sys_load_avg_info()",
			syntheticRow: map[string]any{
				"load_avg_one_minute":      0.1,
				"load_avg_five_minutes":    0.1,
				"load_avg_ten_minutes":     0.1,
				"load_avg_fifteen_minutes": 0.1,
			},
		},
		{
			name:      "process_info",
			probe:     NewPgSysProcessInfoProbe(&ProbeConfig{Name: ProbeNamePgSysProcessInfo}),
			tableName: ProbeNamePgSysProcessInfo,
			queryHint: "pg_sys_process_info()",
			syntheticRow: map[string]any{
				"total_processes": int64(1), "running_processes": int64(1),
				"sleeping_processes": int64(0),
				"stopped_processes":  int64(0),
				"zombie_processes":   int64(0),
			},
		},
		{
			name:      "network_info",
			probe:     NewPgSysNetworkInfoProbe(&ProbeConfig{Name: ProbeNamePgSysNetworkInfo}),
			tableName: ProbeNamePgSysNetworkInfo,
			queryHint: "pg_sys_network_info()",
			syntheticRow: map[string]any{
				"interface_name": "eth0", "ip_address": "127.0.0.1",
				"tx_bytes": int64(0), "tx_packets": int64(0),
				"tx_errors": int64(0), "tx_dropped": int64(0),
				"rx_bytes": int64(0), "rx_packets": int64(0),
				"rx_errors": int64(0), "rx_dropped": int64(0),
				"link_speed_mbps": int64(1000),
			},
		},
		{
			name:      "cpu_memory_by_process",
			probe:     NewPgSysCPUMemoryByProcessProbe(&ProbeConfig{Name: ProbeNamePgSysCPUMemoryByProcess}),
			tableName: ProbeNamePgSysCPUMemoryByProcess,
			queryHint: "pg_sys_cpu_memory_by_process()",
			syntheticRow: map[string]any{
				"pid":  int64(1),
				"name": "init", "running_since_seconds": int64(60),
				"cpu_usage": 0.0, "memory_usage": 0.0,
				"memory_bytes": int64(1024),
			},
		},
	}
}

func TestPgSysProbes_Surface(t *testing.T) {
	for _, c := range newSysCases() {
		t.Run(c.name, func(t *testing.T) {
			if c.probe.GetName() != c.tableName {
				t.Errorf("GetName() = %v", c.probe.GetName())
			}
			if c.probe.GetTableName() != c.tableName {
				t.Errorf("GetTableName() = %v",
					c.probe.GetTableName())
			}
			if c.probe.GetExtensionName() != "system_stats" {
				t.Errorf("GetExtensionName() = %v",
					c.probe.GetExtensionName())
			}
			if c.probe.IsDatabaseScoped() {
				t.Error("system_stats probes are server-scoped")
			}
			if !strings.Contains(c.probe.GetQuery(), c.queryHint) {
				t.Errorf("GetQuery missing %q", c.queryHint)
			}
			if c.probe.GetConfig() == nil {
				t.Error("GetConfig() = nil")
			}
		})
	}
}

func TestPgSysProbes_StoreEmpty(t *testing.T) {
	for _, c := range newSysCases() {
		t.Run(c.name, func(t *testing.T) {
			if err := c.probe.Store(context.Background(), nil, 1,
				time.Now(), nil); err != nil {
				t.Errorf("Store(nil) = %v", err)
			}
			if err := c.probe.Store(context.Background(), nil, 1,
				time.Now(), []map[string]any{}); err != nil {
				t.Errorf("Store([]) = %v", err)
			}
		})
	}
}

// TestPgSysProbes_ExecuteWithoutExtension covers the gate branch in
// every pg_sys_* probe by running Execute against a connection that
// claims the extension is absent. We achieve this by passing a unique
// connectionName to bypass the feature cache after temporarily removing
// the dummy system_stats row; the row is restored afterwards so other
// tests still see the extension as installed.
func TestPgSysProbes_ExecuteWithoutExtension(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	// Capture and remove the dummy extension row, then restore it
	// at the end of the test so subsequent tests still see it.
	if _, err := conn.Exec(ctx,
		"DELETE FROM pg_extension WHERE extname='system_stats'"); err != nil {
		t.Fatalf("delete extension stub: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := conn.Exec(ctx, `
			INSERT INTO pg_extension (oid, extname, extowner,
				extnamespace, extrelocatable, extversion,
				extconfig, extcondition)
			SELECT (SELECT MAX(oid::oid::int)
				FROM pg_extension) + 1, 'system_stats', 10,
				(SELECT oid FROM pg_namespace
					WHERE nspname='public'),
				TRUE, '1.0', NULL, NULL
			WHERE NOT EXISTS (SELECT 1 FROM pg_extension
				WHERE extname='system_stats')`); cleanupErr != nil {
			t.Logf("restore system_stats stub: %v", cleanupErr)
		}
	})

	for _, c := range newSysCases() {
		t.Run(c.name, func(t *testing.T) {
			// Use a unique connection name so the feature cache
			// for this probe does not collide with the cached
			// "true" results from previous tests.
			metrics, err := c.probe.Execute(ctx,
				fmt.Sprintf("sys-noext-%s", c.name), conn,
				pgVersion)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(metrics) != 0 {
				t.Errorf("expected no rows without extension, "+
					"got %d", len(metrics))
			}
		})
	}
}

// TestPgSysProbes_Execute verifies that every probe runs end-to-end
// against the shared integration database. The setup helper installs
// stub system_stats functions and registers a dummy entry in
// pg_extension so the existence check passes; this drives the probe
// down its happy-path Execute branch.
func TestPgSysProbes_Execute(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	for _, c := range newSysCases() {
		t.Run(c.name, func(t *testing.T) {
			metrics, err := c.probe.Execute(ctx,
				fmt.Sprintf("sys-%s", c.name), conn, pgVersion)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(metrics) == 0 {
				t.Errorf("expected at least one row from stub")
			}
			if err := c.probe.Store(ctx, conn, 1,
				time.Now().UTC(), metrics); err != nil {
				t.Fatalf("Store live metrics: %v", err)
			}
		})
	}
}

// TestPgSysProbes_StoreSynthetic walks the full Store body using
// hand-crafted rows that match the partition table column types. This
// gives us coverage of the row-building loop and StoreMetrics path even
// when the system_stats extension is unavailable.
func TestPgSysProbes_StoreSynthetic(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	for _, c := range newSysCases() {
		t.Run(c.name, func(t *testing.T) {
			rows := []map[string]any{c.syntheticRow}
			if err := c.probe.Store(ctx, conn, 1,
				time.Now().UTC(), rows); err != nil {
				t.Fatalf("Store synthetic: %v", err)
			}
		})
	}
}
