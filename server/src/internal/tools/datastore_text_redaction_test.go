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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

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
	for _, c := range []struct{ source, column string }{
		{"connections", "connection_error"}, {"alerts", "description"}, {"alerts", "ai_analysis"},
	} {
		got, ok := configurationSourceColumn(c.source, c.column)
		if !ok || !got.RedactText {
			t.Errorf("%s.%s should be redacted: %+v", c.source, c.column, got)
		}
	}
}

func configurationSourceColumn(source, name string) (datastoreColumn, bool) {
	for _, src := range configurationSources() {
		if src.Name == source {
			return src.column(name)
		}
	}
	return datastoreColumn{}, false
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
            ($1, 2, '{"stmt": "CONNECTION E''host=192.0.2.1 password=\\''example-secret s3cr-tail\\''''"}', NULL,
             'could not connect to postgresql://app:example-secret@192.0.2.1/db', now() - interval '10 minutes'),
            ($2, 1, NULL, 'ALTER ROLE app PASSWORD ''example-secret''', NULL, now() - interval '10 minutes')`,
			args: []any{f.visible, f.hidden}},
		{sql: `UPDATE connections SET connection_error = 'connect to host=192.0.2.1 password=example-secret failed'`},
		{sql: `UPDATE alerts SET description = 'connect to host=192.0.2.1 password=example-secret failed',
            ai_analysis = 'Run ALTER ROLE app PASSWORD ''example-secret'''`},
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
				if strings.Contains(text, "example-secret") || strings.Contains(text, "s3cr") {
					t.Fatalf("%s: a credential reached the caller:\n%s", shape, text)
				}
				for _, want := range []string{`"password": "` + metrics.RedactedValue + `"`, "PASSWORD '" + metrics.RedactedValue + "'",
					"postgresql://app:" + metrics.RedactedValue + "@192.0.2.1/db"} {
					if !strings.Contains(text, want) {
						t.Errorf("%s: result should contain %q:\n%s", shape, want, text)
					}
				}
			}

			// A filter sees only the masked text, so it cannot probe the
			// real secret.
			for value, want := range map[string]int{"%example-secret%": 0, "%PASSWORD '" + metrics.RedactedValue + "'%": tc.masked} {
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
			if isErr || strings.Contains(text, "example-secret") || !strings.Contains(text, "password="+metrics.RedactedValue) {
				t.Errorf("connection_error should be masked: (error=%v) %s", isErr, text)
			}
			text, isErr = call(t, f.tool(), caller, map[string]any{
				"source": "alerts", "columns": []any{"id", "description", "ai_analysis"},
			})
			if isErr || strings.Contains(text, "example-secret") || !strings.Contains(text, "password="+metrics.RedactedValue) ||
				!strings.Contains(text, "PASSWORD '"+metrics.RedactedValue+"'") {
				t.Errorf("alert description and ai_analysis should be masked: (error=%v) %s", isErr, text)
			}
		})
	}

	text, isErr := call(t, DescribeDatastoreTool(f.pool), superuserContext(), map[string]any{"source": "spock_exception_log"})
	if isErr || !strings.Contains(text, metrics.TextRedactionNote) {
		t.Errorf("describe_datastore should note the text masking: (error=%v) %s", isErr, text)
	}
}
