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
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// textRedactionCase is one input and the text RedactedText should turn
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
	{"doubled quote conninfo", "CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=''s3cret a b''' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=********' PUBLICATION p"},
	{"doubled quote with escaped quote", "CREATE SUBSCRIPTION s CONNECTION 'password=''s3cr''''et x'' dbname=db' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'password=******** dbname=db' PUBLICATION p"},
	{"doubled quote backslash escape", `SELECT dblink_connect('password=''s3cr\''et x'' dbname=db')`,
		"SELECT dblink_connect('password=******** dbname=db')"},
	{"positional node_create", "SELECT spock.node_create('n1', 'host=192.0.2.1 password=''s3cret x''')",
		"SELECT spock.node_create('n1', 'host=192.0.2.1 password=********')"},
	{"nested role password", "EXECUTE 'ALTER ROLE app PASSWORD ''s3cret x'''",
		"EXECUTE 'ALTER ROLE app PASSWORD '********''"},
	{"unquoted dollar value", "host=192.0.2.1 password=$s3cret dbname=db", "host=192.0.2.1 password=******** dbname=db"},
	{"ordinary identifiers kept", "UPDATE t SET sort_key = 42, bypass = true, primary_key = 'abc' WHERE salt = 1",
		"UPDATE t SET sort_key = 42, bypass = true, primary_key = 'abc' WHERE salt = 1"},
	{"ordinary json keys kept", `{"cache_key": "abc", "sort_key": 1, "id": 2}`, `{"cache_key": "abc", "sort_key": 1, "id": 2}`},
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
		if strings.Contains(got, "example-secret") || !strings.Contains(got, RedactedValue) {
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
	sql := RedactedText(`"t"."query"`)
	if strings.Count(sql, "regexp_replace(") != len(textRedactions) || !strings.Contains(sql, `"t"."query"::text`) ||
		strings.Count(sql, ", 'gi')") != len(textRedactions) {
		t.Errorf("unexpected SQL: %s", sql)
	}
}

// redactTextQuery masks its one parameter with RedactedText. It is
// built once from constants; the text under test is a bound parameter.
var redactTextQuery = "SELECT " + RedactedText("$1::text")

