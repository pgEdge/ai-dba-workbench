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
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/pkg/redact"
)

func TestRedactSettingRows(t *testing.T) {
	rows := []map[string]any{
		{
			"name":      "primary_conninfo",
			"setting":   "host=192.0.2.1 password=s3cret",
			"reset_val": "postgresql://u:s3cret@192.0.2.1/db",
			"boot_val":  "",
		},
		{"name": "ssl_passphrase_command", "setting": 7, "reset_val": nil},
		{"name": "work_mem", "setting": "4096"},
		{"setting": "password=kept-without-a-name"},
		{"name": "restore_command", "setting": "AWS_SECRET_ACCESS_KEY=s3cret aws s3 cp s3://b/%f %p"},
	}

	redactSettingRows(rows)

	checks := []struct {
		row  int
		col  string
		want any
	}{
		{0, "setting", "host=192.0.2.1 password=********"},
		{0, "reset_val", "postgresql://u:********@192.0.2.1/db"},
		{0, "boot_val", ""},
		{1, "setting", "********"},
		{1, "reset_val", nil},
		{2, "setting", "4096"},
		{3, "setting", "password=kept-without-a-name"},
		{4, "setting", "AWS_SECRET_ACCESS_KEY=******** aws ********"},
	}
	for _, c := range checks {
		if got := rows[c.row][c.col]; got != c.want {
			t.Errorf("row %d %s = %#v, want %#v", c.row, c.col, got, c.want)
		}
	}
	if _, added := rows[1]["boot_val"]; added {
		t.Error("a missing column was added")
	}
}

// TestQueryLatestRows_RedactsPgSettings stores a pg_settings row as a
// collector that did not redact it would have, and checks the latest-row
// query returns it without the password (GitHub issue #643).
func TestQueryLatestRows_RedactsPgSettings(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS metrics;
		DROP TABLE IF EXISTS metrics.pg_settings CASCADE;
		CREATE TABLE metrics.pg_settings (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			name TEXT NOT NULL,
			setting TEXT,
			reset_val TEXT,
			boot_val TEXT
		)`); err != nil {
		t.Fatalf("create pg_settings: %v", err)
	}
	defer dropTable(context.Background(), pool, pgSettingsProbe)

	if _, err := pool.Exec(ctx, `
		INSERT INTO metrics.pg_settings
			(connection_id, collected_at, name, setting, reset_val, boot_val)
		VALUES
			(1, $1, 'primary_conninfo', 'host=192.0.2.1 password=s3cret',
			 'host=192.0.2.1 password=********', ''),
			(1, $1, 'work_mem', '4096', '4096', '4096')
	`, time.Now().UTC()); err != nil {
		t.Fatalf("insert pg_settings: %v", err)
	}

	result, err := QueryLatestRows(ctx, pool, pgSettingsProbe,
		[]int{1}, MetricFilters{}, "", "desc", 10)
	if err != nil {
		t.Fatalf("QueryLatestRows: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d rows, want 2", len(result))
	}
	for _, row := range result {
		for _, col := range settingValueColumns {
			if s, ok := row[col].(string); ok && strings.Contains(s, "s3cret") {
				t.Errorf("%v.%s leaks the password: %q", row["name"], col, s)
			}
		}
		// A connection string the patterns would rewrite is masked
		// whole; one the collector already redacted is kept.
		if row["name"] == "primary_conninfo" &&
			(row["setting"] != RedactedValue || row["reset_val"] != "host=192.0.2.1 password=********") {
			t.Errorf("primary_conninfo setting = %#v, reset_val = %#v", row["setting"], row["reset_val"])
		}
		if row["name"] == "work_mem" && row["setting"] != "4096" {
			t.Errorf("work_mem setting = %#v, want 4096", row["setting"])
		}
	}

	// A setting's value column is refused as order_by.
	for _, col := range settingValueColumns {
		_, err := QueryLatestRows(ctx, pool, pgSettingsProbe, []int{1}, MetricFilters{}, col, "asc", 10)
		if err == nil || err.Error() != UnsortableColumnError(pgSettingsProbe, col) {
			t.Errorf("order_by %s: got %v, want the unsortable column error", col, err)
		}
	}
}

// TestQueryLatestRows_RedactsStatementText stores statement text whose
// secret came back when the server masked it in SQL before lexing it,
// and checks the latest-row query lexes the stored text instead and
// returns neither the secret nor the stored text column (GitHub issue
// #670).
func TestQueryLatestRows_RedactsStatementText(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS metrics;
		DROP TABLE IF EXISTS metrics.spock_exception_log CASCADE;
		CREATE TABLE metrics.spock_exception_log (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			remote_xid BIGINT,
			ddl_statement TEXT,
			error_message TEXT
		)`); err != nil {
		t.Fatalf("create spock_exception_log: %v", err)
	}
	defer dropTable(context.Background(), pool, "spock_exception_log")

	now := time.Now().UTC()
	for stmt := range quoteShiftStatements {
		if _, err := pool.Exec(ctx, `
			INSERT INTO metrics.spock_exception_log
				(connection_id, collected_at, remote_xid, ddl_statement, error_message)
			VALUES (1, $1, $2, $3, NULL)`, now, len(stmt), stmt); err != nil {
			t.Fatalf("insert spock_exception_log: %v", err)
		}
	}

	result, err := QueryLatestRows(ctx, pool, "spock_exception_log",
		[]int{1}, MetricFilters{}, "", "desc", 10)
	if err != nil {
		t.Fatalf("QueryLatestRows: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("got no rows")
	}
	for _, row := range result {
		for col := range row {
			if strings.HasPrefix(col, rawStatementPrefix) {
				t.Errorf("stored statement column %s returned", col)
			}
		}
		if row["error_message"] != nil {
			t.Errorf("error_message = %#v, want NULL", row["error_message"])
		}
		got, _ := row["ddl_statement"].(string)
		if got == "" {
			t.Errorf("ddl_statement = %#v, want masked text", row["ddl_statement"])
		}
		for _, secret := range quoteShiftStatements {
			if strings.Contains(got, secret) {
				t.Errorf("ddl_statement %q keeps %s", got, secret)
			}
		}
	}

	// A query PostgreSQL rejects, here filtering on a queryid column the
	// table lacks, is reported rather than returning rows.
	queryID := int64(7)
	if _, err := QueryLatestRows(ctx, pool, "spock_exception_log", []int{1},
		MetricFilters{QueryID: &queryID}, "", "desc", 10); err == nil ||
		!strings.Contains(err.Error(), "failed to query latest rows") {
		t.Errorf("filter on a missing column: got %v, want the query error", err)
	}
}

