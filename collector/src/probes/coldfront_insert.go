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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// coldFrontBatchRows caps the rows written by one statement under
// ColdFront. Each statement becomes one Iceberg commit, so a probe run
// should normally be a single statement: this is above what a probe
// produces per run on a typical server (pg_stat_statements keeps 5,000
// statements by default, pg_settings about 350 rows).
const coldFrontBatchRows = 5000

// coldFrontBatchBytes caps the encoded JSON of one statement. Row counts
// alone do not bound a batch, because free text such as
// pg_stat_statements.query is unbounded and comes from the monitored
// server: 5,000 large rows could pass jsonb's 256 MB limit or the 1 GB
// limit on the literal INSERT the helper builds, failing the probe's
// whole run on every cycle. 16 MiB keeps the helper's working copies
// (the jsonb, the typed rows and the rendered INSERT, each of the same
// order) well inside those limits and server memory, while a typical
// pg_stat_statements run of a few thousand statements still fits in one
// commit. A single row larger than this is still sent on its own.
const coldFrontBatchBytes = 16 << 20

// maxColdFrontDroppedRows bounds how many rows one StoreMetrics call
// drops one by one before it treats the failure as not row-specific and
// returns it, so a systemic failure costs a bounded number of statements
// and log lines rather than one per row.
const maxColdFrontDroppedRows = 10

// coldFrontInsertStatement writes one batch through the datastore's
// workbench_coldfront.insert_rows; see
// database.EnsureColdFrontInsertHelper. ColdFront rewrites a cold-tier
// INSERT with bound parameters into a function call, which caps it at 99
// parameters, so the rows travel as one jsonb parameter and the helper
// turns them into a literal multi-row INSERT on the server.
var coldFrontInsertStatement = sqlmarker.Tag(
	"SELECT " + coldfront.InsertRowsFunction + "($1, $2::jsonb)")

// coldFrontRow is one encoded row and the connection it belongs to,
// which is all that is logged if ColdFront refuses the row.
type coldFrontRow struct {
	json       json.RawMessage
	connection any
}

// coldFrontWriter writes a batch of encoded rows as one statement.
type coldFrontWriter func(ctx context.Context, rows []coldFrontRow) error

// storeColdFrontMetrics is StoreMetrics under ColdFront.
//
// Each statement runs on its own rather than in one transaction: every
// statement is already its own Iceberg commit, and a failed statement
// would otherwise abort the transaction, which ColdFront gives no
// SAVEPOINT to recover from.
func storeColdFrontMetrics(ctx context.Context, conn *pgxpool.Conn, tableName string, columns []string, values [][]any) error {
	// convertForColdFront refuses a table without a specification, so
	// the lookup after it always succeeds.
	values, err := convertForColdFront(tableName, columns, values)
	if err != nil {
		return err
	}
	spec, _ := coldfront.Lookup(tableName)

	rows, err := encodeColdFrontRows(spec, columns, values)
	if err != nil {
		return err
	}

	write := func(ctx context.Context, batch []coldFrontRow) error {
		_, err := conn.Exec(ctx, coldFrontInsertStatement, tableName, joinColdFrontRows(batch))
		return err
	}
	return writeColdFrontBatches(ctx, tableName, rows, write)
}

// writeColdFrontBatches writes rows in batches bounded by
// coldFrontBatchRows and coldFrontBatchBytes. A batch that fails with an
// error a single row can cause is retried in halves, so one row that
// ColdFront refuses is dropped and logged rather than losing the run;
// any other error, or more than maxColdFrontDroppedRows dropped rows, is
// returned.
func writeColdFrontBatches(ctx context.Context, tableName string, rows []coldFrontRow, write coldFrontWriter) error {
	dropped := 0
	for _, batch := range splitColdFrontBatches(rows, coldFrontBatchRows, coldFrontBatchBytes) {
		if err := writeOrHalve(ctx, tableName, batch, write, &dropped); err != nil {
			var refused *ColdFrontRowsRefusedError
			if errors.As(err, &refused) {
				refused.Rows = len(rows)
			}
			return fmt.Errorf("failed to execute INSERT: %w", err)
		}
	}
	return nil
}

// ColdFrontRowsRefusedError is returned by StoreMetrics under ColdFront
// when more than maxColdFrontDroppedRows rows of one call were refused
// one by one. It carries the SQLSTATE of the refusal that hit the limit
// but not the database's message, which can quote the rejected value
// (`invalid input syntax for type integer: "<value>"`), so it
// deliberately does not wrap the *pgconn.PgError: errors.As for one does
// not match it. A caller that needs the code calls SQLState, the method
// pgx-aware code looks for through errors.As on
// interface{ SQLState() string }.
type ColdFrontRowsRefusedError struct {
	// Table is the metrics table, without the schema.
	Table string
	// Code is the SQLSTATE of the refusal that hit the limit.
	Code string
	// Dropped is how many rows were dropped before the limit was hit.
	Dropped int
	// Rows is how many rows the StoreMetrics call was given.
	Rows int
}

