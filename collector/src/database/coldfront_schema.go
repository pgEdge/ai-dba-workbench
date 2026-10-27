/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
)

// SchemaManagerOption configures a SchemaManager.
type SchemaManagerOption func(*SchemaManager)

// WithColdFrontMetrics selects where the migrations create the metrics.*
// tables. With false, the default, they are partitioned PostgreSQL
// tables. With true, each is created through
// coldfront.create_iceberg_table, and the migrations issue no index,
// constraint, partition or SAVEPOINT statement against them.
func WithColdFrontMetrics(enabled bool) SchemaManagerOption {
	return func(sm *SchemaManager) {
		sm.useColdFrontForMetrics = enabled
	}
}

// ddlExecer is the part of pgx.Tx the ColdFront DDL translation uses.
type ddlExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// createIcebergTableSQL creates one metrics table through ColdFront. The
// table name, column specification and partition specification are all
// bound as parameters.
const createIcebergTableSQL = `
    SELECT coldfront.create_iceberg_table(
        p_schema => 'metrics',
        p_table => $1,
        p_columns => $2::jsonb,
        p_partition_cols => $3::text[])
`

// execMetricsDDL runs the DDL block that creates the metrics table named
// table. In the default mode the block is executed exactly as written.
// Under ColdFront the block is translated statement by statement: its
// CREATE TABLE becomes a coldfront.create_iceberg_table call built from
// the column specification, after the CREATE TABLE has been checked
// against that specification; COMMENT ON TABLE and COMMENT ON COLUMN run
// unchanged; and CREATE INDEX and COMMENT ON INDEX are dropped, because
// ColdFront views carry no indexes. Any other statement is an error, so a
// new kind of statement in a metrics block cannot be silently skipped.
func (sm *SchemaManager) execMetricsDDL(ctx context.Context, tx ddlExecer, table, ddl string) error {
	if !sm.useColdFrontForMetrics {
		_, err := tx.Exec(ctx, ddl)
		return err
	}

	spec, ok := coldfront.Lookup(table)
	if !ok {
		return fmt.Errorf("metrics.%s has no ColdFront column specification", table)
	}

	var keep []string
	created := false
	for _, stmt := range splitSQLStatements(ddl) {
		switch kind := statementKind(stmt); kind {
		case "CREATE TABLE":
			if created {
				return fmt.Errorf("metrics.%s: more than one CREATE TABLE in its DDL block", table)
			}
			if err := checkCreateTableMatchesSpec(stmt, spec); err != nil {
				return err
			}
			if err := createColdFrontTable(ctx, tx, spec); err != nil {
				return err
			}
			created = true
		case "COMMENT ON TABLE", "COMMENT ON COLUMN":
			keep = append(keep, stmt)
		case "CREATE INDEX", "CREATE UNIQUE INDEX", "COMMENT ON INDEX":
			// ColdFront views cannot be indexed.
		default:
			return fmt.Errorf("metrics.%s: unexpected %q statement in its DDL block under ColdFront", table, kind)
		}
	}
	if !created {
		return fmt.Errorf("metrics.%s: no CREATE TABLE in its DDL block", table)
	}

	for _, stmt := range keep {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// createColdFrontTable creates the metrics table described by spec
// through coldfront.create_iceberg_table, partitioned by day of
// collected_at.
func createColdFrontTable(ctx context.Context, tx ddlExecer, spec coldfront.Table) error {
	columns, err := spec.ColumnsJSON()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, createIcebergTableSQL,
		spec.Name, columns, coldfront.PartitionColumns); err != nil {
		return fmt.Errorf("coldfront.create_iceberg_table for metrics.%s: %w", spec.Name, err)
	}
	return nil
}

// addMetricsConstraint adds a constraint to a metrics table in the
// default mode and does nothing under ColdFront, whose views cannot carry
// constraints.
func (sm *SchemaManager) addMetricsConstraint(ctx context.Context, tx pgx.Tx, table, name, definition string) error {
	if sm.useColdFrontForMetrics {
		return nil
	}
	return addConstraintIfMissing(ctx, tx, table, name, definition)
}

// splitSQLStatements splits a block of SQL into its statements, without
// the terminating semicolons, surrounding whitespace or -- comments. It
// understands single-quoted literals (with ” escapes), double-quoted
// identifiers and -- line comments, which is everything the metrics DDL
// blocks contain; it does not handle dollar quoting or block comments.
func splitSQLStatements(sql string) []string {
	var (
		stmts []string
		cur   strings.Builder
	)
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			stmts = append(stmts, s)
		}
		cur.Reset()
	}

	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '\'' || c == '"':
			// Copy the quoted text through its closing quote. A doubled
			// quote is an escaped quote and keeps the literal open.
			cur.WriteByte(c)
			for i++; i < len(sql); i++ {
				cur.WriteByte(sql[i])
				if sql[i] == c {
					if i+1 < len(sql) && sql[i+1] == c {
						i++
						cur.WriteByte(sql[i])
						continue
					}
					break
				}
			}
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			cur.WriteByte('\n')
		case c == ';':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return stmts
}

