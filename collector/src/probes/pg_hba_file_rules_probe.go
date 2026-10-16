/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// PgHbaFileRulesProbe collects metrics from pg_hba_file_rules view
// This probe only stores data when HBA configuration changes are detected
type PgHbaFileRulesProbe struct {
	BaseMetricsProbe
}

// NewPgHbaFileRulesProbe creates a new pg_hba_file_rules probe
func NewPgHbaFileRulesProbe(config *ProbeConfig) *PgHbaFileRulesProbe {
	return &PgHbaFileRulesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute (default for PG16+)
func (p *PgHbaFileRulesProbe) GetQuery() string {
	return p.GetQueryForVersion(16)
}

// GetQueryForVersion returns the appropriate SQL query for the given PostgreSQL version
func (p *PgHbaFileRulesProbe) GetQueryForVersion(pgVersion int) string {
	if pgVersion >= 16 {
		// PG16+ has a rule_number column, but it is NULL for a line
		// PostgreSQL could not parse, and metrics.pg_hba_file_rules
		// keys every row on rule_number. Such a row is given a negative
		// key, numbered in order of file name and then line number (not
		// the order in which files are included), so it cannot collide
		// with a valid rule's positive number (a line number could, and
		// so could the same line in two included files), and a valid
		// rule keeps its own number.
		return `
            SELECT
                rule_number,
                file_name,
                line_number,
                type,
                database,
                user_name,
                address,
                netmask,
                auth_method,
                options,
                error
            FROM (
                SELECT
                    COALESCE(
                        r.rule_number,
                        -(ROW_NUMBER() OVER (
                            PARTITION BY r.rule_number IS NULL
                            ORDER BY r.file_name, r.line_number, r.error
                        ))::integer
                    ) AS rule_number,
                    r.file_name,
                    r.line_number,
                    r.type,
                    r.database,
                    r.user_name,
                    r.address,
                    r.netmask,
                    r.auth_method,
                    r.options,
                    r.error
                FROM pg_hba_file_rules r
            ) hba
            ORDER BY rule_number
        `
	}
	// PG14-15: rule_number and file_name don't exist. There are no
	// include files before PG16, so the line number is unique and
	// keys valid and error rows alike.
	return `
        SELECT
            line_number AS rule_number,
            NULL::text AS file_name,
            line_number,
            type,
            database,
            user_name,
            address,
            netmask,
            auth_method,
            options,
            error
        FROM pg_hba_file_rules
        ORDER BY line_number
    `
}

// Execute runs the probe against a monitored connection
func (p *PgHbaFileRulesProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	query := WrapQuery(ProbeNamePgHbaFileRules, p.GetQueryForVersion(pgVersion))
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore only if data has changed
func (p *PgHbaFileRulesProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	// Mask the secrets in options and the values quoted in error text
	// before change detection, so the comparison and the stored rows
	// both see the masked form.
	metrics = maskHBASecrets(metrics)

	// Check if data has changed
	changed, err := HasDataChanged(ctx, datastoreConn, connectionID, "pg_hba_file_rules", metrics,
		`SELECT rule_number, file_name, line_number, type, database,
		        user_name, address, netmask, auth_method, options, error
		 FROM metrics.pg_hba_file_rules
		 WHERE connection_id = $1
		   AND collected_at = (
		       SELECT MAX(collected_at)
		       FROM metrics.pg_hba_file_rules
		       WHERE connection_id = $1
		   )
		 ORDER BY rule_number`,
		nil)
	if err != nil {
		return fmt.Errorf("failed to check if data changed: %w", err)
	}

	if !changed {
		// Data unchanged, skip storage
		return nil
	}

	logger.Infof("pg_hba_file_rules data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"rule_number", "file_name", "line_number", "type",
		"database", "user_name", "address", "netmask",
		"auth_method", "options", "error",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["rule_number"],
			metric["file_name"],
			metric["line_number"],
			metric["type"],
			metric["database"],
			metric["user_name"],
			metric["address"],
			metric["netmask"],
			metric["auth_method"],
			metric["options"],
			metric["error"],
		}
		values = append(values, row)
	}

	// Store metrics
	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// hbaMask replaces a value masked in pg_hba_file_rules, as PostgreSQL
// masks a password in pg_stat_wal_receiver.conninfo.
const hbaMask = "********"

// hbaSecretOptions are the options pg_hba_file_rules.options reports
// with a credential as the value. PostgreSQL (14 to 18) prints every
// option as name=value, and these two are the only secrets among them.
// Migration 24 masks the same names in rows stored before this was
// added, so the two lists must change together.
var hbaSecretOptions = map[string]bool{
	"ldapbindpasswd": true,
	"radiussecrets":  true,
}

// maskHBASecrets returns metrics with every error value passed through
// maskHBAError and every options value through maskHBAOptions. Rows
// that need masking are copied, so the caller's maps are never
// changed; the other rows are shared.
func maskHBASecrets(metrics []map[string]any) []map[string]any {
	var masked []map[string]any
	for i, metric := range metrics {
		msg, hasError := metric["error"].(string)
		hasError = hasError && msg != ""
		options, hasSecret := maskHBAOptions(metric["options"])
		if !hasError && !hasSecret {
			continue
		}
		if masked == nil {
			masked = make([]map[string]any, len(metrics))
			copy(masked, metrics)
		}
		row := make(map[string]any, len(metric))
		for k, v := range metric {
			row[k] = v
		}
		if hasError {
			row["error"] = maskHBAError(msg)
		}
		if hasSecret {
			row["options"] = options
		}
		masked[i] = row
	}
	if masked == nil {
		return metrics
	}
	return masked
}

// maskHBAOptions masks the value of each option named in
// hbaSecretOptions, giving for example "ldapbindpasswd=********". It
// returns a new array and true when an element was masked, and the
// value unchanged and false otherwise. pgx scans a text[] as []any;
// []string is accepted as well.
func maskHBAOptions(value any) (any, bool) {
	switch opts := value.(type) {
	case []any:
		var out []any
		for i, o := range opts {
			s, ok := o.(string)
			if !ok {
				continue
			}
			if m, changed := maskHBAOption(s); changed {
				if out == nil {
					out = make([]any, len(opts))
					copy(out, opts)
				}
				out[i] = m
			}
		}
		if out != nil {
			return out, true
		}
	case []string:
		var out []string
		for i, s := range opts {
			if m, changed := maskHBAOption(s); changed {
				if out == nil {
					out = make([]string, len(opts))
					copy(out, opts)
				}
				out[i] = m
			}
		}
		if out != nil {
			return out, true
		}
	}
	return value, false
}

// maskHBAOption masks one name=value option whose name is in
// hbaSecretOptions, and reports whether the result differs from the
// input, so an option already masked is reported unchanged.
func maskHBAOption(option string) (string, bool) {
	name, _, found := strings.Cut(option, "=")
	if !found || !hbaSecretOptions[name] {
		return option, false
	}
	masked := name + "=" + hbaMask
	return masked, masked != option
}

// maskHBAError masks the values PostgreSQL quotes from pg_hba.conf in
// the error for a line it could not parse, keeping its description of
// the problem. A quoted value can hold a credential, as in
// "authentication option not in name=value format: ldapbindpasswdS3cret".
// Every message in PostgreSQL's hba.c (16 to 18) that quotes the line
// puts the value in double quotes or after a colon, so each line of
// the error is cut at its first double quote or colon and the rest is
// replaced by hbaMask. A message with neither, such as
// "end-of-line before IP address specification", is kept whole. The
// errors from several included files are joined by newlines, so each
// line is masked separately. Masking an error twice gives the same
// result as masking it once.
func maskHBAError(msg string) string {
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		cut := strings.IndexAny(line, `":`)
		if cut < 0 {
			continue
		}
		if line[cut] == '"' {
			lines[i] = line[:cut] + `"` + hbaMask + `"`
		} else {
			lines[i] = line[:cut] + ": " + hbaMask
		}
	}
	return strings.Join(lines, "\n")
}