// Error describes the failure without any value from the rows.
func (e *ColdFrontRowsRefusedError) Error() string {
	return fmt.Sprintf("ColdFront refused more than %d rows of metrics.%s one by one "+
		"(SQLSTATE %s, %d of %d rows dropped)", e.Dropped, e.Table, e.Code, e.Dropped, e.Rows)
}

// SQLState returns the SQLSTATE of the refusal that hit the limit.
func (e *ColdFrontRowsRefusedError) SQLState() string {
	return e.Code
}

// writeOrHalve writes batch, splitting it in two and retrying each half
// when the failure could be caused by a single row.
func writeOrHalve(ctx context.Context, tableName string, batch []coldFrontRow, write coldFrontWriter, dropped *int) error {
	err := write(ctx, batch)
	if err == nil {
		return nil
	}
	code, rowSpecific := rowSpecificColdFrontError(err)
	if !rowSpecific {
		return err
	}
	if len(batch) == 1 {
		if *dropped >= maxColdFrontDroppedRows {
			// The database's message is left out, as in the log line
			// below: it can quote the rejected value.
			return &ColdFrontRowsRefusedError{Table: tableName, Code: code, Dropped: *dropped}
		}
		*dropped++
		// Only the table, the connection and the SQLSTATE are logged:
		// the error text can quote the rejected value.
		logger.Errorf("Dropped a row of metrics.%s for connection %v that ColdFront refused (SQLSTATE %s)",
			tableName, batch[0].connection, code)
		return nil
	}
	mid := len(batch) / 2
	if err := writeOrHalve(ctx, tableName, batch[:mid], write, dropped); err != nil {
		return err
	}
	return writeOrHalve(ctx, tableName, batch[mid:], write, dropped)
}

// rowSpecificColdFrontError returns the SQLSTATE of err and whether one
// bad row's data can cause it: a data exception (22), an integrity
// violation (23), a program limit such as jsonb's size (54), or an
// internal error (XX000) that is a DuckDB "Conversion Error", which is
// how pg_duckdb reports a value DuckDB cannot convert.
//
// Everything else affects every row, and splitting the batch would only
// repeat it once per row: a connection failure, a missing function, a
// permission error, and every other XX000, such as pg_duckdb's "SAVEPOINT
// and subtransactions are not supported", which live once made the
// collector drop rows one by one for an error no row caused. A message
// naming something as not supported is never treated as row-specific.
func rowSpecificColdFrontError(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || len(pgErr.Code) < 2 {
		return "", false
	}
	msg := strings.ToLower(pgErr.Message)
	if strings.Contains(msg, "not supported") || strings.Contains(msg, "not implemented") ||
		strings.Contains(msg, "savepoint") || strings.Contains(msg, "subtransaction") {
		return pgErr.Code, false
	}
	switch pgErr.Code[:2] {
	case "22", "23", "54":
		return pgErr.Code, true
	case "XX":
		return pgErr.Code, pgErr.Code == "XX000" && strings.Contains(pgErr.Message, "Conversion Error")
	}
	return pgErr.Code, false
}

// splitColdFrontBatches splits rows into consecutive batches of at most
// maxRows rows and, unless a single row is larger, at most maxBytes of
// encoded JSON.
func splitColdFrontBatches(rows []coldFrontRow, maxRows, maxBytes int) [][]coldFrontRow {
	var (
		batches [][]coldFrontRow
		start   int
		size    int
	)
	for i, r := range rows {
		n := len(r.json) + 1 // the separating comma or bracket
		if i > start && (i-start >= maxRows || size+n > maxBytes) {
			batches = append(batches, rows[start:i])
			start, size = i, 0
		}
		size += n
	}
	if start < len(rows) {
		batches = append(batches, rows[start:])
	}
	return batches
}

// joinColdFrontRows renders a batch as the JSON array the helper takes.
func joinColdFrontRows(rows []coldFrontRow) []byte {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(r.json)
	}
	b.WriteByte(']')
	return b.Bytes()
}

