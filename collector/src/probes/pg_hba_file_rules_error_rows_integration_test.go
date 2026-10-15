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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/database"
)

// hbaFixtureView stands in for pg_catalog.pg_hba_file_rules with the
// rows PostgreSQL 16 and later return for a pg_hba.conf holding two
// lines it cannot parse: rule_number is NULL on those rows and the
// error column holds the message, which quotes the line. The tests
// cannot edit the server's real pg_hba.conf, so the view is created in
// its own schema and the monitored connection's search_path puts that
// schema ahead of pg_catalog, so the probe's own query reads it.
//
// The error at line 3 of pg_hba.conf would collide with rule 3 if the
// line number were the key, and so would the error at line 3 of the
// included file; both quote a credential in the forms PostgreSQL 18
// reports.
const hbaFixtureView = `
CREATE SCHEMA hba_fixture;
CREATE VIEW hba_fixture.pg_hba_file_rules AS
SELECT * FROM (VALUES
    (1, '/etc/postgresql/pg_hba.conf', 1, 'local',
     ARRAY['all'], ARRAY['all'], NULL::text, NULL::text,
     'trust', NULL::text[], NULL::text),
    (2, '/etc/postgresql/pg_hba.conf', 2, 'host',
     ARRAY['all'], ARRAY['all'], '192.0.2.0', '255.255.255.0',
     'scram-sha-256', NULL::text[], NULL::text),
    (NULL, '/etc/postgresql/pg_hba.conf', 3, NULL,
     NULL::text[], NULL::text[], NULL::text, NULL::text,
     NULL, NULL::text[],
     'authentication option not in name=value format: ldapbindpasswdS3cretValue'),
    (3, '/etc/postgresql/pg_hba.conf', 4, 'host',
     ARRAY['all'], ARRAY['all'], '198.51.100.0', '255.255.255.0',
     'ldap', ARRAY['ldapserver=ldap.example.com', 'ldapprefix=cn='], NULL::text),
    (NULL, '/etc/postgresql/extra.conf', 3, NULL,
     NULL::text[], NULL::text[], NULL::text, NULL::text,
     NULL, NULL::text[],
     'invalid IP address "ldapbindpasswd=S3cretValue": Name or service not known')
) AS v(rule_number, file_name, line_number, type, database, user_name,
       address, netmask, auth_method, options, error);
`

// newMigratedProbeDatastore creates a database with the collector's
// full schema, including the real primary key on
// metrics.pg_hba_file_rules that the shared probe test schema lacks,
// and returns a pool on it. The database is dropped when the test ends.
func newMigratedProbeDatastore(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base, ok := integrationConnString()
	if !ok {
		t.Skip("TEST_AI_WORKBENCH_SERVER (or TEST_DB_CONN) not set; " +
			"skipping integration test")
	}
	ctx := context.Background()

	admin, err := newProbeAdminPool(ctx, replaceProbeDatabase(base, "postgres"))
	if err != nil {
		t.Fatalf("connect to admin db: %v", err)
	}
	t.Cleanup(admin.Close)

	name := fmt.Sprintf("ai_workbench_probes_hba_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatalf("create test db: %v", err)
	}
	// FORCE ends any backend still attached, so the drop cannot fail
	// and leak the database (issue #486). Cleanups run last in, first
	// out, so this runs after the pool below is closed.
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			t.Logf("dropping test database %s: %v", name, err)
		}
	})

	cfg, err := pgxpool.ParseConfig(replaceProbeDatabase(base, name))
	if err != nil {
		t.Fatalf("parse test db config: %v", err)
	}
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to test db: %v", err)
	}
	t.Cleanup(pool.Close)

	// Release the connection straight away: the test needs both of the
	// pool's connections.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()
	if err := database.NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return pool
}

