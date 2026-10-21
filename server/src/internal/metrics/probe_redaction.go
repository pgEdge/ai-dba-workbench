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
	"slices"

	"github.com/pgedge/ai-workbench/pkg/pgsettings"
	"github.com/pgedge/ai-workbench/pkg/redact"
	"github.com/pgedge/ai-workbench/pkg/sqlredact"
)

// pgSettingsProbe is the probe table that stores configuration settings.
const pgSettingsProbe = "pg_settings"

// settingValueColumns are the pg_settings columns holding a value of the
// setting itself.
var settingValueColumns = []string{"setting", "reset_val", "boot_val"}

// statementTextColumns maps each probe table that stores SQL statement
// text to the columns holding it.
var statementTextColumns = map[string][]string{
	"pg_stat_activity":    {"query"},
	"pg_stat_statements":  {"query"},
	"spock_exception_log": {"ddl_statement", "error_message"},
}

// rawStatementPrefix starts the alias under which a latest query
// selects a statement-text column as stored, beside the column masked
// in SQL (RedactedText) under its own name. A probe column cannot clash
// with it, since none begins with an underscore.
const rawStatementPrefix = "_raw_statement_"

// RawStatementAlias returns the alias under which a latest query selects
// column, a statement-text column, as stored. RedactProbeRows masks the
// column from the value under this alias and removes the alias from the
// row.
func RawStatementAlias(column string) string {
	return rawStatementPrefix + column
}

// RawStatementColumns returns the statement-text columns of probe, among
// columns, that MaskedProbeColumn returns. A latest query selects each as
// stored under RawStatementAlias, beside its masked value, so that
// RedactProbeRows can run sqlredact.RedactQuery over the stored text: run
// over the text RedactedText masked, the lexer can read a quote that
// masking moved as the end of a literal and return the secret after it
// (GitHub issue #670).
func RawStatementColumns(probe string, columns []string) []string {
	var out []string
	for _, col := range statementTextColumns[probe] {
		if slices.Contains(columns, col) && ProbeColumnMask(probe, col, columns) != MaskOmit {
			out = append(out, col)
		}
	}
	return out
}

// RedactProbeRows removes secrets from rows read from a probe table
// before they are returned: credentials in pg_settings values, such as
// the password in primary_conninfo, and passwords in statement text,
// such as ALTER ROLE ... PASSWORD. The collector redacts both before
// storing and migrates rows stored earlier (GitHub issue #643); this
// covers a datastore it has not migrated yet. Rows of other probes are
// left as they are. Every path that returns raw probe rows calls it.
//
// A statement-text column is masked from the stored value the query
// selected under RawStatementAlias, never from the column's own value,
// which SQL has already masked; a row with a non-NULL statement and no
// stored value beside it has the statement replaced whole.
func RedactProbeRows(probeName string, rows []map[string]any) {
	if probeName == pgSettingsProbe {
		redactSettingRows(rows)
		return
	}
	if columns, ok := statementTextColumns[probeName]; ok {
		redactStatementRows(rows, columns)
	}
}

// RedactStatementText removes passwords and other secrets from a single
// SQL statement text, as stored, before it is returned: it masks the
// statement with sqlredact.RedactQuery and then the result as
// RedactedText masks text in SQL, cut to redact.MaxTextLength. The order
// matters, since the lexer must read the text as stored; masking only
// replaces text, so the second pass cannot restore what the first
// masked. See RedactProbeRows.
func RedactStatementText(text string) string {
	return redact.Text(sqlredact.RedactQuery(text))
}

// redactSettingRows removes credentials from pg_settings rows. A value of
// an unexpected type in a credential-bearing row is replaced outright.
func redactSettingRows(rows []map[string]any) {
	for _, row := range rows {
		name, ok := row["name"].(string)
		if !ok || !pgsettings.IsCredentialSetting(name) {
			continue
		}
		for _, col := range settingValueColumns {
			switch v := row[col].(type) {
			case nil:
			case string:
				row[col] = pgsettings.RedactValue(name, v)
			default:
				row[col] = pgsettings.RedactedValue
			}
		}
	}
}

// redactStatementRows removes secrets from the named statement-text
// columns, masking each from the stored value under RawStatementAlias
// and removing that alias from the row. A statement with no stored
// value beside it, or a stored value of an unexpected type, which
// cannot be lexed, is replaced outright.
func redactStatementRows(rows []map[string]any, columns []string) {
	for _, row := range rows {
		for _, col := range columns {
			alias := RawStatementAlias(col)
			raw := row[alias]
			delete(row, alias)
			row[col] = RedactStatementValue(row[col], raw)
		}
	}
}

// RedactStatementValue returns the value to return for a statement-text
// column, given masked, the column's value as SQL masked it
// (RedactedText), and raw, the text as stored, selected beside it under
// RawStatementAlias. A NULL statement stays NULL; otherwise the result is
// RedactStatementText over raw, or sqlredact.Redacted when raw is not
// text, as when no stored value was selected, since masked alone cannot
// be lexed safely.
func RedactStatementValue(masked, raw any) any {
	if masked == nil {
		return nil
	}
	if v, ok := raw.(string); ok {
		return RedactStatementText(v)
	}
	return sqlredact.Redacted
}