// statementKindPattern captures the leading keywords that identify a
// statement: the verb and, for CREATE and COMMENT, the object type.
var statementKindPattern = regexp.MustCompile(
	`^(?i)(CREATE\s+UNIQUE\s+INDEX|CREATE\s+(?:TABLE|INDEX)|COMMENT\s+ON\s+(?:TABLE|COLUMN|INDEX)|\S+)`)

// statementKind returns the upper-cased leading keywords of stmt, with
// runs of whitespace collapsed to one space.
func statementKind(stmt string) string {
	kw := statementKindPattern.FindString(stmt)
	return strings.ToUpper(strings.Join(strings.Fields(kw), " "))
}

// tableConstraintKeywords start a table constraint rather than a column
// definition inside CREATE TABLE.
var tableConstraintKeywords = map[string]bool{
	"PRIMARY": true, "FOREIGN": true, "CONSTRAINT": true,
	"UNIQUE": true, "CHECK": true, "EXCLUDE": true,
}

// columnOptionKeywords end the type of a column definition.
var columnOptionKeywords = map[string]bool{
	"NOT": true, "NULL": true, "DEFAULT": true, "PRIMARY": true,
	"REFERENCES": true, "CHECK": true, "UNIQUE": true,
	"CONSTRAINT": true, "COLLATE": true, "GENERATED": true,
}

// ddlTypeAliases maps the type spellings the metrics DDL uses to the
// names format_type() reports, which the column specification uses.
var ddlTypeAliases = map[string]string{
	"timestamptz": "timestamp with time zone",
	"int":         "integer",
	"int4":        "integer",
	"int8":        "bigint",
	"int2":        "smallint",
	"bool":        "boolean",
	"float8":      "double precision",
	"float4":      "real",
}

var varcharPattern = regexp.MustCompile(`^varchar\((\d+)\)$`)

// normalizeDDLType converts a type as written in DDL to its
// format_type() name.
func normalizeDDLType(t string) string {
	t = strings.ToLower(strings.Join(strings.Fields(t), " "))
	elem, isArray := strings.CutSuffix(t, "[]")
	if alias, ok := ddlTypeAliases[elem]; ok {
		elem = alias
	}
	if m := varcharPattern.FindStringSubmatch(elem); m != nil {
		elem = "character varying(" + m[1] + ")"
	}
	if isArray {
		return elem + "[]"
	}
	return elem
}

// parseCreateTableColumns returns the column definitions of a CREATE
// TABLE statement as format_type()-normalised columns, in order. Table
// constraints are skipped.
func parseCreateTableColumns(stmt string) ([]coldfront.Column, error) {
	open := strings.Index(stmt, "(")
	if open < 0 {
		return nil, fmt.Errorf("CREATE TABLE has no column list")
	}

	var (
		items   []string
		cur     strings.Builder
		depth   int
		done    bool
		inQuote bool
	)
	for _, r := range stmt[open+1:] {
		if r == '\'' {
			// A doubled quote toggles twice and so stays inside.
			inQuote = !inQuote
		}
		if inQuote {
			cur.WriteRune(r)
			continue
		}
		switch r {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				done = true
			}
			depth--
		case ',':
			if depth == 0 {
				items = append(items, cur.String())
				cur.Reset()
				continue
			}
		}
		if done {
			break
		}
		cur.WriteRune(r)
	}
	if !done {
		return nil, fmt.Errorf("CREATE TABLE column list is not closed")
	}
	items = append(items, cur.String())

	var cols []coldfront.Column
	for _, item := range items {
		fields := strings.Fields(item)
		if len(fields) == 0 {
			continue
		}
		if tableConstraintKeywords[strings.ToUpper(fields[0])] {
			continue
		}
		var typeWords []string
		for _, f := range fields[1:] {
			if columnOptionKeywords[strings.ToUpper(f)] {
				break
			}
			typeWords = append(typeWords, f)
		}
		if len(typeWords) == 0 {
			return nil, fmt.Errorf("column %s has no type", fields[0])
		}
		cols = append(cols, coldfront.Column{
			Name:   fields[0],
			PGType: normalizeDDLType(strings.Join(typeWords, " ")),
		})
	}
	return cols, nil
}

// checkCreateTableMatchesSpec verifies that a metrics CREATE TABLE
// statement declares exactly the columns, in order and with the same
// types, that the ColdFront specification says the table is created
// with. It keeps the two definitions of every metrics table from
// drifting apart.
func checkCreateTableMatchesSpec(stmt string, spec coldfront.Table) error {
	ddlCols, err := parseCreateTableColumns(stmt)
	if err != nil {
		return fmt.Errorf("metrics.%s: %w", spec.Name, err)
	}
	specCols := spec.CreateColumns()

	describe := func(cols []coldfront.Column) string {
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = c.Name + " " + c.PGType
		}
		return strings.Join(parts, ", ")
	}
	if describe(ddlCols) != describe(specCols) {
		return fmt.Errorf("metrics.%s: the ColdFront column specification "+
			"does not match the CREATE TABLE statement; DDL declares (%s), "+
			"specification has (%s)",
			spec.Name, describe(ddlCols), describe(specCols))
	}
	return nil
}
