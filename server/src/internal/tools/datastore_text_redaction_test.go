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
	"time"
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
	{"backslash escaped quote", `ALTER ROLE app PASSWORD 'exa\'mple-secret' VALID UNTIL 'infinity'`,
		"ALTER ROLE app PASSWORD '********' VALID UNTIL 'infinity'"},
	{"escape string", "ALTER ROLE app PASSWORD E'example-secret'",
		"ALTER ROLE app PASSWORD '********'"},
	{"unicode escape string", "ALTER ROLE app PASSWORD U&'example-secret'",
		"ALTER ROLE app PASSWORD '********'"},
	{"dollar quoted", "ALTER ROLE app PASSWORD $$exam$ple-secret$$ VALID UNTIL 'infinity'",
		"ALTER ROLE app PASSWORD '********' VALID UNTIL 'infinity'"},
	{"tagged dollar quoted", "ALTER ROLE app PASSWORD $pw$exa$mple $$secret$pw$;",
		"ALTER ROLE app PASSWORD '********';"},
	{"truncated statement", "CREATE ROLE app PASSWORD 'example-sec",
		"CREATE ROLE app PASSWORD '********'"},
	{"truncated after escaped quote", "CREATE ROLE app PASSWORD 'exa''mple-sec",
		"CREATE ROLE app PASSWORD '********'"},
	{"truncated dollar quoted", "CREATE ROLE app PASSWORD $$example-sec",
		"CREATE ROLE app PASSWORD '********'"},
	{"password null kept", "ALTER ROLE app PASSWORD NULL", "ALTER ROLE app PASSWORD NULL"},
	{"user mapping", "CREATE USER MAPPING FOR app SERVER remote OPTIONS (user 'app', password 'example-secret')",
		"CREATE USER MAPPING FOR app SERVER remote OPTIONS (user 'app', password '********')"},
	{"set to", "SET app.password TO 'example-secret'", "SET app.password TO '********'"},
	{"set quoted name", `SET "app.api_key" TO E'example-secret'`, `SET "app.api_key" TO '********'`},
	{"set equals", "SET app.password = 'example secret'", "SET app.password = ********"},
	{"set_config", "SELECT set_config('app.password', 'example-secret', false)",
		"SELECT set_config('app.password', '********', false)"},
	{"alter system command", "ALTER SYSTEM SET archive_command TO 'curl -u app:example-secret -T %p https://backup.example.com'",
		"ALTER SYSTEM SET archive_command TO '********'"},
	{"alter system conninfo", "ALTER SYSTEM SET primary_conninfo = $$host=192.0.2.1 password=example-secret$$",
		"ALTER SYSTEM SET primary_conninfo = ********"},
	{"conninfo unquoted", "CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=app password=example-secret dbname=db' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=app password=******** dbname=db' PUBLICATION p"},
	{"dblink", "SELECT dblink_connect('host=192.0.2.1 password = s3cret')",
		"SELECT dblink_connect('host=192.0.2.1 password = ********"},
	{"unquoted value with brackets", "SELECT dblink_connect('password=s3cr)e;t\"x rest')",
		"SELECT dblink_connect('password=******** rest')"},
	{"conninfo quoted", `primary_conninfo = host=192.0.2.1 password='example\'secret' port=5432`,
		`primary_conninfo = ******** password=******** port=5432`},
	{"escape string after equals", "host=192.0.2.1 password=E'example secret' dbname=db",
		"host=192.0.2.1 password=******** dbname=db"},
	{"named argument", "SELECT f(password => 'example secret')", "SELECT f(password => ********)"},
	{"double quoted shell value", `PGPASSWORD="example secret" pg_dump db`, "PGPASSWORD=******** pg_dump db"},
	{"cloud key", "AWS_SECRET_ACCESS_KEY=example-secret aws s3 cp %p s3://bucket/%f",
		"AWS_SECRET_ACCESS_KEY=******** aws s3 cp %p s3://bucket/%f"},
	{"token", "curl -H token=s3cret-token https://api.example.com", "curl -H token=******** https://api.example.com"},
	{"ldapbindpasswd", "ldapserver=ldap.example.com ldapbindpasswd=example-secret",
		"ldapserver=ldap.example.com ldapbindpasswd=********"},
	{"radiussecrets", "radiusservers=192.0.2.1 radiussecrets=s3cr,s3cr2",
		"radiusservers=192.0.2.1 radiussecrets=********"},
	{"uri", "could not connect to postgresql://app:example-secret@192.0.2.1:5432/db",
		"could not connect to postgresql://app:********@192.0.2.1:5432/db"},
	{"uri postgres scheme", "postgres://app:s3cret@db.example.com/x", "postgres://app:********@db.example.com/x"},
	{"uri other scheme", "redis://app:s3cr/et@cache.example.com:6379", "redis://app:********@cache.example.com:6379"},
	{"uri truncated", "connect to postgresql://app:example-sec", "connect to postgresql://app:********@"},
	{"uri without password", "postgresql://app@192.0.2.1/db", "postgresql://app@192.0.2.1/db"},
	{"json row image", `{"id": 1, "user_password": "example-secret", "note": "password"}`,
		`{"id": 1, "user_password": "********", "note": "password"}`},
	{"json escaped quote", `{"api_secret":"ex\"ample-secret","n":2}`, `{"api_secret":"********","n":2}`},
	{"json other secret keys", `{"api_key": "example-secret", "token": 31337, "creds": {"private_key": "example-secret"}}`,
		`{"api_key": "********", "token": "********", "creds": {"private_key": "********"}}`},
	{"json object value", `{"secret": {"value": "example-secret"}, "n": 1}`, `{"secret": "********", "n": 1}`},
	{"json array value", `{"passwords": ["s3cr1", "s3cr2"], "n": 1}`, `{"passwords": "********", "n": 1}`},
	{"plain text kept", "SELECT * FROM t WHERE password_encryption = $1", "SELECT * FROM t WHERE password_encryption = $1"},
	{"normalised parameter", "ALTER ROLE app PASSWORD $1", "ALTER ROLE app PASSWORD $1"},
	{"normalised set parameter", "SELECT set_config('app.password', $1, false)",
		"SELECT set_config('app.password', $1, false)"},
}

