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
	"github.com/pgedge/ai-workbench/pkg/pgsettings"
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

// RedactProbeRows removes secrets from rows read from a probe table
// before they are returned: credentials in pg_settings values, such as
// the password in primary_conninfo, and passwords in statement text,
// such as ALTER ROLE ... PASSWORD. The collector redacts both before
// storing and migrates rows stored earlier (GitHub issue #643); this
// covers a datastore it has not migrated yet. Rows of other probes are
// left as they are. Every path that returns raw probe rows calls it.
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
// SQL statement text before it is returned. See RedactProbeRows.
func RedactStatementText(text string) string {
	return sqlredact.RedactQuery(text)
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
// columns. A value of an unexpected type is replaced outright, since it
// cannot be lexed.
func redactStatementRows(rows []map[string]any, columns []string) {
	for _, row := range rows {
		for _, col := range columns {
			switch v := row[col].(type) {
			case nil:
			case string:
				row[col] = sqlredact.RedactQuery(v)
			default:
				row[col] = sqlredact.Redacted
			}
		}
	}
}
