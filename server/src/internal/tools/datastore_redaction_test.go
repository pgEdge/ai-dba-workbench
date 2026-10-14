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

func settingsColumns(names ...string) []datastoreColumn {
	cols := make([]datastoreColumn, len(names))
	for i, n := range names {
		cols[i] = col(n, typeText)
	}
	return cols
}

func TestRedactMetricColumns(t *testing.T) {
	t.Run("pg_settings value columns are marked", func(t *testing.T) {
		got := redactMetricColumns("pg_settings",
			settingsColumns("connection_id", "name", "setting", "reset_val", "boot_val", "unit"))
		want := map[string]string{"setting": "name", "reset_val": "name", "boot_val": "name"}
		for _, c := range got {
			if c.RedactBy != want[c.Name] {
				t.Errorf("%s RedactBy = %q, want %q", c.Name, c.RedactBy, want[c.Name])
			}
		}
		if len(got) != 6 {
			t.Errorf("got %d columns, want 6", len(got))
		}
	})

	t.Run("value columns are dropped without the name column", func(t *testing.T) {
		got := redactMetricColumns("pg_settings", settingsColumns("connection_id", "setting", "unit"))
		for _, c := range got {
			if c.Name == "setting" {
				t.Errorf("setting should be dropped when it cannot be masked by row")
			}
		}
		if len(got) != 2 {
			t.Errorf("got %d columns, want 2", len(got))
		}
	})

	t.Run("other probes are untouched", func(t *testing.T) {
		got := redactMetricColumns("ds_probe", settingsColumns("name", "setting"))
		for _, c := range got {
			if c.RedactBy != "" {
				t.Errorf("%s should not be redacted", c.Name)
			}
		}
	})
}

// TestCredentialSettingLiteralsAreSafe pins the constants that
// credentialSettingCondition writes into SQL as literals.
func TestCredentialSettingLiteralsAreSafe(t *testing.T) {
	safe := regexp.MustCompile(`^[a-z_]+$`)
	for _, v := range append(append([]string{}, credentialSettings...), secretColumnFragments...) {
		if !safe.MatchString(v) {
			t.Errorf("%q is not safe to write as a SQL literal", v)
		}
	}
	if !safe.MatchString(strings.ReplaceAll(redactedValue, "*", "a")) {
		t.Errorf("redactedValue %q is not safe to write as a SQL literal", redactedValue)
	}
	cond := credentialSettingCondition("t", "name")
	for _, want := range []string{`lower(t."name") IN ('primary_conninfo', 'ssl_passphrase_command')`, `~ '(password|`} {
		if !strings.Contains(cond, want) {
			t.Errorf("condition %s does not contain %s", cond, want)
		}
	}
}

func TestScopedFromRedactsWithoutServerPredicates(t *testing.T) {
	src := datastoreSource{
		Name: "pg_settings", Table: "pg_settings", Kind: datastoreKindMetric,
		Columns: redactMetricColumns("pg_settings", settingsColumns("name", "setting")),
	}
	from, err := scopedFrom(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(from, "CASE WHEN") || !strings.Contains(from, `END AS "setting"`) ||
		strings.Contains(from, "WHERE") {
		t.Errorf("unexpected FROM item: %s", from)
	}

	plain := datastoreSource{Name: "ds_probe", Table: "ds_probe", Kind: datastoreKindMetric,
		Columns: settingsColumns("name")}
	if from, _ := scopedFrom(plain, nil); strings.Contains(from, "SELECT") {
		t.Errorf("a source with nothing to scope or mask should read the table directly: %s", from)
	}
}

func TestQueryDatastoreRedactsCredentialSettingsIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	const secret = "host=192.0.2.1 user=replicator password=example-secret"
	stmts := []fixtureStatement{
		{sql: `DROP TABLE IF EXISTS metrics.pg_settings`},
		{sql: `CREATE TABLE metrics.pg_settings (
            connection_id INTEGER NOT NULL,
            name TEXT NOT NULL,
            setting TEXT,
            boot_val TEXT,
            reset_val TEXT,
            collected_at TIMESTAMPTZ NOT NULL)`},
		{sql: `INSERT INTO metrics.pg_settings (connection_id, name, setting, boot_val, reset_val, collected_at) VALUES
            ($1, 'primary_conninfo', $3, $3, $3, now() - interval '10 minutes'),
            ($1, 'myext.Password', 'example-secret', '', 'example-secret', now() - interval '10 minutes'),
            ($1, 'ssl_passphrase_command', 'echo example-secret', '', NULL, now() - interval '10 minutes'),
            ($1, 'work_mem', '4096', '4096', '4096', now() - interval '10 minutes'),
            ($2, 'primary_conninfo', $3, $3, $3, now() - interval '10 minutes')`,
			args: []any{f.visible, f.hidden, secret}},
	}
	for _, s := range stmts {
		if _, err := f.pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seed %q: %v", s.sql, err)
		}
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DROP TABLE IF EXISTS metrics.pg_settings`); err != nil {
			t.Logf("drop pg_settings: %v", err)
		}
	})

	for name, caller := range map[string]context.Context{"restricted": f.userCtx(), "superuser": superuserContext()} {
		t.Run(name, func(t *testing.T) {
			text, isErr := call(t, f.tool(), caller, map[string]any{
				"source": "pg_settings", "columns": []any{"connection_id", "name", "setting", "boot_val", "reset_val"},
			})
			if isErr {
				t.Fatalf("query failed: %s", text)
			}
			if strings.Contains(text, "example-secret") {
				t.Fatalf("a credential reached the caller:\n%s", text)
			}
			for _, r := range parseResultRows(t, text) {
				switch r["name"] {
				case "work_mem":
					if r["setting"] != "4096" {
						t.Errorf("work_mem should not be masked: %v", r)
					}
				case "primary_conninfo":
					if r["setting"] != redactedValue || r["boot_val"] != redactedValue || r["reset_val"] != redactedValue {
						t.Errorf("primary_conninfo should be masked: %v", r)
					}
				default:
					if r["setting"] != redactedValue {
						t.Errorf("%s should be masked: %v", r["name"], r)
					}
				}
			}

			// A filter sees only the masked value, so it cannot probe the
			// real one.
			rows := mustQuery(t, f.tool(), caller, map[string]any{
				"source":  "pg_settings",
				"filters": []any{map[string]any{"column": "setting", "op": "like", "value": "%password%"}},
			})
			if len(rows) != 0 {
				t.Errorf("a filter on the real value matched %d rows", len(rows))
			}
		})
	}

	// An empty value stays empty, since it reveals only that the setting
	// is unset.
	rows := mustQuery(t, f.tool(), superuserContext(), map[string]any{
		"source":  "pg_settings",
		"columns": []any{"name", "boot_val"},
		"filters": []any{map[string]any{"column": "name", "op": "=", "value": "myext.Password"}},
	})
	if len(rows) != 1 || rows[0]["boot_val"] != "" {
		t.Errorf("an empty value should stay empty: %v", rows)
	}

	for _, args := range []map[string]any{{"source": "pg_settings"}, {}} {
		text, isErr := call(t, DescribeDatastoreTool(f.pool), superuserContext(), args)
		if isErr || !strings.Contains(text, redactionNote) {
			t.Errorf("describe_datastore %v should note the masking: (error=%v) %s", args, isErr, text)
		}
	}
}