// TestPgHbaFileRulesProbe_StoresErrorRowsIntegration is the regression
// test for issue #680: on PostgreSQL 16 and later a line PostgreSQL
// cannot parse has a NULL rule_number, which used to fail the insert of
// the whole snapshot. Both the error rows and the valid rows must now
// be stored, under keys that cannot collide, with the values quoted in
// the error masked.
func TestPgHbaFileRulesProbe_StoresErrorRowsIntegration(t *testing.T) {
	pool := newMigratedProbeDatastore(t)
	ctx := context.Background()

	datastoreConn := acquireConn(t, pool)
	monitoredConn := acquireConn(t, pool)

	if _, err := datastoreConn.Exec(ctx, hbaFixtureView); err != nil {
		t.Fatalf("create fixture view: %v", err)
	}
	if _, err := monitoredConn.Exec(ctx,
		"SET search_path TO hba_fixture, pg_catalog"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	var connectionID int
	if err := datastoreConn.QueryRow(ctx, `
		INSERT INTO connections
		    (name, host, port, database_name, username, owner_username)
		VALUES ('hba-errors', 'db.example.com', 5432, 'postgres',
		        'monitor', 'tester')
		RETURNING id`).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	p := NewPgHbaFileRulesProbe(&ProbeConfig{
		Name:                      ProbeNamePgHbaFileRules,
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
	})

	// The fixture has the PostgreSQL 16 columns whatever the server's
	// version, so the PG16+ query is the one under test.
	metrics, err := p.Execute(ctx, "hba-errors", monitoredConn, 16)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) != 5 {
		t.Fatalf("Execute returned %d rows, want the fixture's 5", len(metrics))
	}

	collectedAt := time.Now().UTC()
	if err := p.Store(ctx, datastoreConn, connectionID, collectedAt, metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}

	type storedRule struct {
		ruleNumber int
		fileName   string
		lineNumber int
		authMethod *string
		errText    *string
	}
	rows, err := datastoreConn.Query(ctx, `
		SELECT rule_number, file_name, line_number, auth_method, error
		FROM metrics.pg_hba_file_rules
		WHERE connection_id = $1
		ORDER BY rule_number`, connectionID)
	if err != nil {
		t.Fatalf("read stored rules: %v", err)
	}
	var got []storedRule
	for rows.Next() {
		var r storedRule
		if err := rows.Scan(&r.ruleNumber, &r.fileName, &r.lineNumber,
			&r.authMethod, &r.errText); err != nil {
			rows.Close()
			t.Fatalf("scan stored rule: %v", err)
		}
		got = append(got, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read stored rules: %v", err)
	}

	str := func(s string) *string { return &s }
	want := []storedRule{
		{-2, "/etc/postgresql/pg_hba.conf", 3, nil,
			str("authentication option not in name=value format: ********")},
		{-1, "/etc/postgresql/extra.conf", 3, nil,
			str(`invalid IP address "********"`)},
		{1, "/etc/postgresql/pg_hba.conf", 1, str("trust"), nil},
		{2, "/etc/postgresql/pg_hba.conf", 2, str("scram-sha-256"), nil},
		{3, "/etc/postgresql/pg_hba.conf", 4, str("ldap"), nil},
	}
	if len(got) != len(want) {
		t.Fatalf("stored %d rules, want %d: %+v", len(got), len(want), got)
	}
	deref := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ruleNumber != w.ruleNumber || g.fileName != w.fileName ||
			g.lineNumber != w.lineNumber ||
			deref(g.authMethod) != deref(w.authMethod) ||
			deref(g.errText) != deref(w.errText) {
			t.Errorf("rule %d = {%d %s %d %s %q}, want {%d %s %d %s %q}", i,
				g.ruleNumber, g.fileName, g.lineNumber, deref(g.authMethod), deref(g.errText),
				w.ruleNumber, w.fileName, w.lineNumber, deref(w.authMethod), deref(w.errText))
		}
	}

	var leaked int
	if err := datastoreConn.QueryRow(ctx, `
		SELECT count(*) FROM metrics.pg_hba_file_rules
		WHERE connection_id = $1 AND error LIKE '%S3cret%'`,
		connectionID).Scan(&leaked); err != nil {
		t.Fatalf("check for the credential: %v", err)
	}
	if leaked != 0 {
		t.Errorf("%d stored errors still quote the credential", leaked)
	}

	// A second collection of the same file must match the stored,
	// masked snapshot, so no new snapshot is written.
	again, err := p.Execute(ctx, "hba-errors", monitoredConn, 16)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if err := p.Store(ctx, datastoreConn, connectionID,
		collectedAt.Add(time.Hour), again); err != nil {
		t.Fatalf("second Store: %v", err)
	}
	var snapshots int
	if err := datastoreConn.QueryRow(ctx, `
		SELECT count(DISTINCT collected_at) FROM metrics.pg_hba_file_rules
		WHERE connection_id = $1`, connectionID).Scan(&snapshots); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if snapshots != 1 {
		t.Errorf("stored %d snapshots of an unchanged file, want 1", snapshots)
	}

	// Two rows under one key still break the primary key, and the
	// failure is reported rather than lost.
	dup := []map[string]any{
		{"rule_number": int32(1), "line_number": int32(1), "auth_method": "trust"},
		{"rule_number": int32(1), "line_number": int32(2), "auth_method": "trust"},
	}
	err = p.Store(ctx, datastoreConn, connectionID, collectedAt.Add(2*time.Hour), dup)
	if err == nil || !strings.Contains(err.Error(), "failed to store metrics") {
		t.Errorf("Store with a duplicate key = %v, want a store error", err)
	}

	// The probe must not have changed the rows it was given.
	for _, m := range metrics {
		if e, ok := m["error"].(string); ok && !strings.Contains(e, "S3cret") {
			t.Errorf("Store changed the caller's error value to %q", e)
		}
	}
}
