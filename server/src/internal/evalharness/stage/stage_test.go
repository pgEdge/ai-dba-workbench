/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package stage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/incident"
	"github.com/pgedge/ai-workbench/server/internal/testutil/pkgdb"
)

// TestMain gives the package a database of its own; see pkgdb. Each
// test stages incidents into further databases on the same instance,
// with unique names, and drops them again.
func TestMain(m *testing.M) {
	os.Exit(pkgdb.Run(m, "evalstage"))
}

// datastoreDDL creates stand-ins for the parts of the Workbench
// datastore the stager writes to.
const datastoreDDL = `
CREATE TABLE IF NOT EXISTS alerts (
    id bigserial PRIMARY KEY, alert_type text NOT NULL, connection_id integer NOT NULL,
    database_name text, probe_name text, metric_name text, metric_value real,
    threshold_value real, operator text,
    severity text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')), title text NOT NULL,
    description text NOT NULL, object_name text, status text NOT NULL,
    triggered_at timestamptz NOT NULL, last_updated timestamptz);
CREATE SCHEMA IF NOT EXISTS metrics;
CREATE TABLE IF NOT EXISTS metrics.part_metric (
    connection_id integer NOT NULL, value bigint, collected_at timestamptz NOT NULL
) PARTITION BY RANGE (collected_at);
CREATE TABLE IF NOT EXISTS metrics.plain_metric (
    connection_id integer NOT NULL, label text, amount bigint, collected_at timestamptz NOT NULL);
CREATE TABLE IF NOT EXISTS teardown_log (note text);
`

var (
	setupOnce sync.Once
	setupErr  error
)

// testDSN returns the package database, with the stand-in datastore
// tables created, or skips the test when no database is configured.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(pkgdb.EnvVar)
	if dsn == "" {
		t.Skipf("%s is not set", pkgdb.EnvVar)
	}
	setupOnce.Do(func() {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			setupErr = err
			return
		}
		defer conn.Close(ctx)
		_, setupErr = conn.Exec(ctx, datastoreDDL)
	})
	if setupErr != nil {
		t.Fatalf("create stand-in datastore: %v", setupErr)
	}
	return dsn
}

var nextID atomic.Int32

// fakeRegistry records connections instead of calling the API.
type fakeRegistry struct {
	mu        sync.Mutex
	created   []ConnectionSpec
	deleted   []int
	createErr error
	deleteErr error
}

func (r *fakeRegistry) Create(_ context.Context, spec ConnectionSpec) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return 0, r.createErr
	}
	r.created = append(r.created, spec)
	return int(1000 + nextID.Add(1)), nil
}

func (r *fakeRegistry) Delete(_ context.Context, id int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, id)
	return r.deleteErr
}

// testConn connects to dsn for the length of the test. Callers run their
// literal queries on it directly.
func testConn(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return conn
}

