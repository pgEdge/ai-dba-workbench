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
)

// These tests run each probe that collects a credential or statement
// text against a temporary view seeded with a secret, and fail if the
// secret survives Execute, so dropping the redaction call from a probe
// is caught (GitHub issue #643). The redaction rules themselves are
// tested in pkg/pgsettings and pkg/sqlredact.

// wiringSecret is the fake password seeded into every view.
const wiringSecret = "S3CRET"

// requireNoSecret fails the test if any value in rows mentions
// wiringSecret.
func requireNoSecret(t *testing.T, rows []map[string]any, err error, wantRows int) {
	t.Helper()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(rows) != wantRows {
		t.Fatalf("Execute() returned %d rows, want %d: %v", len(rows), wantRows, rows)
	}
	for _, row := range rows {
		for col, v := range row {
			if strings.Contains(fmt.Sprint(v), wiringSecret) {
				t.Errorf("column %s leaks the secret: %v", col, v)
			}
		}
	}
}

func TestPgSettingsProbe_ExecuteRedactsCredentials(t *testing.T) {
	conn := shadowViews(t, `
        CREATE TEMP VIEW pg_settings AS SELECT
            v.name, v.setting, NULL::text AS unit, 'Replication'::text AS category,
            ''::text AS short_desc, NULL::text AS extra_desc,
            'sighup'::text AS context, 'string'::text AS vartype,
            'configuration file'::text AS source, NULL::text AS min_val,
            NULL::text AS max_val, NULL::text[] AS enumvals,
            v.setting AS boot_val, v.setting AS reset_val,
            NULL::text AS sourcefile, NULL::integer AS sourceline,
            false AS pending_restart
        FROM (VALUES
            ('primary_conninfo', 'host=192.0.2.1 user=rep password=S3CRET'),
            ('restore_command', 'PGPASSWORD=S3CRET cp %f %p'),
            ('work_mem', '4096')) AS v(name, setting)`)
	p := NewPgSettingsProbe(&ProbeConfig{Name: ProbeNamePgSettings})

	rows, err := p.Execute(context.Background(), "settings-redaction", conn, 16)
	requireNoSecret(t, rows, err, 3)
	for _, row := range rows {
		if row["name"] == "primary_conninfo" &&
			!strings.Contains(fmt.Sprint(row["setting"]), "host=192.0.2.1") {
			t.Errorf("primary_conninfo lost more than its password: %v", row["setting"])
		}
		if row["name"] == "work_mem" && row["setting"] != "4096" {
			t.Errorf("work_mem = %v, want 4096", row["setting"])
		}
	}
}

func TestPgStatActivityProbe_ExecuteRedactsQuery(t *testing.T) {
	conn := shadowViews(t, `
        CREATE TEMP VIEW pg_stat_activity AS SELECT
            NULL::oid AS datid, NULL::name AS datname, 0 AS pid,
            NULL::integer AS leader_pid, NULL::oid AS usesysid,
            NULL::name AS usename, ''::text AS application_name,
            NULL::inet AS client_addr, NULL::text AS client_hostname,
            NULL::integer AS client_port, now() AS backend_start,
            NULL::timestamptz AS xact_start, now() AS query_start,
            now() AS state_change, NULL::text AS wait_event_type,
            NULL::text AS wait_event, 'idle'::text AS state,
            NULL::xid AS backend_xid, NULL::xid AS backend_xmin,
            'ALTER ROLE app PASSWORD ''S3CRET'''::text AS query,
            NULL::bigint AS query_id, 'client backend'::text AS backend_type`)
	p := NewPgStatActivityProbe(&ProbeConfig{Name: ProbeNamePgStatActivity})

	rows, err := p.Execute(context.Background(), "activity-redaction", conn, 16)
	requireNoSecret(t, rows, err, 1)
}

func TestPgStatStatementsProbe_ExecuteRedactsQuery(t *testing.T) {
	conn := shadowViews(t, `
        CREATE TEMP VIEW pg_stat_statements AS SELECT
            10::oid AS userid, 1::oid AS dbid, 42::bigint AS queryid,
            'CREATE ROLE app PASSWORD ''S3CRET'''::text AS query,
            1::bigint AS calls, 1::double precision AS total_exec_time,
            1::double precision AS mean_exec_time,
            1::double precision AS min_exec_time,
            1::double precision AS max_exec_time,
            0::double precision AS stddev_exec_time, 0::bigint AS rows,
            0::bigint AS shared_blks_hit, 0::bigint AS shared_blks_read,
            0::bigint AS shared_blks_dirtied, 0::bigint AS shared_blks_written,
            0::bigint AS local_blks_hit, 0::bigint AS local_blks_read,
            0::bigint AS local_blks_dirtied, 0::bigint AS local_blks_written,
            0::bigint AS temp_blks_read, 0::bigint AS temp_blks_written`)
	const connName = "statements-redaction"
	seedFeature(t, connName, conn, "pg_stat_statements_ext", true)
	seedFeature(t, connName, conn, "pg_stat_statements_shared_blk_time", false)
	seedFeature(t, connName, conn, "pg_stat_statements_blk_read_time", false)
	seedFeature(t, connName, conn, "pg_stat_statements_info_view", false)
	p := NewPgStatStatementsProbe(&ProbeConfig{Name: ProbeNamePgStatStatements})

	rows, err := p.Execute(context.Background(), connName, conn, 16)
	requireNoSecret(t, rows, err, 1)
}

func TestSpockExceptionLogProbe_ExecuteRedactsStatementText(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// The probe names spock.exception_log with its schema, so a
	// temporary view cannot stand in for it. The schema is created in a
	// transaction that is rolled back, leaving nothing behind.
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "ROLLBACK"); err != nil {
			t.Errorf("ROLLBACK: %v", err)
		}
	})
	var exists bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = 'spock')",
	).Scan(&exists); err != nil {
		t.Fatalf("check spock schema: %v", err)
	}
	if exists {
		t.Skip("a spock schema already exists")
	}
	for _, s := range []string{
		`CREATE TEMP VIEW pg_extension AS SELECT 'spock'::name AS extname`,
		`CREATE SCHEMA spock`,
		`CREATE VIEW spock.exception_log AS SELECT
            16384::oid AS remote_origin, now() AS remote_commit_ts,
            1 AS command_counter, now() AS retry_errored_at,
            12345::bigint AS remote_xid, NULL::oid AS local_origin,
            NULL::timestamptz AS local_commit_ts, NULL::text AS table_schema,
            NULL::text AS table_name, 'DDL'::text AS operation,
            NULL::jsonb AS local_tup, NULL::jsonb AS remote_old_tup,
            NULL::jsonb AS remote_new_tup,
            'ALTER ROLE app PASSWORD ''S3CRET'''::text AS ddl_statement,
            'postgres'::text AS ddl_user,
            'failed to apply ALTER ROLE app PASSWORD ''S3CRET'''::text AS error_message`,
	} {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("setup: %v\nSQL: %s", err, s)
		}
	}
	p := newSpockExceptionLogProbeForTest()

	rows, err := p.Execute(ctx, "spock-redaction", conn, 16)
	requireNoSecret(t, rows, err, 1)
}
