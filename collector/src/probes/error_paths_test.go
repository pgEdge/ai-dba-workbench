/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests that exercise probe Execute and Store error-wrapping branches
// by closing the underlying connection before invoking the probe. The
// pgxpool.Conn surfaces the underlying error from Query/Exec, which the
// production code wraps and returns. Each test acquires a dedicated
// connection so closing it does not affect other tests.
package probes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// breakConn closes the underlying pgx connection so any subsequent
// query against the pool conn errors out. The pool itself stays
// healthy because the bad conn is discarded on Release.
func breakConn(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	if err := conn.Conn().Close(context.Background()); err != nil {
		t.Fatalf("close underlying conn: %v", err)
	}
}

// TestCheckHelpers_ErrorPath exercises the error-wrap branches of the
// per-probe catalog check helpers (checkHasWorkerType,
// checkHasSharedBlkTime, etc.) by invoking each one against a closed
// connection.
func TestCheckHelpers_ErrorPath(t *testing.T) {
	pool := requireIntegrationPool(t)

	cases := []struct {
		name string
		run  func(ctx context.Context, c *pgxpool.Conn) error
	}{
		{"replication_slots_stat_available", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgReplicationSlotsProbe(&ProbeConfig{Name: ProbeNamePgReplicationSlots})
			_, err := p.checkStatReplicationSlotsAvailable(ctx, c)
			return err
		}},
		{"replication_slots_total_count", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgReplicationSlotsProbe(&ProbeConfig{Name: ProbeNamePgReplicationSlots})
			_, err := p.checkHasTotalCount(ctx, c)
			return err
		}},
		{"connection_security_gssapi", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatConnectionSecurityProbe(&ProbeConfig{Name: ProbeNamePgStatConnectionSecurity})
			_, err := p.checkGSSAPIAvailable(ctx, c)
			return err
		}},
		{"connection_security_credentials_delegated", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatConnectionSecurityProbe(&ProbeConfig{Name: ProbeNamePgStatConnectionSecurity})
			_, err := p.checkCredentialsDelegatedColumn(ctx, c)
			return err
		}},
		{"io_view_exists", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatIOProbe(&ProbeConfig{Name: ProbeNamePgStatIO})
			_, err := p.checkIOViewExists(ctx, c)
			return err
		}},
		{"slru_view_exists", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatIOProbe(&ProbeConfig{Name: ProbeNamePgStatIO})
			_, err := p.checkSLRUViewExists(ctx, c)
			return err
		}},
		{"statements_shared_blk_time", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatStatementsProbe(&ProbeConfig{Name: ProbeNamePgStatStatements})
			_, err := p.checkHasSharedBlkTime(ctx, c)
			return err
		}},
		{"statements_blk_read_time", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatStatementsProbe(&ProbeConfig{Name: ProbeNamePgStatStatements})
			_, err := p.checkHasBlkReadTime(ctx, c)
			return err
		}},
		{"subscription_worker_type", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatSubscriptionProbe(&ProbeConfig{Name: ProbeNamePgStatSubscription})
			_, err := p.checkHasWorkerType(ctx, c)
			return err
		}},
		{"subscription_stats_available", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatSubscriptionProbe(&ProbeConfig{Name: ProbeNamePgStatSubscription})
			_, err := p.checkStatSubscriptionStatsAvailable(ctx, c)
			return err
		}},
		{"replication_in_recovery", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatReplicationProbe(&ProbeConfig{Name: ProbeNamePgStatReplication})
			_, err := p.checkIsInRecovery(ctx, c)
			return err
		}},
		{"check_view_exists", func(ctx context.Context, c *pgxpool.Conn) error {
			_, err := CheckViewExists(ctx, c, "pg_views")
			return err
		}},
		{"check_extension_exists", func(ctx context.Context, c *pgxpool.Conn) error {
			_, err := CheckExtensionExists(ctx, "x", c, "plpgsql")
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			defer conn.Release()
			breakConn(t, conn)

			err = tc.run(ctx, conn)
			if err == nil {
				t.Fatal("expected error from broken conn")
			}
			// Every helper wraps its underlying query failure with a
			// "failed to ..." prefix; assert the wrap so a fixture
			// regression that produced a different early error would
			// surface here instead of going green.
			if !strings.Contains(err.Error(), "failed to") {
				t.Errorf("expected wrapped helper error, got %v", err)
			}
		})
	}
}