// scanInt reads a single integer from row.
func scanInt(t *testing.T, row pgx.Row) int {
	t.Helper()
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func databaseExists(t *testing.T, dsn, name string) bool {
	return scanInt(t, testConn(t, dsn).QueryRow(context.Background(), "SELECT count(*) FROM pg_database WHERE datname = $1", name)) == 1
}

func float(v float64) *float64 { return &v }

func baseIncident(id string) *incident.Incident {
	return &incident.Incident{
		ID:    id,
		Setup: "CREATE TABLE stock (sku text PRIMARY KEY, qty integer);\nINSERT INTO stock VALUES ('a', 1);",
		Alert: incident.Alert{
			Title: "Blocked queries", Description: "  2 blocked  \n", Severity: "critical",
			Probe: "pg_stat_activity", Value: float(2), Threshold: float(1), Operator: ">",
			TriggeredAgo: incident.Duration(5 * time.Minute),
		},
	}
}

func TestStageAndTeardown(t *testing.T) {
	t.Parallel()
	dsn := testDSN(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	inc := baseIncident("stage-full")
	inc.Workload = []incident.Session{
		{Name: "holder", SQL: "SET application_name = 'holder';\nBEGIN;\nUPDATE stock SET qty = 2 WHERE sku = 'a';"},
		{Name: "waiter", Blocks: true, SQL: "UPDATE stock SET qty = 3 WHERE sku = 'a';"},
	}
	inc.Metrics = []incident.MetricSeries{
		{Table: "part_metric", Samples: []incident.Sample{
			{Ago: incident.Duration(30 * time.Minute), Values: map[string]any{"value": 4}},
			{Ago: incident.Duration(time.Minute), Values: map[string]any{"value": 9}},
		}},
		{Table: "plain_metric", Samples: []incident.Sample{{Values: map[string]any{"label": "x"}}}},
	}
	inc.Teardown = "INSERT INTO teardown_log VALUES ('stage-full');"

	reg := &fakeRegistry{}
	s := &Stager{AdminDSN: dsn, DatastoreDSN: dsn, Registry: reg, Now: func() time.Time { return now }}
	st, err := s.Stage(ctx, inc)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if !strings.HasPrefix(st.Database, "eval_stage_full_") || !strings.HasPrefix(st.ConnectionName, "eval-stage-full-") {
		t.Errorf("names %q / %q", st.Database, st.ConnectionName)
	}
	if !databaseExists(t, dsn, st.Database) {
		t.Fatal("staged database does not exist")
	}
	spec := reg.created[0]
	if spec.Database != st.Database || spec.SSLMode == "" || spec.Host == "" || spec.Port == 0 || spec.Name != st.ConnectionName {
		t.Errorf("connection spec %+v", spec)
	}
	if spec.Username != st.Role || len(spec.Password) != 32 || !strings.HasPrefix(st.Role, "eval_stage_full_") {
		t.Errorf("connection role %q, password length %d", spec.Username, len(spec.Password))
	}
	if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(), `SELECT count(*) FROM pg_roles r
        WHERE r.rolname = $1 AND r.rolcanlogin AND NOT r.rolsuper
          AND pg_has_role(r.oid, 'pg_monitor', 'MEMBER')
          AND pg_has_role(r.oid, 'pg_read_all_data', 'MEMBER')`, st.Role)); n != 1 {
		t.Errorf("found %d matching login roles", n)
	}
	if pd := st.PromptData(); pd.ConnectionID != st.ConnectionID || pd.ConnectionName != st.ConnectionName || pd.Database != st.Database {
		t.Errorf("PromptData = %+v", pd)
	}
	id := st.ConnectionID

	if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(), `SELECT count(*) FROM alerts WHERE connection_id = $1 AND database_name = $2
        AND description = '2 blocked' AND status = 'active' AND metric_name IS NULL
        AND triggered_at = $3`, id, st.Database, now.Add(-5*time.Minute))); n != 1 {
		t.Errorf("found %d matching alerts", n)
	}
	if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(), `SELECT count(*) FROM metrics.part_metric WHERE connection_id = $1
        AND ((value = 4 AND collected_at = $2) OR (value = 9 AND collected_at = $3))`,
		id, now.Add(-30*time.Minute), now.Add(-time.Minute))); n != 2 {
		t.Errorf("found %d partitioned metric rows", n)
	}
	if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(), "SELECT count(*) FROM metrics.plain_metric WHERE connection_id = $1 AND label = 'x'", id)); n != 1 {
		t.Errorf("found %d plain metric rows", n)
	}
	if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
        WHERE datname = $1 AND state = 'idle in transaction' AND application_name = 'holder'`, st.Database)); n != 1 {
		t.Errorf("found %d idle-in-transaction holders", n)
	}

	if err := st.Teardown(ctx); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if databaseExists(t, dsn, st.Database) {
		t.Error("staged database still exists")
	}
	if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(), "SELECT count(*) FROM pg_roles WHERE rolname = $1", st.Role)); n != 0 {
		t.Error("login role still exists")
	}
	if len(reg.deleted) != 1 || reg.deleted[0] != id {
		t.Errorf("deleted connections %v, want [%d]", reg.deleted, id)
	}
	if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(), "SELECT count(*) FROM teardown_log WHERE note = 'stage-full'")); n != 1 {
		t.Errorf("teardown SQL ran %d times", n)
	}
	// A second teardown has nothing left to do but the fixture's SQL.
	if err := st.Teardown(ctx); err != nil {
		t.Errorf("second Teardown: %v", err)
	}
}

func TestStageFailures(t *testing.T) {
	t.Parallel()
	dsn := testDSN(t)

	cases := []struct {
		name      string
		mutate    func(*incident.Incident, *Stager, *fakeRegistry)
		want      string
		registers bool
	}{
		{
			name: "setup error",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Setup = "SELECT 1;\nSELECT no_such_column FROM stock;"
			},
			want: "setup: SELECT no_such_column FROM stock",
		},
		{
			name:   "registry error",
			mutate: func(_ *incident.Incident, _ *Stager, r *fakeRegistry) { r.createErr = errors.New("api down") },
			want:   "register connection: api down",
		},
		{
			name: "datastore unreachable",
			mutate: func(_ *incident.Incident, s *Stager, _ *fakeRegistry) {
				s.DatastoreDSN = "postgres://127.0.0.1:1/x?connect_timeout=2"
			},
			want:      "connect to datastore",
			registers: true,
		},
		{
			name: "unknown metrics table",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Metrics = []incident.MetricSeries{{Table: "no_such", Samples: []incident.Sample{{Values: map[string]any{"a": 1}}}}}
			},
			want:      "metrics.no_such: no such table",
			registers: true,
		},
		{
			name: "unknown metrics column",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Metrics = []incident.MetricSeries{{Table: "plain_metric", Samples: []incident.Sample{{Values: map[string]any{"hue": 1}}}}}
			},
			want:      `unknown column "hue"`,
			registers: true,
		},
		{
			name: "bad metrics value",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Metrics = []incident.MetricSeries{{Table: "plain_metric", Samples: []incident.Sample{{Values: map[string]any{"amount": "lots"}}}}}
			},
			want:      "insert sample",
			registers: true,
		},
		{
			name: "unencodable metrics value",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Metrics = []incident.MetricSeries{{Table: "plain_metric", Samples: []incident.Sample{{Values: map[string]any{"label": func() {}}}}}}
			},
			want:      "encode sample",
			registers: true,
		},
		{
			name: "alert rejected",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Alert.Severity = "fatal"
			},
			want:      "insert alert",
			registers: true,
		},
		{
			name: "workload error",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Workload = []incident.Session{{Name: "broken", SQL: "SELECT * FROM no_such_table;"}}
			},
			want:      "workload broken: SELECT * FROM no_such_table",
			registers: true,
		},
		{
			name: "blocking session fails",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Workload = []incident.Session{{Name: "waiter", Blocks: true, SQL: "SELECT 1/0;"}}
			},
			want:      "workload waiter: SELECT 1/0",
			registers: true,
		},
		{
			name: "blocking session finishes",
			mutate: func(i *incident.Incident, _ *Stager, _ *fakeRegistry) {
				i.Workload = []incident.Session{{Name: "waiter", Blocks: true, SQL: "SELECT 1;"}}
			},
			want:      "workload waiter finished instead of waiting on a lock",
			registers: true,
		},
		{
			name: "blocking session never waits",
			mutate: func(i *incident.Incident, s *Stager, _ *fakeRegistry) {
				s.SettleTimeout = 300 * time.Millisecond
				i.Workload = []incident.Session{{Name: "sleeper", Blocks: true, SQL: "SELECT pg_sleep(30);"}}
			},
			want:      "only 0 of 1 blocking workload sessions were waiting",
			registers: true,
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inc := baseIncident(fmt.Sprintf("fail-%d", i))
			inc.Teardown = "INSERT INTO teardown_log VALUES ('" + strings.ReplaceAll(tc.name, " ", "-") + "');"
			reg := &fakeRegistry{}
			s := &Stager{AdminDSN: dsn, DatastoreDSN: dsn, Registry: reg}
			tc.mutate(inc, s, reg)

			st, err := s.Stage(ctx, inc)
			if err == nil {
				_ = st.Teardown(ctx)
				t.Fatal("Stage succeeded")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Stage error = %v, want it to contain %q", err, tc.want)
			}
			if got := len(reg.deleted) == 1; got != tc.registers {
				t.Errorf("connection deleted = %v, want %v (deleted %v)", got, tc.registers, reg.deleted)
			}
			if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(), "SELECT count(*) FROM teardown_log WHERE note = $1",
				strings.ReplaceAll(tc.name, " ", "-"))); n != 1 {
				t.Errorf("teardown SQL ran %d times", n)
			}
			if n := scanInt(t, testConn(t, dsn).QueryRow(context.Background(),
				`SELECT count(*) FROM pg_database WHERE datname LIKE 'eval\_fail\_' || $1::int || '\_%'`, i)); n != 0 {
				t.Errorf("staged database left behind")
			}
		})
	}
}

func TestTeardownReportsErrors(t *testing.T) {
	t.Parallel()
	dsn := testDSN(t)
	ctx := context.Background()
	reg := &fakeRegistry{deleteErr: errors.New("gone")}
	s := &Stager{AdminDSN: dsn, DatastoreDSN: dsn, Registry: reg}
	inc := baseIncident("stage-teardown")
	inc.Teardown = "SELECT no_such_function();"
	st, err := s.Stage(ctx, inc)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	// A schema the role owns stops DROP ROLE.
	adminCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ownedSchema := "SELECT format('CREATE SCHEMA %1$I AUTHORIZATION %1$I', $1::text)"
	if err := execGenerated(ctx, adminCfg, ownedSchema, st.Role); err != nil {
		t.Fatal(err)
	}
	role := st.Role
	t.Cleanup(func() {
		for _, q := range []string{"SELECT format('DROP SCHEMA %I', $1::text)", "SELECT format('DROP ROLE %I', $1::text)"} {
			if err := execGenerated(ctx, adminCfg, q, role); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	err = st.Teardown(ctx)
	for _, want := range []string{"delete connection", "gone", "drop role " + role, "teardown SQL: SELECT no_such_function()"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Teardown error = %v, want it to contain %q", err, want)
		}
	}
	if databaseExists(t, dsn, st.Database) {
		t.Error("database not dropped despite earlier teardown errors")
	}
}

func TestBadAdminDSN(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := &Stager{AdminDSN: "postgres://%zz", Registry: &fakeRegistry{}}
	if _, err := s.Stage(ctx, baseIncident("bad-dsn")); err == nil || !strings.Contains(err.Error(), "parse admin DSN") {
		t.Errorf("Stage error = %v", err)
	}
	s.AdminDSN = "postgres://postgres@127.0.0.1:1/x?connect_timeout=2"
	if _, err := s.Stage(ctx, baseIncident("bad-dsn")); err == nil || !strings.Contains(err.Error(), "create database") {
		t.Errorf("Stage error = %v", err)
	}
}

func TestFirstLine(t *testing.T) {
	t.Parallel()
	if got := firstLine("SELECT 1\nFROM t"); got != "SELECT 1" {
		t.Errorf("firstLine = %q", got)
	}
	if got := firstLine(strings.Repeat("x", 100)); got != strings.Repeat("x", 80)+"..." {
		t.Errorf("firstLine = %q", got)
	}
}