func TestSortableColumn(t *testing.T) {
	for _, tc := range []struct {
		probe, column string
		want          bool
	}{
		{"pg_settings", "setting", false},
		{"pg_settings", "reset_val", false},
		{"pg_settings", "boot_val", false},
		{"pg_settings", "name", true},
		{"pg_stat_statements", "query", true},
		{"pg_stat_activity", "setting", true},
	} {
		if got := SortableColumn(tc.probe, tc.column); got != tc.want {
			t.Errorf("SortableColumn(%q, %q) = %v, want %v", tc.probe, tc.column, got, tc.want)
		}
	}
}

func TestRedactProbeRows(t *testing.T) {
	const leaked = "ALTER ROLE app PASSWORD 'hunter2'"
	const redacted = "ALTER ROLE app PASSWORD '********'"
	raw := RawStatementAlias("query")

	tests := []struct {
		name, probe string
		row, want   map[string]any
	}{
		{
			name:  "activity query",
			probe: "pg_stat_activity",
			row:   map[string]any{"query": "masked in SQL", raw: leaked, "state": leaked},
			want:  map[string]any{"query": redacted, "state": leaked},
		},
		{
			name:  "normalized statement",
			probe: "pg_stat_statements",
			row:   map[string]any{"query": "SELECT $1", raw: "SELECT $1", "calls": int64(4)},
			want:  map[string]any{"query": "SELECT $1", "calls": int64(4)},
		},
		{
			name:  "unexpected type",
			probe: "pg_stat_statements",
			row:   map[string]any{"query": leaked, raw: []byte(leaked)},
			want:  map[string]any{"query": "********"},
		},
		{
			name:  "no stored value selected",
			probe: "pg_stat_statements",
			row:   map[string]any{"query": leaked},
			want:  map[string]any{"query": "********"},
		},
		{
			name:  "NULL statement",
			probe: "pg_stat_statements",
			row:   map[string]any{"query": nil, raw: nil},
			want:  map[string]any{"query": nil},
		},
		{
			name:  "spock exception",
			probe: "spock_exception_log",
			row: map[string]any{"ddl_statement": "masked in SQL", RawStatementAlias("ddl_statement"): leaked,
				"error_message": nil, RawStatementAlias("error_message"): nil},
			want: map[string]any{"ddl_statement": redacted, "error_message": nil},
		},
		{
			name:  "settings",
			probe: "pg_settings",
			row: map[string]any{
				"name":    "primary_conninfo",
				"setting": "host=192.0.2.1 password=s3cret",
			},
			want: map[string]any{
				"name":    "primary_conninfo",
				"setting": "host=192.0.2.1 password=********",
			},
		},
		{
			name:  "other probe",
			probe: "pg_stat_database",
			row:   map[string]any{"query": leaked},
			want:  map[string]any{"query": leaked},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RedactProbeRows(tt.probe, []map[string]any{tt.row})
			for col, want := range tt.want {
				if got := tt.row[col]; got != want {
					t.Errorf("%s = %#v, want %#v", col, got, want)
				}
			}
			for col := range tt.row {
				if strings.HasPrefix(col, rawStatementPrefix) {
					t.Errorf("stored statement column %s left in the row", col)
				}
			}
		})
	}
}

