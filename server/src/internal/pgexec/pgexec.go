/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package pgexec holds the small pieces of simple-protocol statement
// handling that the query endpoint and the action executor share: the
// post-statement connection checks and the rendering of result rows.
// Each caller keeps its own message text and result type.
package pgexec

// ConnState is the part of *pgconn.PgConn that the post-statement checks
// read.
type ConnState interface {
	ParameterStatus(key string) string
	TxStatus() byte
}

// txIdle is the transaction status of a connection outside a
// transaction.
const txIdle = 'I'

// EncodingIsUTF8 reports whether the connection is still on a
// client_encoding of UTF8. The classifier reads statement text as UTF-8,
// so a statement that changes the encoding must stop the batch.
func EncodingIsUTF8(c ConnState) bool {
	return c.ParameterStatus("client_encoding") == "UTF8"
}

// TxLeftOpen reports whether a statement left a transaction open.
func TxLeftOpen(c ConnState) bool {
	return c.TxStatus() != txIdle
}

// RowReader is the part of *pgconn.ResultReader that AppendRows reads.
type RowReader interface {
	NextRow() bool
	Values() [][]byte
}

// RowValues renders one simple-protocol row as strings, with a NULL
// column reported as the text NULL.
func RowValues(row [][]byte) []string {
	values := make([]string, len(row))
	for i, col := range row {
		if col == nil {
			values[i] = "NULL"
		} else {
			values[i] = string(col)
		}
	}
	return values
}

// AppendRows reads every row of rr and appends those that fit within
// limit to rows. Rows past the limit are read but not kept, so the
// reader closes cleanly without buffering an unbounded result; the
// second return value reports that some were dropped.
func AppendRows(rows [][]string, rr RowReader, limit int) ([][]string, bool) {
	truncated := false
	for rr.NextRow() {
		if len(rows) >= limit {
			truncated = true
			continue
		}
		rows = append(rows, RowValues(rr.Values()))
	}
	return rows, truncated
}