// secretLeft reports whether s still holds one of the test secrets.
func secretLeft(s string) bool {
	return strings.Contains(s, "example") && strings.Contains(s, "secret") && !strings.Contains(s, "example.com") ||
		strings.Contains(s, "s3cr") || strings.Contains(s, "31337")
}

// applyTextRedactionsInGo applies textRedactions with Go's regexp
// package, converting PostgreSQL's \1 to Go's ${1}.
func applyTextRedactionsInGo(t *testing.T, s string) string {
	t.Helper()
	for _, r := range textRedactions {
		// PostgreSQL's . matches a newline and, since every quantifier
		// is greedy, it takes the longest match: s and Longest do the
		// same in Go.
		re, err := regexp.Compile("(?is)" + r.Pattern)
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		re.Longest()
		s = re.ReplaceAllString(s, strings.ReplaceAll(r.Replacement, `\1`, "${1}"))
	}
	return s
}

func TestTextRedactions(t *testing.T) {
	for _, tc := range textRedactionCases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyTextRedactionsInGo(t, tc.in)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			if secretLeft(got) {
				t.Errorf("a secret survived: %q", got)
			}
		})
	}
}

// TestTextRedactionPatterns checks each pattern on its own, so removing
// one fails a test.
func TestTextRedactionPatterns(t *testing.T) {
	samples := map[string]string{
		"URI userinfo":           "postgresql://app:example-secret@192.0.2.1/db",
		"JSON key":               `{"password": "example-secret"}`,
		"SET credential setting": "SET restore_command TO 'example-secret'",
		"quoted name, value":     "('password', 'example-secret')",
		"keyword=value":          "password=example-secret",
		"PASSWORD 'value'":       "PASSWORD 'example-secret'",
	}
	if len(samples) != len(textRedactions) {
		t.Fatalf("%d samples for %d patterns", len(samples), len(textRedactions))
	}
	for _, r := range textRedactions {
		sample, ok := samples[r.Name]
		if !ok {
			t.Fatalf("no sample for %q", r.Name)
		}
		re := regexp.MustCompile("(?is)" + r.Pattern)
		re.Longest()
		got := re.ReplaceAllString(sample, strings.ReplaceAll(r.Replacement, `\1`, "${1}"))
		if strings.Contains(got, "example-secret") || !strings.Contains(got, redactedValue) {
			t.Errorf("%s: %q became %q", r.Name, sample, got)
		}
	}
}

func TestRedactedTextSQL(t *testing.T) {
	if got := sqlStringLiteral(`a'b\c`); got != `E'a''b\\c'` {
		t.Errorf("sqlStringLiteral = %s", got)
	}
	// Only group 1 may capture, so the engines cannot differ on which
	// group a replacement names.
	for _, r := range textRedactions {
		if n := regexp.MustCompile(r.Pattern).NumSubexp(); n != 1 {
			t.Errorf("%s has %d capturing groups, want 1", r.Name, n)
		}
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
	if got[1].DataType != typeText || got[1].StoredType != typeJSONB || got[2].StoredType != "" {
		t.Errorf("a redacted row image should be text stored as jsonb, got %+v", got[1:3])
	}
	// Masking does not change which columns are entity keys: a row
	// image stays a value, and text columns stay keys.
	keys := datastoreEntityKeys(datastoreSource{Columns: got})
	if strings.Join(keys, ",") != "ddl_statement,relname" {
		t.Errorf("entity keys = %v", keys)
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

// TestRedactedTextAdversarialIntegration masks inputs of about 1 MB
// built to make a regular expression engine backtrack, and checks each
// finishes well within a statement timeout.
func TestRedactedTextAdversarialIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET statement_timeout = '30s'"); err != nil {
		t.Fatalf("set statement_timeout: %v", err)
	}
	const size = 1 << 20
	inputs := map[string]string{
		"quote pairs":       "password '" + strings.Repeat("''", size/2),
		"backslashes":       "password='" + strings.Repeat(`\'`, size/3),
		"dollar signs":      "PASSWORD $pw$" + strings.Repeat("$a", size/2),
		"repeated keywords": strings.Repeat("password=", size/9),
		"json keys":         `{"password": ` + strings.Repeat(`"password": `, size/12),
		"uri":               "postgresql://" + strings.Repeat("a:", size/2),
	}
	for name, in := range inputs {
		start := time.Now()
		var got string
		if err := conn.QueryRow(ctx, redactTextQuery, in).Scan(&got); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("%s took %s", name, elapsed)
		}
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