func TestRawStatementColumns(t *testing.T) {
	for _, tc := range []struct {
		probe   string
		columns []string
		want    []string
	}{
		{"pg_stat_statements", []string{"queryid", "query", "calls"}, []string{"query"}},
		{"pg_stat_statements", []string{"queryid", "calls"}, nil},
		{"spock_exception_log", []string{"error_message", "ddl_statement", "local_tup"},
			[]string{"ddl_statement", "error_message"}},
		{"pg_stat_database", []string{"query"}, nil},
	} {
		got := RawStatementColumns(tc.probe, tc.columns)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("RawStatementColumns(%q, %v) = %v, want %v", tc.probe, tc.columns, got, tc.want)
		}
	}
	if got := RawStatementAlias("query"); got != "_raw_statement_query" {
		t.Errorf("RawStatementAlias(query) = %q", got)
	}
}

func TestRedactStatementText(t *testing.T) {
	got := RedactStatementText(
		"CREATE USER MAPPING FOR app SERVER s OPTIONS (user 'app', password 'hunter2')")
	want := "CREATE USER MAPPING FOR app SERVER s OPTIONS (user 'app', password '********')"
	if got != want {
		t.Errorf("RedactStatementText() = %q, want %q", got, want)
	}
	if got := RedactStatementText("SELECT 1"); got != "SELECT 1" {
		t.Errorf("RedactStatementText(SELECT 1) = %q", got)
	}
}

// quoteShiftStatements are statements in which masking the text first,
// as RedactedText masks it in SQL, moves a quote so that
// sqlredact.RedactQuery then reads the secret as outside a literal and
// returns it (GitHub issue #670). RedactStatementText lexes the text as
// stored, so it must mask every secret.
var quoteShiftStatements = map[string]string{
	`ALTER SYSTEM SET restore_command = 'PGPASSWORD=\''PSSLEAKsecret17 cp %f %p'`:       "PSSLEAKsecret17",
	`ALTER SYSTEM SET primary_conninfo = 'host=h user=rep password=\''PSSLEAKsecret18'`: "PSSLEAKsecret18",
	`ALTER ROLE app PASSWORD '\''WFLEAKsecret19'`:                                       "WFLEAKsecret19",
}

func TestRedactStatementTextQuoteShift(t *testing.T) {
	for stmt, secret := range quoteShiftStatements {
		if got := RedactStatementText(stmt); strings.Contains(got, secret) {
			t.Errorf("RedactStatementText(%q) = %q, keeps the secret", stmt, got)
		}
		row := map[string]any{"query": "masked in SQL", RawStatementAlias("query"): stmt}
		RedactProbeRows("pg_stat_statements", []map[string]any{row})
		if got, _ := row["query"].(string); strings.Contains(got, secret) || got == "" {
			t.Errorf("RedactProbeRows on %q returned %q", stmt, got)
		}
	}
}

func TestRedactStatementValue(t *testing.T) {
	const stored = `ALTER ROLE app PASSWORD 'hunter2'`
	for name, tc := range map[string]struct {
		masked, raw, want any
	}{
		"NULL stays NULL":          {nil, stored, nil},
		"masked from stored text":  {"masked in SQL", stored, RedactStatementText(stored)},
		"no stored text":           {"masked in SQL", nil, RedactedValue},
		"stored text not a string": {"masked in SQL", int64(7), RedactedValue},
	} {
		if got := RedactStatementValue(tc.masked, tc.raw); got != tc.want {
			t.Errorf("%s: RedactStatementValue() = %v, want %v", name, got, tc.want)
		}
	}
}

func TestRedactStatementTextCutsLongText(t *testing.T) {
	long := strings.Repeat("x", redact.MaxTextLength+10)
	if got := RedactStatementText(long); len(got) != redact.MaxTextLength {
		t.Errorf("RedactStatementText cut %d characters to %d, want %d",
			len(long), len(got), redact.MaxTextLength)
	}
}