func TestProbeExecute_QueryError(t *testing.T) {
	pool := requireIntegrationPool(t)

	// Each probe is invoked against a freshly closed connection so the
	// Query call inside Execute returns a wrapped error.
	cases := []struct {
		name string
		run  func(ctx context.Context, c *pgxpool.Conn) error
	}{
		{"pg_database", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgDatabaseProbe(&ProbeConfig{Name: ProbeNamePgDatabase})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_activity", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatActivityProbe(&ProbeConfig{Name: ProbeNamePgStatActivity})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_database", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatDatabaseProbe(&ProbeConfig{Name: ProbeNamePgStatDatabase})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_database_conflicts", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatDatabaseConflictsProbe(&ProbeConfig{Name: ProbeNamePgStatDatabaseConflicts})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_all_tables", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatAllTablesProbe(&ProbeConfig{Name: ProbeNamePgStatAllTables})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_all_indexes", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatAllIndexesProbe(&ProbeConfig{Name: ProbeNamePgStatAllIndexes})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_statio_all_sequences", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatioAllSequencesProbe(&ProbeConfig{Name: ProbeNamePgStatioAllSequences})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_user_functions", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatUserFunctionsProbe(&ProbeConfig{Name: ProbeNamePgStatUserFunctions})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_extension", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgExtensionProbe(&ProbeConfig{Name: ProbeNamePgExtension})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_hba_file_rules", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgHbaFileRulesProbe(&ProbeConfig{Name: ProbeNamePgHbaFileRules})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_settings", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSettingsProbe(&ProbeConfig{Name: ProbeNamePgSettings})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_connectivity", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgConnectivityProbe(&ProbeConfig{Name: ProbeNamePgConnectivity})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_server_info", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgServerInfoProbe(&ProbeConfig{Name: ProbeNamePgServerInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_node_role", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_replication", func(ctx context.Context, c *pgxpool.Conn) error {
			// pg_stat_replication's Execute starts with checkIsInRecovery,
			// which fails first on a closed connection.
			p := NewPgStatReplicationProbe(&ProbeConfig{Name: ProbeNamePgStatReplication})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_subscription", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatSubscriptionProbe(&ProbeConfig{Name: ProbeNamePgStatSubscription})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_checkpointer", func(ctx context.Context, c *pgxpool.Conn) error {
			// checkpointer uses cachedCheck; a unique probe name avoids
			// cache collisions with other tests.
			p := NewPgStatCheckpointerProbe(&ProbeConfig{Name: ProbeNamePgStatCheckpointer})
			_, err := p.Execute(ctx,
				"err-ckpt-"+time.Now().Format(time.RFC3339Nano), c, 16)
			return err
		}},
		{"pg_stat_io", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatIOProbe(&ProbeConfig{Name: ProbeNamePgStatIO})
			_, err := p.Execute(ctx,
				"err-io-"+time.Now().Format(time.RFC3339Nano), c, 16)
			return err
		}},
		{"pg_stat_wal", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatWalProbe(&ProbeConfig{Name: ProbeNamePgStatWAL})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_stat_recovery_prefetch", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatRecoveryPrefetchProbe(&ProbeConfig{Name: ProbeNamePgStatRecoveryPrefetch})
			_, err := p.Execute(ctx,
				"err-rp-"+time.Now().Format(time.RFC3339Nano), c, 16)
			return err
		}},
		{"pg_stat_connection_security", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatConnectionSecurityProbe(&ProbeConfig{Name: ProbeNamePgStatConnectionSecurity})
			_, err := p.Execute(ctx,
				"err-sec-"+time.Now().Format(time.RFC3339Nano), c, 16)
			return err
		}},
		{"pg_replication_slots", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgReplicationSlotsProbe(&ProbeConfig{Name: ProbeNamePgReplicationSlots})
			_, err := p.Execute(ctx,
				"err-slot-"+time.Now().Format(time.RFC3339Nano), c, 16)
			return err
		}},
		{"pg_ident_file_mappings", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgIdentFileMappingsProbe(&ProbeConfig{Name: ProbeNamePgIdentFileMappings})
			_, err := p.Execute(ctx,
				"err-id-"+time.Now().Format(time.RFC3339Nano), c, 16)
			return err
		}},
		{"pg_stat_statements", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatStatementsProbe(&ProbeConfig{Name: ProbeNamePgStatStatements})
			_, err := p.Execute(ctx,
				"err-stmts-"+time.Now().Format(time.RFC3339Nano), c, 16)
			return err
		}},

		// pg_sys_* probes: Execute begins with CheckExtensionExists,
		// which queries pg_extension and fails on the broken conn.
		{"pg_sys_os_info", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysOsInfoProbe(&ProbeConfig{Name: ProbeNamePgSysOsInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_cpu_info", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysCPUInfoProbe(&ProbeConfig{Name: ProbeNamePgSysCPUInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_cpu_usage", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysCPUUsageInfoProbe(&ProbeConfig{Name: ProbeNamePgSysCPUUsageInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_memory", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysMemoryInfoProbe(&ProbeConfig{Name: ProbeNamePgSysMemoryInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_io_analysis", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysIoAnalysisInfoProbe(&ProbeConfig{Name: ProbeNamePgSysIoAnalysisInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_disk", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysDiskInfoProbe(&ProbeConfig{Name: ProbeNamePgSysDiskInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_load_avg", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysLoadAvgInfoProbe(&ProbeConfig{Name: ProbeNamePgSysLoadAvgInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_process", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysProcessInfoProbe(&ProbeConfig{Name: ProbeNamePgSysProcessInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_network", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysNetworkInfoProbe(&ProbeConfig{Name: ProbeNamePgSysNetworkInfo})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
		{"pg_sys_cpu_memory_by_process", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysCPUMemoryByProcessProbe(&ProbeConfig{Name: ProbeNamePgSysCPUMemoryByProcess})
			_, err := p.Execute(ctx, "x", c, 16)
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			defer conn.Release()
			breakConn(t, conn)

			err = tc.run(ctx, conn)
			if err == nil {
				t.Fatal("expected error from broken conn")
			}
			// Every Execute path wraps its underlying failure with a
			// known prefix: "failed to ..." for the bulk of probes
			// (failed to execute query, failed to check ..., etc.)
			// or a probe-specific phrase such as "connectivity
			// check failed" for the connectivity probe. Assert the
			// wrap so a fixture regression that produced a
			// different early error would surface here.
			msg := err.Error()
			if !strings.Contains(msg, "failed to") &&
				!strings.Contains(msg, "connectivity check failed") {
				t.Errorf("expected wrapped Execute error, got %v", err)
			}
		})
	}
}

// TestProbeStore_PartitionError exercises the EnsurePartition error
// branch in each probe's Store method by closing the underlying
// connection before invoking Store with non-empty metrics. Every
// probe is expected to surface the partition error.
func TestProbeStore_PartitionError(t *testing.T) {
	pool := requireIntegrationPool(t)

	now := time.Now().UTC()
	cases := []struct {
		name string
		run  func(ctx context.Context, c *pgxpool.Conn) error
	}{
		{"pg_database", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgDatabaseProbe(&ProbeConfig{Name: ProbeNamePgDatabase})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"datname": "x"}})
		}},
		{"pg_stat_activity", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatActivityProbe(&ProbeConfig{Name: ProbeNamePgStatActivity})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"pid": int64(1)}})
		}},
		{"pg_replication_slots", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgReplicationSlotsProbe(&ProbeConfig{Name: ProbeNamePgReplicationSlots})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"slot_name": "s"}})
		}},
		{"pg_stat_recovery_prefetch", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatRecoveryPrefetchProbe(&ProbeConfig{Name: ProbeNamePgStatRecoveryPrefetch})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"prefetch": int64(0)}})
		}},
		{"pg_stat_connection_security", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatConnectionSecurityProbe(&ProbeConfig{Name: ProbeNamePgStatConnectionSecurity})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"pid": int64(1)}})
		}},
		{"pg_stat_io", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatIOProbe(&ProbeConfig{Name: ProbeNamePgStatIO})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"backend_type": "x"}})
		}},
		{"pg_stat_checkpointer", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatCheckpointerProbe(&ProbeConfig{Name: ProbeNamePgStatCheckpointer})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"num_timed": int64(0)}})
		}},
		{"pg_stat_wal", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatWalProbe(&ProbeConfig{Name: ProbeNamePgStatWAL})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"wal_records": int64(0)}})
		}},
		{"pg_stat_database", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatDatabaseProbe(&ProbeConfig{Name: ProbeNamePgStatDatabase})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"_database_name": "x"}})
		}},
		{"pg_stat_database_conflicts", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatDatabaseConflictsProbe(&ProbeConfig{Name: ProbeNamePgStatDatabaseConflicts})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"_database_name": "x"}})
		}},
		{"pg_stat_all_tables", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatAllTablesProbe(&ProbeConfig{Name: ProbeNamePgStatAllTables})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"_database_name": "x"}})
		}},
		{"pg_stat_all_indexes", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatAllIndexesProbe(&ProbeConfig{Name: ProbeNamePgStatAllIndexes})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"_database_name": "x"}})
		}},
		{"pg_statio_all_sequences", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatioAllSequencesProbe(&ProbeConfig{Name: ProbeNamePgStatioAllSequences})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"_database_name": "x"}})
		}},
		{"pg_stat_user_functions", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatUserFunctionsProbe(&ProbeConfig{Name: ProbeNamePgStatUserFunctions})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"_database_name": "x"}})
		}},
		{"pg_stat_statements", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatStatementsProbe(&ProbeConfig{Name: ProbeNamePgStatStatements})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"_database_name": "x", "queryid": int64(1)}})
		}},
		{"pg_node_role", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"primary_role": "standalone"}})
		}},
		{"pg_stat_replication", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatReplicationProbe(&ProbeConfig{Name: ProbeNamePgStatReplication})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"role": "primary"}})
		}},
		{"pg_stat_subscription", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgStatSubscriptionProbe(&ProbeConfig{Name: ProbeNamePgStatSubscription})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"subname": "s"}})
		}},
		{"pg_extension", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgExtensionProbe(&ProbeConfig{Name: ProbeNamePgExtension})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"_database_name": "x", "extname": "plpgsql"}})
		}},
		{"pg_hba_file_rules", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgHbaFileRulesProbe(&ProbeConfig{Name: ProbeNamePgHbaFileRules})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"rule_number": int64(1)}})
		}},
		{"pg_ident_file_mappings", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgIdentFileMappingsProbe(&ProbeConfig{Name: ProbeNamePgIdentFileMappings})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"map_number": int64(1)}})
		}},
		{"pg_settings", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSettingsProbe(&ProbeConfig{Name: ProbeNamePgSettings})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"name": "x"}})
		}},
		{"pg_server_info", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgServerInfoProbe(&ProbeConfig{Name: ProbeNamePgServerInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"server_version": "x"}})
		}},
		{"pg_connectivity", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgConnectivityProbe(&ProbeConfig{Name: ProbeNamePgConnectivity})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"response_time_ms": 1.0}})
		}},

		// pg_sys_* probes: Store path begins with EnsurePartition,
		// which fails on a broken connection.
		{"pg_sys_os_info", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysOsInfoProbe(&ProbeConfig{Name: ProbeNamePgSysOsInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"name": "x"}})
		}},
		{"pg_sys_cpu_info", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysCPUInfoProbe(&ProbeConfig{Name: ProbeNamePgSysCPUInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"vendor": "x"}})
		}},
		{"pg_sys_cpu_usage", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysCPUUsageInfoProbe(&ProbeConfig{Name: ProbeNamePgSysCPUUsageInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"idle_mode_percent": 99.0}})
		}},
		{"pg_sys_memory", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysMemoryInfoProbe(&ProbeConfig{Name: ProbeNamePgSysMemoryInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"total_memory": int64(1)}})
		}},
		{"pg_sys_io_analysis", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysIoAnalysisInfoProbe(&ProbeConfig{Name: ProbeNamePgSysIoAnalysisInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"device_name": "x"}})
		}},
		{"pg_sys_disk", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysDiskInfoProbe(&ProbeConfig{Name: ProbeNamePgSysDiskInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"mount_point": "/"}})
		}},
		{"pg_sys_load_avg", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysLoadAvgInfoProbe(&ProbeConfig{Name: ProbeNamePgSysLoadAvgInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"load_avg_one_minute": 0.1}})
		}},
		{"pg_sys_process", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysProcessInfoProbe(&ProbeConfig{Name: ProbeNamePgSysProcessInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"total_processes": int64(1)}})
		}},
		{"pg_sys_network", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysNetworkInfoProbe(&ProbeConfig{Name: ProbeNamePgSysNetworkInfo})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"interface_name": "eth0"}})
		}},
		{"pg_sys_cpu_memory_by_process", func(ctx context.Context, c *pgxpool.Conn) error {
			p := NewPgSysCPUMemoryByProcessProbe(&ProbeConfig{Name: ProbeNamePgSysCPUMemoryByProcess})
			return p.Store(ctx, c, 1, now,
				[]map[string]any{{"pid": int64(1)}})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			defer conn.Release()
			breakConn(t, conn)

			err = tc.run(ctx, conn)
			if err == nil {
				t.Fatal("expected error from broken conn")
			}
			// Every probe Store path begins with EnsurePartition and
			// wraps its failure with "failed to ensure partition" (or
			// the per-row "failed to insert ..." wrap). Assert the
			// wrap so a fixture regression that produced a different
			// early error would surface here.
			if !strings.Contains(err.Error(), "failed to") {
				t.Errorf("expected wrapped Store error, got %v", err)
			}
		})
	}
}