// encodeColdFrontRows encodes each row as a JSON object keyed by column
// name. Every column must be in the table's specification, since the
// helper would otherwise drop an unknown key without a word. Values are
// made JSON-faithful by jsonValue; NaN and infinite floats, which JSON
// cannot carry, are stored as NULL and reported in one log line per
// column.
func encodeColdFrontRows(spec coldfront.Table, columns []string, values [][]any) ([]coldFrontRow, error) {
	types, connCol, err := coldFrontColumnTypes(spec, columns)
	if err != nil {
		return nil, err
	}

	nonFinite := map[string]int{}
	rows := make([]coldFrontRow, len(values))
	for i, row := range values {
		b, err := encodeColdFrontRow(spec.Name, types, columns, row, nonFinite)
		if err != nil {
			return nil, err
		}
		rows[i].json = b
		if connCol >= 0 && connCol < len(row) {
			rows[i].connection = row[connCol]
		}
	}
	for name, n := range nonFinite {
		logger.Infof("Storing %d NaN or infinite value(s) of metrics.%s.%s as NULL: ColdFront rows travel as JSON, which cannot represent them",
			n, spec.Name, name)
	}
	return rows, nil
}

// coldFrontColumnTypes returns the PostgreSQL type of each column in the
// table's specification and the position of connection_id in columns, or
// -1 if it is absent. A column missing from the specification is an
// error.
func coldFrontColumnTypes(spec coldfront.Table, columns []string) (map[string]string, int, error) {
	types := make(map[string]string, len(spec.Columns))
	for _, c := range spec.Columns {
		types[c.Name] = c.PGType
	}
	connCol := -1
	for i, name := range columns {
		if _, ok := types[name]; !ok {
			return nil, -1, fmt.Errorf("metrics.%s has no column %s in its ColdFront specification", spec.Name, name)
		}
		if name == "connection_id" {
			connCol = i
		}
	}
	return types, connCol, nil
}

// encodeColdFrontRow encodes one row of metrics.table as a JSON object
// keyed by column name, ignoring columns beyond the end of the row. A
// NaN or infinite float is stored as NULL and counted in nonFinite under
// its column name.
func encodeColdFrontRow(table string, types map[string]string, columns []string, row []any, nonFinite map[string]int) (json.RawMessage, error) {
	obj := make(map[string]any, len(columns))
	for j, name := range columns {
		if j >= len(row) {
			break
		}
		v, err := jsonValue(types[name], row[j])
		if errors.Is(err, errNonFinite) {
			nonFinite[name]++
			v = nil
		} else if err != nil {
			return nil, fmt.Errorf("metrics.%s.%s: %w", table, name, err)
		}
		obj[name] = v
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("metrics.%s: failed to encode rows: %w", table, err)
	}
	return b, nil
}

// errInvalidJSON reports a json or jsonb value that is not a JSON document.
var errInvalidJSON = errors.New("value is not valid JSON")

// errNonFinite marks a NaN or infinite float, which JSON cannot carry.
var errNonFinite = errors.New("non-finite float")

// jsonValue returns v in a form encoding/json renders faithfully for a
// column of the given PostgreSQL type, which jsonb_populate_recordset
// then reads back as the same value:
//
//   - time.Time marshals as RFC 3339 with nanoseconds, and integers and
//     pgtype.Numeric as exact JSON numbers, which jsonb keeps exactly.
//   - pgtype.Interval has no JSON form, so it becomes PostgreSQL interval
//     input text in months, days and microseconds.
//   - A string or []byte for a json or jsonb column is the document
//     itself, as pgx would send it, so it is embedded rather than quoted
//     as a JSON string; invalid JSON is an error, as it is in PostgreSQL.
//   - A NaN or infinite float returns errNonFinite.
func jsonValue(pgType string, v any) (any, error) {
	switch x := v.(type) {
	case float64:
		return finiteFloat(v, x)
	case float32:
		return finiteFloat(v, float64(x))
	case time.Time:
		return x.Format(time.RFC3339Nano), nil
	case pgtype.Interval:
		return intervalText(x), nil
	case string:
		if isJSONType(pgType) {
			return jsonDocument([]byte(x))
		}
	case []byte:
		if isJSONType(pgType) {
			return jsonDocument(x)
		}
	}
	return v, nil
}

// finiteFloat returns v, whose value is f, or errNonFinite if f is NaN
// or infinite.
func finiteFloat(v any, f float64) (any, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, errNonFinite
	}
	return v, nil
}

// intervalText renders an interval as PostgreSQL interval input text in
// months, days and microseconds, or nil for a NULL interval.
func intervalText(x pgtype.Interval) any {
	if !x.Valid {
		return nil
	}
	return fmt.Sprintf("%d months %d days %d microseconds", x.Months, x.Days, x.Microseconds)
}

// isJSONType reports whether pgType is json or jsonb.
func isJSONType(pgType string) bool {
	return pgType == "jsonb" || pgType == "json"
}

// jsonDocument returns doc as a JSON document to embed, or
// errInvalidJSON if it is not valid JSON.
func jsonDocument(doc []byte) (any, error) {
	if !json.Valid(doc) {
		return nil, errInvalidJSON
	}
	return json.RawMessage(doc), nil
}
