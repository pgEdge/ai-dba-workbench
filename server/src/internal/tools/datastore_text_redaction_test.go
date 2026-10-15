/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// textRedactionCase is one input and the text redactedText should turn
// it into. The secrets are all "example-secret" or start "s3cr", so a
// test can check none survives.
type textRedactionCase struct {
	name, in, want string
}

var textRedactionCases = []textRedactionCase{
	{"create role", "CREATE ROLE app LOGIN PASSWORD 'example-secret'",
		"CREATE ROLE app LOGIN PASSWORD '********'"},
	{"alter user encrypted", "alter user app with encrypted password 'example-secret' valid until 'infinity'",
		"alter user app with encrypted password '********' valid until 'infinity'"},
	{"escaped quote", "ALTER ROLE app PASSWORD 'exa''mple-secret'",
		"ALTER ROLE app PASSWORD '********'"},
	{"escape string", "ALTER ROLE app PASSWORD E'example-secret'",
		"ALTER ROLE app PASSWORD E'********'"},
	{"truncated statement", "CREATE ROLE app PASSWORD 'example-sec",
		"CREATE ROLE app PASSWORD '********'"},
	{"password null kept", "ALTER ROLE app PASSWORD NULL", "ALTER ROLE app PASSWORD NULL"},
	{"user mapping", "CREATE USER MAPPING FOR app SERVER remote OPTIONS (user 'app', password 'example-secret')",
		"CREATE USER MAPPING FOR app SERVER remote OPTIONS (user 'app', password '********')"},
	{"conninfo unquoted", "CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=app password=example-secret dbname=db' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=app password=******** dbname=db' PUBLICATION p"},
	{"dblink", "SELECT dblink_connect('host=192.0.2.1 password = s3cret')",
		"SELECT dblink_connect('host=192.0.2.1 password = ********')"},
	{"conninfo quoted", `primary_conninfo = host=192.0.2.1 password='example\'secret' port=5432`,
		`primary_conninfo = host=192.0.2.1 password=******** port=5432`},
	{"ldapbindpasswd", "ldapserver=ldap.example.com ldapbindpasswd=example-secret",
		"ldapserver=ldap.example.com ldapbindpasswd=********"},
	{"radiussecrets", "radiusservers=192.0.2.1 radiussecrets=s3cr,s3cr2",
		"radiusservers=192.0.2.1 radiussecrets=********"},
	{"uri", "could not connect to postgresql://app:example-secret@192.0.2.1:5432/db",
		"could not connect to postgresql://app:********@192.0.2.1:5432/db"},
	{"uri postgres scheme", "postgres://app:s3cret@db.example.com/x", "postgres://app:********@db.example.com/x"},
	{"uri without password", "postgresql://app@192.0.2.1/db", "postgresql://app@192.0.2.1/db"},
	{"json row image", `{"id": 1, "user_password": "example-secret", "note": "password"}`,
		`{"id": 1, "user_password": "********", "note": "password"}`},
	{"json escaped quote", `{"api_secret":"ex\"ample-secret","n":2}`, `{"api_secret":"********","n":2}`},
	{"plain text kept", "SELECT * FROM t WHERE password_encryption = $1", "SELECT * FROM t WHERE password_encryption = $1"},
	{"normalised parameter", "ALTER ROLE app PASSWORD $1", "ALTER ROLE app PASSWORD $1"},
}

// applyTextRedactionsInGo applies textRedactions with Go's regexp
// package, converting PostgreSQL's \1 to Go's ${1}.
func applyTextRedactionsInGo(t *testing.T, s string) string {
	t.Helper()
	for _, r := range textRedactions {
		re, err := regexp.Compile("(?i)" + r.Pattern)
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		s = re.ReplaceAllString(s, strings.ReplaceAll(r.Replacement, `\1`, "${1}"))
	}
	return s
}

