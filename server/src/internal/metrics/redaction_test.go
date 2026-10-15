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

	"github.com/pgedge/ai-workbench/pkg/redact"
	"github.com/pgedge/ai-workbench/pkg/redact/redacttest"
)

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
	if strings.Count(sql, "regexp_replace(") != len(textRedactions) ||
		!strings.Contains(sql, `left("t"."query"::text, 65536)`) ||
		strings.Count(sql, ", 'gi')") != len(textRedactions) {
		t.Errorf("unexpected SQL: %s", sql)
	}
}

// redactTextQuery masks its one parameter with RedactedText. It is
// built once from constants; the text under test is a bound parameter.
var redactTextQuery = "SELECT " + RedactedText("$1::text")

// redactJSONQuery masks its one parameter as a row image holds it, in
// the JSON text of a jsonb string.
var redactJSONQuery = "SELECT " + RedactedText("to_jsonb($1::text)")

// TestRedactedTextIntegration runs RedactedText in PostgreSQL on every
// case, as plain text and as a jsonb row image, so the patterns behave
// there as they do in Go.
func TestRedactedTextIntegration(t *testing.T) {
	pool, cleanup := newLatestRowsTestPool(t)
	defer cleanup()
	for _, tc := range redacttest.Cases {
		var got, gotJSON string
		if err := pool.QueryRow(context.Background(), redactTextQuery, tc.In).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.Name, err)
		}
		if got != tc.Want {
			t.Errorf("%s: got  %q\nwant %q", tc.Name, got, tc.Want)
		}
		if err := pool.QueryRow(context.Background(), redactJSONQuery, tc.In).Scan(&gotJSON); err != nil {
			t.Fatalf("%s as JSON: %v", tc.Name, err)
		}
		if !redacttest.JSONFormOK(tc, gotJSON) {
			t.Errorf("%s: JSON form %q", tc.Name, gotJSON)
		}
		if inGo := redact.Text(redacttest.JSONText(tc.In)); gotJSON != inGo {
			t.Errorf("%s: JSON form differs between PostgreSQL and Go:\npg %q\ngo %q", tc.Name, gotJSON, inGo)
		}
	}
	// The forms the patterns do not mask come back as redact.Text
	// returns them, so the engines agree on those too.
	for _, tc := range redacttest.KnownLimits {
		var got string
		if err := pool.QueryRow(context.Background(), redactTextQuery, tc.In).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.Name, err)
		}
		if got != tc.Want {
			t.Errorf("known limit %s: got  %q\nwant %q", tc.Name, got, tc.Want)
		}
	}
	long := strings.Repeat("é", redact.MaxTextLength+10)
	var cut string
	if err := pool.QueryRow(context.Background(), redactTextQuery, long).Scan(&cut); err != nil ||
		cut != redact.Text(long) || len([]rune(cut)) != redact.MaxTextLength {
		t.Errorf("a long value should be cut to %d characters, got %d (%v)", redact.MaxTextLength,
			len([]rune(cut)), err)
	}
	var null *string
	if err := pool.QueryRow(context.Background(), redactTextQuery, nil).Scan(&null); err != nil || null != nil {
		t.Errorf("NULL should stay NULL: %v, %v", null, err)
	}
}

// TestRedactedTextAdversarialIntegration masks inputs of about 1 MiB
// built to make a regular expression engine backtrack, as plain text
// and as a row image, and checks each finishes well within a statement
// timeout.
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
	timed := func(name, form string, run func(*string) error) {
		start := time.Now()
		var got string
		if err := run(&got); err != nil {
			t.Errorf("%s as %s: %v", name, form, err)
			return
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("%s as %s took %s", name, form, elapsed)
		}
	}
	for name, in := range redacttest.AdversarialInputs(1 << 20) {
		timed(name, "text", func(got *string) error {
			return conn.QueryRow(ctx, redactTextQuery, in).Scan(got)
		})
		timed(name, "JSON", func(got *string) error {
			return conn.QueryRow(ctx, redactJSONQuery, in).Scan(got)
		})
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
