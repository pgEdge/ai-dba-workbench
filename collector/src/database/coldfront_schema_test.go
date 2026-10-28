/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
)

// recordedStatement is one statement a recordingTx was asked to run.
type recordedStatement struct {
	migration int
	sql       string
	args      []any
}

// recordingTx stands in for a migration transaction without a database.
// It records every statement, answers QueryRow with a false boolean (the
// pgvector availability check is the only QueryRow the migrations make)
// and Query with no rows. execErr, when set, decides the error an Exec
// returns.
type recordingTx struct {
	pgx.Tx
	migration int
	stmts     []recordedStatement
	execErr   func(sql string) error
	// queryRow, when set, answers QueryRow instead of falseRow.
	queryRow func(sql string) pgx.Row
}

func (r *recordingTx) record(sql string, args []any) {
	r.stmts = append(r.stmts, recordedStatement{migration: r.migration, sql: sql, args: args})
}

func (r *recordingTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.record(sql, args)
	if r.execErr != nil {
		if err := r.execErr(sql); err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	return pgconn.CommandTag{}, nil
}

func (r *recordingTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	r.record(sql, args)
	if r.queryRow != nil {
		return r.queryRow(sql)
	}
	return falseRow{}
}

func (r *recordingTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	r.record(sql, args)
	return &noRows{}, nil
}

// falseRow scans false into a single *bool, which answers the pgvector
// availability check, and reports no row for anything else, which is
// how the embedding-column type probe sees a missing column.
type falseRow struct{}

func (falseRow) Scan(dest ...any) error {
	if len(dest) == 1 {
		if b, ok := dest[0].(*bool); ok {
			*b = false
			return nil
		}
	}
	return pgx.ErrNoRows
}

// noRows is an empty result set.
type noRows struct{}

func (*noRows) Close()                                       {}
func (*noRows) Err() error                                   { return nil }
func (*noRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (*noRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (*noRows) Next() bool                                   { return false }
func (*noRows) Scan(...any) error                            { return errors.New("no rows") }
func (*noRows) Values() ([]any, error)                       { return nil, errors.New("no rows") }
func (*noRows) RawValues() [][]byte                          { return nil }
func (*noRows) Conn() *pgx.Conn                              { return nil }

// runMigrationsRecorded runs every migration of sm against a
// recordingTx and returns what they issued.
func runMigrationsRecorded(t *testing.T, sm *SchemaManager) []recordedStatement {
	t.Helper()
	sort.Slice(sm.migrations, func(i, j int) bool {
		return sm.migrations[i].Version < sm.migrations[j].Version
	})
	tx := &recordingTx{}
	for _, m := range sm.migrations {
		tx.migration = m.Version
		if err := m.Up(tx); err != nil {
			t.Fatalf("migration %d failed: %v", m.Version, err)
		}
	}
	return tx.stmts
}

// statementsOf splits every recorded SQL text into its statements.
func statementsOf(recorded []recordedStatement) []recordedStatement {
	var out []recordedStatement
	for _, r := range recorded {
		for _, s := range splitSQLStatements(r.sql) {
			out = append(out, recordedStatement{migration: r.migration, sql: s, args: r.args})
		}
	}
	return out
}

var whitespace = regexp.MustCompile(`\s+`)

func squash(s string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
}

// TestColdFrontMigrationsIssueNoMetricsIndexOrPartitionDDL runs the whole
// migration chain under ColdFront and checks what reaches the datastore:
// every metrics table is created once through create_iceberg_table, in
// the migration that creates it, with the specification's columns and
// the day(collected_at) partition spec; no statement creates a metrics
// table, index, constraint or partition; nothing uses a SAVEPOINT or
// rewrites metrics rows; and the comments and later column additions
// are kept.
func TestColdFrontMigrationsIssueNoMetricsIndexOrPartitionDDL(t *testing.T) {
	recorded := runMigrationsRecorded(t, NewSchemaManager(WithColdFrontMetrics(true)))

	created := map[string]int{}
	for _, r := range recorded {
		if !strings.Contains(r.sql, "coldfront.create_iceberg_table") {
			continue
		}
		if len(r.args) != 3 {
			t.Fatalf("create_iceberg_table called with %d arguments", len(r.args))
		}
		table, ok := r.args[0].(string)
		if !ok {
			t.Fatalf("create_iceberg_table table argument is %T", r.args[0])
		}
		spec, ok := coldfront.Lookup(table)
		if !ok {
			t.Errorf("create_iceberg_table for unknown table %q", table)
			continue
		}
		if created[table] != 0 {
			t.Errorf("metrics.%s created twice", table)
		}
		created[table] = r.migration
		if r.migration != spec.CreatedIn {
			t.Errorf("metrics.%s created in migration %d, specification says %d", table, r.migration, spec.CreatedIn)
		}
		wantJSON, err := spec.ColumnsJSON()
		if err != nil {
			t.Fatal(err)
		}
		if r.args[1] != wantJSON {
			t.Errorf("metrics.%s p_columns = %v, want %s", table, r.args[1], wantJSON)
		}
		if !reflect.DeepEqual(r.args[2], []string{"day(collected_at)"}) {
			t.Errorf("metrics.%s p_partition_cols = %v", table, r.args[2])
		}
	}
	if len(created) != len(coldfront.Tables()) {
		t.Errorf("created %d metrics tables through ColdFront, specification has %d", len(created), len(coldfront.Tables()))
	}

	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`(?i)^CREATE TABLE.*\bmetrics\.`),
		regexp.MustCompile(`(?i)^CREATE (UNIQUE )?INDEX.*\bON metrics\.`),
		regexp.MustCompile(`(?i)^(COMMENT ON|DROP) INDEX.*\bmetrics\.`),
		// A metrics relation is a view: COMMENT ON TABLE fails on it.
		regexp.MustCompile(`(?i)^COMMENT ON TABLE metrics\.`),
		regexp.MustCompile(`(?i)^ALTER TABLE metrics\.\S+ (ADD CONSTRAINT|ALTER COLUMN|ADD PRIMARY)`),
		regexp.MustCompile(`(?i)\bPARTITION (BY|OF)\b`),
		regexp.MustCompile(`(?i)\bSAVEPOINT\b`),
		regexp.MustCompile(`(?i)^(UPDATE|DELETE FROM|TRUNCATE)\s+metrics\.`),
		regexp.MustCompile(`(?i)^SELECT DISTINCT \S+ FROM metrics\.`),
		regexp.MustCompile(`(?i)pg_available_extensions`),
		regexp.MustCompile(`(?i)CREATE EXTENSION.*\bvector\b|halfvec`),
	}
	stmts := statementsOf(recorded)
	for _, s := range stmts {
		flat := squash(s.sql)
		// addConstraintIfMissing issues a DO block naming the table.
		if strings.HasPrefix(flat, "DO $$") && strings.Contains(flat, "metrics.") {
			t.Errorf("migration %d adds a metrics constraint: %.120s", s.migration, flat)
		}
		for _, re := range forbidden {
			if re.MatchString(flat) {
				t.Errorf("migration %d issues forbidden statement under ColdFront: %.160s", s.migration, flat)
			}
		}
	}

	// Comments and later column additions still reach the views.
	required := []string{
		"COMMENT ON VIEW metrics.pg_stat_activity IS",
		"COMMENT ON VIEW metrics.spock_resolutions IS",
		"COMMENT ON COLUMN metrics.pg_node_role.postmaster_start_time IS",
		"COMMENT ON COLUMN metrics.spock_exception_log.database_name IS",
		"ALTER TABLE metrics.pg_stat_all_tables ADD COLUMN IF NOT EXISTS table_size BIGINT",
		"ALTER TABLE metrics.pg_stat_all_indexes ADD COLUMN IF NOT EXISTS index_size BIGINT",
		"ALTER TABLE metrics.pg_stat_statements ADD COLUMN IF NOT EXISTS stats_reset TIMESTAMPTZ",
		"ALTER TABLE metrics.pg_stat_activity ADD COLUMN IF NOT EXISTS query_id BIGINT",
		"ALTER TABLE metrics.pg_sys_memory_info ADD COLUMN IF NOT EXISTS available_memory BIGINT",
		"COMMENT ON COLUMN metrics.pg_settings.setting IS",
		"COMMENT ON COLUMN metrics.pg_stat_statements.query IS",
		"CREATE TABLE IF NOT EXISTS chat_memories",
		"CREATE TABLE IF NOT EXISTS connections",
	}
	for _, want := range required {
		found := false
		for _, s := range stmts {
			if strings.HasPrefix(squash(s.sql), want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no statement starting %q under ColdFront", want)
		}
	}
}

// TestDefaultMigrationsUnchanged checks that the default mode still
// issues the partitioned CREATE TABLE of every metrics table and its
// indexes, never calls ColdFront, and is what NewSchemaManager without
// options produces.
func TestDefaultMigrationsUnchanged(t *testing.T) {
	plain := runMigrationsRecorded(t, NewSchemaManager())
	explicit := runMigrationsRecorded(t, NewSchemaManager(WithColdFrontMetrics(false)))
	if !reflect.DeepEqual(plain, explicit) {
		t.Fatal("NewSchemaManager() and WithColdFrontMetrics(false) issue different statements")
	}

	all := ""
	for _, r := range plain {
		if strings.Contains(r.sql, "coldfront") {
			t.Errorf("migration %d mentions coldfront in the default mode", r.migration)
		}
		all += r.sql + "\n"
	}
	for _, tbl := range coldfront.Tables() {
		if !strings.Contains(all, "CREATE TABLE IF NOT EXISTS metrics."+tbl.Name+" (") {
			t.Errorf("default mode does not create metrics.%s", tbl.Name)
		}
	}
	for _, want := range []string{
		"PARTITION BY RANGE (collected_at)",
		"CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_identity_time",
		"fk_pg_stat_wal_connection_id",
		"SELECT EXISTS(SELECT 1 FROM pg_available_extensions WHERE name = 'vector')",
		"DROP INDEX IF EXISTS metrics.idx_pg_stat_all_indexes_conn_db_time",
		"UPDATE metrics.pg_hba_file_rules",
		"SELECT DISTINCT name FROM metrics.pg_settings",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("default mode no longer issues %q", want)
		}
	}
}

func TestSplitSQLStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{"empty", "  \n ", nil},
		{"single without semicolon", "SELECT 1", []string{"SELECT 1"}},
		{"two", "SELECT 1; SELECT 2;", []string{"SELECT 1", "SELECT 2"}},
		{"semicolon in literal", "COMMENT ON TABLE t IS 'a; b';", []string{"COMMENT ON TABLE t IS 'a; b'"}},
		{"escaped quote", "COMMENT ON TABLE t IS 'it''s; fine'; SELECT 2", []string{"COMMENT ON TABLE t IS 'it''s; fine'", "SELECT 2"}},
		{"quoted identifier", `CREATE TABLE "a;b" (x int); SELECT 1`, []string{`CREATE TABLE "a;b" (x int)`, "SELECT 1"}},
		{"line comment", "-- note; here\nSELECT 1; -- trailing\n", []string{"SELECT 1"}},
		{"comment inside statement", "CREATE INDEX i\n-- why; because\nON t (x);", []string{"CREATE INDEX i\n\nON t (x)"}},
		{"unterminated literal", "SELECT 'abc", []string{"SELECT 'abc"}},
		{"single dash", "SELECT 1 - 2", []string{"SELECT 1 - 2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitSQLStatements(tt.sql); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitSQLStatements(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestStatementKind(t *testing.T) {
	tests := map[string]string{
		"CREATE TABLE IF NOT EXISTS metrics.x (a int)": "CREATE TABLE",
		"create  index i on t (a)":                     "CREATE INDEX",
		"CREATE UNIQUE INDEX i ON t (a)":               "CREATE UNIQUE INDEX",
		"COMMENT ON TABLE t IS 'x'":                    "COMMENT ON TABLE",
		"COMMENT\n ON COLUMN t.a IS 'x'":               "COMMENT ON COLUMN",
		"COMMENT ON INDEX i IS 'x'":                    "COMMENT ON INDEX",
		"ALTER TABLE t ADD COLUMN a int":               "ALTER",
		"":                                             "",
	}
	for in, want := range tests {
		if got := statementKind(in); got != want {
			t.Errorf("statementKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeDDLType(t *testing.T) {
	tests := map[string]string{
		"INTEGER":           "integer",
		"TIMESTAMPTZ":       "timestamp with time zone",
		"VARCHAR(255)":      "character varying(255)",
		"DOUBLE  PRECISION": "double precision",
		"TEXT[]":            "text[]",
		"INT8[]":            "bigint[]",
		`"char"`:            `"char"`,
		"NAME":              "name",
		"OID":               "oid",
		"NUMERIC":           "numeric",
		"BOOL":              "boolean",
	}
	for in, want := range tests {
		if got := normalizeDDLType(in); got != want {
			t.Errorf("normalizeDDLType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseCreateTableColumns(t *testing.T) {
	cols, err := parseCreateTableColumns(`CREATE TABLE IF NOT EXISTS metrics.x (
		connection_id INTEGER NOT NULL,
		name VARCHAR(255) DEFAULT 'a,b',
		flag BOOLEAN NOT NULL DEFAULT TRUE,
		amount NUMERIC(10, 2),
		tags TEXT[],
		collected_at TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (connection_id, collected_at),
		FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
	) PARTITION BY RANGE (collected_at)`)
	if err != nil {
		t.Fatal(err)
	}
	want := []coldfront.Column{
		{Name: "connection_id", PGType: "integer"},
		{Name: "name", PGType: "character varying(255)"},
		{Name: "flag", PGType: "boolean"},
		{Name: "amount", PGType: "numeric(10, 2)"},
		{Name: "tags", PGType: "text[]"},
		{Name: "collected_at", PGType: "timestamp with time zone"},
	}
	if !reflect.DeepEqual(cols, want) {
		t.Errorf("parseCreateTableColumns = %+v, want %+v", cols, want)
	}

	for _, bad := range []string{
		"CREATE TABLE metrics.x",
		"CREATE TABLE metrics.x (a int",
		"CREATE TABLE metrics.x (a NOT NULL)",
	} {
		if _, err := parseCreateTableColumns(bad); err == nil {
			t.Errorf("parseCreateTableColumns(%q) succeeded", bad)
		}
	}

	// An empty item, as a trailing comma would leave, is ignored.
	cols, err = parseCreateTableColumns("CREATE TABLE t (a int, )")
	if err != nil || len(cols) != 1 {
		t.Errorf("trailing comma: cols = %v, err = %v", cols, err)
	}
}

const connectivityDDL = `
	CREATE TABLE IF NOT EXISTS metrics.pg_connectivity (
		connection_id INTEGER NOT NULL,
		collected_at TIMESTAMPTZ NOT NULL,
		response_time_ms DOUBLE PRECISION NOT NULL,
		PRIMARY KEY (connection_id, collected_at)
	) PARTITION BY RANGE (collected_at);

	COMMENT ON TABLE metrics.pg_connectivity IS 'Connectivity; checks';

	CREATE INDEX IF NOT EXISTS idx_c ON metrics.pg_connectivity (connection_id);
	COMMENT ON INDEX metrics.idx_c IS 'x';
`

func TestExecMetricsDDLDefaultRunsBlockVerbatim(t *testing.T) {
	sm := &SchemaManager{}
	tx := &recordingTx{}
	if err := sm.execMetricsDDL(context.Background(), tx, "pg_connectivity", connectivityDDL); err != nil {
		t.Fatal(err)
	}
	if len(tx.stmts) != 1 || tx.stmts[0].sql != connectivityDDL {
		t.Errorf("default mode issued %+v, want the block verbatim", tx.stmts)
	}
}

func TestExecMetricsDDLColdFront(t *testing.T) {
	sm := &SchemaManager{useColdFrontForMetrics: true}
	tx := &recordingTx{}
	if err := sm.execMetricsDDL(context.Background(), tx, "pg_connectivity", connectivityDDL); err != nil {
		t.Fatal(err)
	}
	if len(tx.stmts) != 2 {
		t.Fatalf("issued %d statements, want 2: %+v", len(tx.stmts), tx.stmts)
	}
	if !strings.Contains(tx.stmts[0].sql, "coldfront.create_iceberg_table") {
		t.Errorf("first statement is %q", tx.stmts[0].sql)
	}
	if tx.stmts[1].sql != "COMMENT ON VIEW metrics.pg_connectivity IS 'Connectivity; checks'" {
		t.Errorf("second statement is %q", tx.stmts[1].sql)
	}
}

func TestExecMetricsDDLColdFrontErrors(t *testing.T) {
	createConnectivity := strings.SplitN(connectivityDDL, ";", 2)[0]
	errBoom := errors.New("boom")

	tests := []struct {
		name    string
		table   string
		ddl     string
		execErr func(string) error
		want    string
	}{
		{name: "unknown table", table: "not_a_table", ddl: createConnectivity, want: "no ColdFront column specification"},
		{name: "two creates", table: "pg_connectivity", ddl: createConnectivity + ";" + createConnectivity, want: "more than one CREATE TABLE"},
		{name: "no create", table: "pg_connectivity", ddl: "COMMENT ON TABLE metrics.pg_connectivity IS 'x'", want: "no CREATE TABLE"},
		{name: "unexpected statement", table: "pg_connectivity", ddl: createConnectivity + "; ALTER TABLE metrics.pg_connectivity ADD COLUMN x int", want: `unexpected "ALTER"`},
		{name: "drifted columns", table: "pg_connectivity", ddl: strings.Replace(createConnectivity, "DOUBLE PRECISION", "REAL", 1), want: "does not match the CREATE TABLE"},
		{name: "unparseable create", table: "pg_connectivity", ddl: "CREATE TABLE metrics.pg_connectivity", want: "no column list"},
		{
			name: "create fails", table: "pg_connectivity", ddl: createConnectivity,
			execErr: func(string) error { return errBoom },
			want:    "coldfront.create_iceberg_table for metrics.pg_connectivity: boom",
		},
		{
			name: "comment fails", table: "pg_connectivity", ddl: connectivityDDL,
			execErr: func(sql string) error {
				if strings.HasPrefix(sql, "COMMENT") {
					return errBoom
				}
				return nil
			},
			want: "boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &SchemaManager{useColdFrontForMetrics: true}
			tx := &recordingTx{execErr: tt.execErr}
			err := sm.execMetricsDDL(context.Background(), tx, tt.table, tt.ddl)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("execMetricsDDL error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestCreateColdFrontTableRejectsUnsupportedType(t *testing.T) {
	tx := &recordingTx{}
	spec := coldfront.Table{Name: "bogus", Columns: []coldfront.Column{{Name: "a", PGType: "cidr"}}}
	if err := createColdFrontTable(context.Background(), tx, spec); err == nil {
		t.Fatal("createColdFrontTable accepted an unsupported type")
	}
	if len(tx.stmts) != 0 {
		t.Errorf("createColdFrontTable issued %d statements for an unsupported type", len(tx.stmts))
	}
}

func TestAddMetricsConstraintSkippedUnderColdFront(t *testing.T) {
	tx := &recordingTx{}
	sm := &SchemaManager{useColdFrontForMetrics: true}
	if err := sm.addMetricsConstraint(context.Background(), tx, "metrics.pg_stat_wal", "fk", "FOREIGN KEY (connection_id) REFERENCES connections(id)"); err != nil {
		t.Fatal(err)
	}
	if len(tx.stmts) != 0 {
		t.Errorf("addMetricsConstraint issued %d statements under ColdFront", len(tx.stmts))
	}

	sm.useColdFrontForMetrics = false
	if err := sm.addMetricsConstraint(context.Background(), tx, "metrics.pg_stat_wal", "fk", "FOREIGN KEY (connection_id) REFERENCES connections(id)"); err != nil {
		t.Fatal(err)
	}
	if len(tx.stmts) != 1 || !strings.Contains(tx.stmts[0].sql, "ADD CONSTRAINT fk") {
		t.Errorf("addMetricsConstraint issued %+v in the default mode", tx.stmts)
	}
}

func TestCommentOnTableAsView(t *testing.T) {
	tests := map[string]string{
		"COMMENT ON TABLE metrics.x IS 'a table'":       "COMMENT ON VIEW metrics.x IS 'a table'",
		"comment  on\n table metrics.x IS 'ON TABLE y'": "COMMENT ON VIEW metrics.x IS 'ON TABLE y'",
		"COMMENT ON COLUMN metrics.x.a IS 'b'":          "COMMENT ON COLUMN metrics.x.a IS 'b'",
	}
	for in, want := range tests {
		if got := commentOnTableAsView(in); got != want {
			t.Errorf("commentOnTableAsView(%q) = %q, want %q", in, got, want)
		}
	}
}
