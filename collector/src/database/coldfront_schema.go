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
// against that specification; COMMENT ON TABLE becomes COMMENT ON VIEW,
// because the relation ColdFront creates is a view; COMMENT ON COLUMN
// runs unchanged; and CREATE INDEX and COMMENT ON INDEX are dropped, because
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

	keep, err := translateMetricsDDL(ctx, tx, table, spec, ddl)
	if err != nil {
		return err
	}
	for _, stmt := range keep {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// translateMetricsDDL walks the statements of a metrics DDL block under
// ColdFront, as execMetricsDDL describes. It creates the table when it
// reaches the CREATE TABLE, and returns the statements still to be run
// afterwards.
func translateMetricsDDL(ctx context.Context, tx ddlExecer, table string, spec coldfront.Table, ddl string) ([]string, error) {
	var keep []string
	created := false
	for _, stmt := range splitSQLStatements(ddl) {
		switch kind := statementKind(stmt); kind {
		case "CREATE TABLE":
			if created {
				return nil, fmt.Errorf("metrics.%s: more than one CREATE TABLE in its DDL block", table)
			}
			if err := createTableFromDDL(ctx, tx, stmt, spec); err != nil {
				return nil, err
			}
			created = true
		case "COMMENT ON TABLE":
			// The relation is a view under ColdFront, and PostgreSQL
			// refuses COMMENT ON TABLE for a view (SQLSTATE 42809).
			keep = append(keep, commentOnTableAsView(stmt))
		case "COMMENT ON COLUMN":
			// COMMENT ON COLUMN accepts a view's columns as it is.
			keep = append(keep, stmt)
		case "CREATE INDEX", "CREATE UNIQUE INDEX", "COMMENT ON INDEX":
			// ColdFront views cannot be indexed.
		default:
			return nil, fmt.Errorf("metrics.%s: unexpected %q statement in its DDL block under ColdFront", table, kind)
		}
	}
	if !created {
		return nil, fmt.Errorf("metrics.%s: no CREATE TABLE in its DDL block", table)
	}
	return keep, nil
}

// createTableFromDDL checks a metrics CREATE TABLE statement against the
// table's column specification and then creates the table through
// ColdFront.
func createTableFromDDL(ctx context.Context, tx ddlExecer, stmt string, spec coldfront.Table) error {
	if err := checkCreateTableMatchesSpec(stmt, spec); err != nil {
		return err
	}
	return createColdFrontTable(ctx, tx, spec)
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

// metricsAlterTarget captures the metrics relation an ALTER TABLE
// statement changes.
var metricsAlterTarget = regexp.MustCompile(`(?is)^ALTER\s+TABLE\s+metrics\.(\w+)\s`)

// metricsCommentsQuery returns the COMMENT statements that set a metrics
// relation's current comment and its column comments again, view first
// and then columns in attribute order. The statements are built on the
// server with format(%I, %L), so the quoting is PostgreSQL's own and
// does not depend on how a client escapes literals.
const metricsCommentsQuery = `
    SELECT COALESCE(array_agg(stmt ORDER BY ord), '{}')
    FROM (
        SELECT 0 AS ord,
               format('COMMENT ON VIEW %I.%I IS %L', n.nspname, c.relname, d.description) AS stmt
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_description d
          ON d.objoid = c.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0
        WHERE n.nspname = 'metrics' AND c.relname = $1
        UNION ALL
        SELECT a.attnum,
               format('COMMENT ON COLUMN %I.%I.%I IS %L', n.nspname, c.relname, a.attname, d.description)
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
        JOIN pg_description d
          ON d.objoid = c.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = a.attnum
        WHERE n.nspname = 'metrics' AND c.relname = $1
    ) s
`

// metricsCommentStatements returns the COMMENT statements that set the
// metrics view's current comment and column comments again.
func metricsCommentStatements(ctx context.Context, q rowQuerier, view string) ([]string, error) {
	var stmts []string
	if err := q.QueryRow(ctx, metricsCommentsQuery, view).Scan(&stmts); err != nil {
		return nil, fmt.Errorf("failed to read the comments on metrics.%s: %w", view, err)
	}
	return stmts, nil
}

// migrationTx is what execMetricsAlter needs from a migration
// transaction.
type migrationTx interface {
	ddlExecer
	rowQuerier
}

// execMetricsAlter runs a block of SQL that alters metrics relations,
// as migrations 7, 10, 12 and 13 do with ALTER TABLE ... ADD COLUMN. In
// the default mode the block runs exactly as written.
//
// Under ColdFront each statement runs on its own, and around each ALTER
// TABLE of a metrics relation the relation's comments are read before
// and written back after. ColdFront applies an ALTER TABLE to one of its
// views by dropping the view and creating it again
// (coldfront._rebuild_iceberg_view), which keeps the owner and grants
// but loses the view comment and every column comment; on a live
// datastore the five views these migrations alter were left without
// their comments.
func (sm *SchemaManager) execMetricsAlter(ctx context.Context, tx migrationTx, sql string) error {
	if !sm.useColdFrontForMetrics {
		_, err := tx.Exec(ctx, sql)
		return err
	}
	for _, stmt := range splitSQLStatements(sql) {
		m := metricsAlterTarget.FindStringSubmatch(stmt)
		if m == nil {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return err
			}
			continue
		}
		restore, err := metricsCommentStatements(ctx, tx, m[1])
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
		for _, c := range restore {
			if _, err := tx.Exec(ctx, c); err != nil {
				return fmt.Errorf("failed to restore a comment on metrics.%s: %w", m[1], err)
			}
		}
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
			i = copyQuoted(&cur, sql, i)
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			i = skipLineComment(sql, i)
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

// copyQuoted copies the quoted text that opens at sql[i] to cur, through
// its closing quote, and returns the index of the last byte copied. A
// doubled quote is an escaped quote and keeps the literal open; an
// unterminated literal is copied to the end of sql.
func copyQuoted(cur *strings.Builder, sql string, i int) int {
	q := sql[i]
	cur.WriteByte(q)
	for i++; i < len(sql); i++ {
		cur.WriteByte(sql[i])
		if sql[i] != q {
			continue
		}
		if i+1 < len(sql) && sql[i+1] == q {
			i++
			cur.WriteByte(sql[i])
			continue
		}
		return i
	}
	return i
}

// skipLineComment returns the index of the newline that ends the --
// comment starting at sql[i], or len(sql) if the comment runs to the end.
func skipLineComment(sql string, i int) int {
	for i < len(sql) && sql[i] != '\n' {
		i++
	}
	return i
}

// statementKindPattern captures the leading keywords that identify a
// statement: the verb and, for CREATE and COMMENT, the object type.
var statementKindPattern = regexp.MustCompile(
	`^(?i)(CREATE\s+UNIQUE\s+INDEX|CREATE\s+(?:TABLE|INDEX)|COMMENT\s+ON\s+(?:TABLE|COLUMN|INDEX)|\S+)`)

// commentOnTablePrefix matches the leading keywords of COMMENT ON TABLE.
var commentOnTablePrefix = regexp.MustCompile(`^(?i)COMMENT\s+ON\s+TABLE\b`)

// commentOnTableAsView rewrites a COMMENT ON TABLE statement as COMMENT
// ON VIEW, leaving the object name and the comment text untouched.
func commentOnTableAsView(stmt string) string {
	return commentOnTablePrefix.ReplaceAllLiteralString(stmt, "COMMENT ON VIEW")
}

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

	items, err := splitColumnList(stmt[open+1:])
	if err != nil {
		return nil, err
	}

	var cols []coldfront.Column
	for _, item := range items {
		col, ok, err := parseColumnDefinition(item)
		if err != nil {
			return nil, err
		}
		if ok {
			cols = append(cols, col)
		}
	}
	return cols, nil
}

// splitColumnList splits the text after the opening parenthesis of a
// CREATE TABLE column list into its comma-separated items, up to the
// matching closing parenthesis. Commas inside nested parentheses or
// single-quoted literals do not separate items.
func splitColumnList(body string) ([]string, error) {
	var (
		items   []string
		cur     strings.Builder
		depth   int
		done    bool
		inQuote bool
	)
	for _, r := range body {
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
			done = depth == 0
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
	return append(items, cur.String()), nil
}

// parseColumnDefinition parses one item of a CREATE TABLE column list as
// a format_type()-normalised column. It reports false, with no error,
// for an empty item or a table constraint.
func parseColumnDefinition(item string) (coldfront.Column, bool, error) {
	fields := strings.Fields(item)
	if len(fields) == 0 || tableConstraintKeywords[strings.ToUpper(fields[0])] {
		return coldfront.Column{}, false, nil
	}
	var typeWords []string
	for _, f := range fields[1:] {
		if columnOptionKeywords[strings.ToUpper(f)] {
			break
		}
		typeWords = append(typeWords, f)
	}
	if len(typeWords) == 0 {
		return coldfront.Column{}, false, fmt.Errorf("column %s has no type", fields[0])
	}
	return coldfront.Column{
		Name:   fields[0],
		PGType: normalizeDDLType(strings.Join(typeWords, " ")),
	}, true, nil
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