func TestTextRedactions(t *testing.T) {
	for _, tc := range textRedactionCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyTextRedactionsInGo(t, tc.in); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestTextRedactionPatterns checks each pattern on its own, so removing
// one fails a test.
func TestTextRedactionPatterns(t *testing.T) {
	samples := map[string]string{
		"URI userinfo":     "postgresql://app:example-secret@192.0.2.1/db",
		"JSON key":         `{"password": "example-secret"}`,
		"keyword=value":    "password=example-secret",
		"PASSWORD 'value'": "PASSWORD 'example-secret'",
	}
	if len(samples) != len(textRedactions) {
		t.Fatalf("%d samples for %d patterns", len(samples), len(textRedactions))
	}
	for _, r := range textRedactions {
		sample, ok := samples[r.Name]
		if !ok {
			t.Fatalf("no sample for %q", r.Name)
		}
		re := regexp.MustCompile("(?i)" + r.Pattern)
		got := re.ReplaceAllString(sample, strings.ReplaceAll(r.Replacement, `\1`, "${1}"))
		if strings.Contains(got, "example-secret") || !strings.Contains(got, redactedValue) {
			t.Errorf("%s: %q became %q", r.Name, sample, got)
		}
	}
}

func TestRedactedTextSQL(t *testing.T) {
	if got := sqlStringLiteral("a'b"); got != "'a''b'" {
		t.Errorf("sqlStringLiteral = %s", got)
	}
	sql := redactedText(`"t"."query"`)
	if strings.Count(sql, "regexp_replace(") != len(textRedactions) || !strings.Contains(sql, `"t"."query"::text`) ||
		strings.Count(sql, ", 'gi')") != len(textRedactions) {
		t.Errorf("unexpected SQL: %s", sql)
	}
}

func TestRedactMetricColumnsText(t *testing.T) {
	cols := []datastoreColumn{col("connection_id", typeInteger), col("local_tup", typeJSONB),
		col("ddl_statement", typeText), col("relname", typeText)}
	got := redactMetricColumns("spock_exception_log", cols)
	for _, c := range got {
		redacted := c.Name == "local_tup" || c.Name == "ddl_statement"
		if c.RedactText != redacted {
			t.Errorf("%s: RedactText = %v", c.Name, c.RedactText)
		}
	}
	if got[1].DataType != typeText {
		t.Errorf("a redacted row image should be text, got %s", got[1].DataType)
	}
	conn, ok := connectionsSourceColumn("connection_error")
	if !ok || !conn.RedactText {
		t.Errorf("connections.connection_error should be redacted: %+v", conn)
	}
}

func connectionsSourceColumn(name string) (datastoreColumn, bool) {
	for _, src := range configurationSources() {
		if src.Name == "connections" {
			return src.column(name)
		}
	}
	return datastoreColumn{}, false
}

// redactTextQuery masks its one parameter with redactedText. It is
// built once from constants; the text under test is a bound parameter.
var redactTextQuery = "SELECT " + redactedText("$1::text")

// TestRedactedTextIntegration runs redactedText in PostgreSQL on every
// case, so the patterns behave there as they do in Go.
func TestRedactedTextIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()
	for _, tc := range textRedactionCases {
		var got string
		if err := pool.QueryRow(context.Background(), redactTextQuery, tc.in).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got  %q\nwant %q", tc.name, got, tc.want)
		}
	}
	var null *string
	if err := pool.QueryRow(context.Background(), redactTextQuery, nil).Scan(&null); err != nil || null != nil {
		t.Errorf("NULL should stay NULL: %v, %v", null, err)
	}
}

func TestQueryDatastoreRedactsFreeTextIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	stmts := []fixtureStatement{
		{sql: `DROP TABLE IF EXISTS metrics.spock_exception_log`},
		{sql: `CREATE TABLE metrics.spock_exception_log (
            connection_id INTEGER NOT NULL,
            id INTEGER NOT NULL,
            local_tup JSONB,
            ddl_statement TEXT,
            error_message TEXT,
            collected_at TIMESTAMPTZ NOT NULL)`},
		{sql: `INSERT INTO metrics.spock_exception_log (connection_id, id, local_tup, ddl_statement, error_message, collected_at) VALUES
            ($1, 1, '{"id": 1, "password": "example-secret"}', 'ALTER ROLE app PASSWORD ''example-secret''', NULL, now() - interval '10 minutes'),
            ($1, 2, NULL, NULL, 'could not connect to postgresql://app:example-secret@192.0.2.1/db', now() - interval '10 minutes'),
            ($2, 1, NULL, 'ALTER ROLE app PASSWORD ''example-secret''', NULL, now() - interval '10 minutes')`,
			args: []any{f.visible, f.hidden}},
		{sql: `UPDATE connections SET connection_error = 'connect to host=192.0.2.1 password=example-secret failed'`},
	}
	for _, s := range stmts {
		if _, err := f.pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seed %q: %v", s.sql, err)
		}
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DROP TABLE IF EXISTS metrics.spock_exception_log`); err != nil {
			t.Logf("drop spock_exception_log: %v", err)
		}
	})

	for name, tc := range map[string]struct {
		caller context.Context
		// masked is how many rows' masked ddl_statement the caller sees.
		masked int
	}{"restricted": {f.userCtx(), 1}, "superuser": {superuserContext(), 2}} {
		caller := tc.caller
		t.Run(name, func(t *testing.T) {
			for _, shape := range []string{"rows", "latest"} {
				text, isErr := call(t, f.tool(), caller, map[string]any{
					"source": "spock_exception_log", "shape": shape,
					"columns": []any{"connection_id", "id", "local_tup", "ddl_statement", "error_message"},
				})
				if isErr {
					t.Fatalf("%s query failed: %s", shape, text)
				}
				if strings.Contains(text, "example-secret") {
					t.Fatalf("%s: a credential reached the caller:\n%s", shape, text)
				}
				for _, want := range []string{`"password": "` + redactedValue + `"`, "PASSWORD '" + redactedValue + "'",
					"postgresql://app:" + redactedValue + "@192.0.2.1/db"} {
					if !strings.Contains(text, want) {
						t.Errorf("%s: result should contain %q:\n%s", shape, want, text)
					}
				}
			}

			// A filter sees only the masked text, so it cannot probe the
			// real secret.
			for value, want := range map[string]int{"%example-secret%": 0, "%PASSWORD '" + redactedValue + "'%": tc.masked} {
				rows := mustQuery(t, f.tool(), caller, map[string]any{
					"source":  "spock_exception_log",
					"filters": []any{map[string]any{"column": "ddl_statement", "op": "like", "value": value}},
				})
				if len(rows) != want {
					t.Errorf("like %q matched %d rows, want %d", value, len(rows), want)
				}
			}
			rows := mustQuery(t, f.tool(), caller, map[string]any{
				"source":  "spock_exception_log",
				"filters": []any{map[string]any{"column": "local_tup", "op": "like", "value": "%example-secret%"}},
			})
			if len(rows) != 0 {
				t.Errorf("a filter on the real row image matched %d rows", len(rows))
			}

			text, isErr := call(t, f.tool(), caller, map[string]any{
				"source": "connections", "columns": []any{"id", "connection_error"},
			})
			if isErr || strings.Contains(text, "example-secret") || !strings.Contains(text, "password="+redactedValue) {
				t.Errorf("connection_error should be masked: (error=%v) %s", isErr, text)
			}
		})
	}

	text, isErr := call(t, DescribeDatastoreTool(f.pool), superuserContext(), map[string]any{"source": "spock_exception_log"})
	if isErr || !strings.Contains(text, textRedactionNote) {
		t.Errorf("describe_datastore should note the text masking: (error=%v) %s", isErr, text)
	}
}