// TestRedactedTextIntegration runs RedactedText in PostgreSQL on every
// case, so the patterns behave there as they do in Go.
func TestRedactedTextIntegration(t *testing.T) {
	pool, cleanup := newLatestRowsTestPool(t)
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
	pool, cleanup := newLatestRowsTestPool(t)
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
		"nested quotes":     "password=''" + strings.Repeat("''''", size/4),
		"lone quotes":       "password ''" + strings.Repeat("'", size),
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

// TestCredentialSettingLiteralsAreSafe pins the constants that
// credentialSettingCondition writes into SQL as literals.
func TestCredentialSettingLiteralsAreSafe(t *testing.T) {
	safe := regexp.MustCompile(`^[a-z0-9_]+$`)
	for _, v := range slices.Concat(credentialSettings, nonCredentialSettings, secretColumnFragments) {
		if !safe.MatchString(v) {
			t.Errorf("%q is not safe to write as a SQL literal", v)
		}
	}
	safePattern := regexp.MustCompile(`^[a-z_?$]+$`)
	for _, v := range slices.Concat(secretNamePatterns, textSecretNamePatterns) {
		if !safePattern.MatchString(v) {
			t.Errorf("pattern %q is not safe to write as a SQL literal", v)
		}
	}
	if !safe.MatchString(strings.ReplaceAll(RedactedValue, "*", "a")) {
		t.Errorf("RedactedValue %q is not safe to write as a SQL literal", RedactedValue)
	}
	cond := credentialSettingCondition(`t."name"`)
	for _, want := range []string{`lower(t."name") IN ('primary_conninfo', 'ssl_passphrase_command', 'archive_command', 'archive_cleanup_command', 'restore_command', 'recovery_end_command')`, `~ '(password|`, `|pass$|key$|api_?key|access_key|salt)'`,
		`NOT IN ('password_encryption', 'md5_password_warnings', 'ssl_passphrase_command_supports_reload')`} {
		if !strings.Contains(cond, want) {
			t.Errorf("condition %s does not contain %s", cond, want)
		}
	}
}

func TestIsSecretColumnName(t *testing.T) {
	for name, want := range map[string]bool{
		"conninfo":           true,
		"SSLKEY":             true,
		"password_encrypted": true,
		"db_passwd":          true,
		"client_secret":      true,
		"api_token":          true,
		"subconninfo":        true,
		"ssl_passphrase":     true,
		"primary_connstr":    true,
		"connection_string":  true,
		"target_dsn":         true,
		"sslrootcert_path":   true,
		"subscription_name":  false,
		"auto_group_key":     false,
		"xact_commit":        false,
		"datname":            false,
	} {
		if got := IsSecretColumnName(name); got != want {
			t.Errorf("IsSecretColumnName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSecretNamePatterns(t *testing.T) {
	re := regexp.MustCompile("(" + strings.Join(secretNamePatterns, "|") + ")")
	for name, want := range map[string]bool{
		"ldapbindpasswd":          true,
		"radiussecrets":           true,
		"myext.api_key":           true,
		"myext.apikey":            true,
		"myext.access_key_id":     true,
		"myext.dsn":               true,
		"myext.connection_string": true,
		"myext.salt":              true,
		"myext.pass":              true,
		"ldapserver":              false,
		"work_mem":                false,
		"shared_buffers":          false,
		"ssl_key_file":            false,
	} {
		if got := re.MatchString(name); got != want {
			t.Errorf("%q matches = %v, want %v", name, got, want)
		}
	}
}

func TestProbeColumnMask(t *testing.T) {
	settings := []string{"connection_id", "name", "setting", "unit"}
	for _, tc := range []struct {
		probe, column string
		columns       []string
		want          ColumnMask
	}{
		{"pg_settings", "setting", settings, MaskSetting},
		{"pg_settings", "name", settings, MaskNone},
		{"pg_settings", "setting", []string{"setting", "unit"}, MaskOmit},
		{"pg_hba_file_rules", "options", nil, MaskOptions},
		{"pg_hba_file_rules", "error", nil, MaskWhole},
		{"pg_stat_activity", "query", nil, MaskText},
		{"spock_exception_log", "local_tup", nil, MaskText},
		{"pg_stat_activity", "usename", nil, MaskNone},
		{"pg_stat_wal_receiver", "conninfo", nil, MaskOmit},
		{"any_probe", "api_token", nil, MaskOmit},
		{"any_probe", "setting", nil, MaskNone},
	} {
		if got := ProbeColumnMask(tc.probe, tc.column, tc.columns); got != tc.want {
			t.Errorf("ProbeColumnMask(%s, %s) = %d, want %d", tc.probe, tc.column, got, tc.want)
		}
	}
}

func TestMaskedProbeColumn(t *testing.T) {
	ref := func(c string) string { return `t."` + c + `"` }
	settings := []string{"name", "setting"}
	for _, tc := range []struct {
		probe, column string
		want          string
		ok            bool
	}{
		{"pg_settings", "setting", MaskedSetting(`t."setting"`, `t."name"`), true},
		{"pg_hba_file_rules", "options", MaskedOptions(`t."options"`), true},
		{"pg_hba_file_rules", "error", `CASE WHEN t."error" <> '' THEN '` + RedactedValue + `' ELSE t."error" END`, true},
		{"pg_stat_statements", "query", RedactedText(`t."query"`), true},
		{"pg_stat_statements", "calls", `t."calls"`, true},
		{"any_probe", "sslkey", "", false},
	} {
		got, ok := MaskedProbeColumn(tc.probe, tc.column, settings, ref)
		if got != tc.want || ok != tc.ok {
			t.Errorf("MaskedProbeColumn(%s, %s) = %q, %v; want %q, %v", tc.probe, tc.column, got, ok, tc.want, tc.ok)
		}
	}
	got := ReturnableColumns("pg_settings", []string{"name", "setting", "ssl_passphrase", "unit"})
	if strings.Join(got, ",") != "name,setting,unit" {
		t.Errorf("ReturnableColumns = %v", got)
	}
	if got := ReturnableColumns("pg_settings", []string{"setting", "unit"}); strings.Join(got, ",") != "unit" {
		t.Errorf("a setting without its name column should be left out: %v", got)
	}

	setting := MaskedSetting(`t."setting"`, `t."name"`)
	for _, want := range []string{`CASE WHEN (lower(t."name") IN (`, `AND t."setting" <> '' THEN '` + RedactedValue +
		`' ELSE t."setting" END`} {
		if !strings.Contains(setting, want) {
			t.Errorf("MaskedSetting %s does not contain %s", setting, want)
		}
	}
}

// TestBuildLatestRowsQueryMasks checks the latest-rows query masks in its
// inner query, so DISTINCT ON and the sort see the masked value, and
// selects a column that should have been left out as NULL.
func TestBuildLatestRowsQueryMasks(t *testing.T) {
	query, _ := buildLatestRowsQuery("pg_settings", []string{"name", "setting", "sslkey"},
		map[string]string{"name": "text", "setting": "text", "sslkey": "text"},
		[]int{1}, MetricFilters{}, "setting", "asc", 10)
	for _, want := range []string{
		`SELECT "name", "setting", "sslkey"` + "\n",
		MaskedSetting(`"setting"`, `"name"`) + ` AS "setting", NULL AS "sslkey", connection_id, collected_at`,
		`DISTINCT ON (connection_id, "name", "setting", "sslkey")`,
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query does not contain %s:\n%s", want, query)
		}
	}
}
