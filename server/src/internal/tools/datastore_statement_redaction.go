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
	"slices"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/metrics"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// Statement text in query_datastore results is masked twice: in SQL
// (metrics.RedactedText), where the caller's filters, grouping and
// ordering see it, and then in Go from the text as stored, which the
// statement selects beside each result column holding statement text
// under metrics.RawStatementAlias of that column's name. The lexer in
// metrics.RedactStatementText must read the stored text, since a quote
// the SQL patterns move can make it return the secret after it (GitHub
// issues #670 and #724).

// rawStatementPrefix starts every alias metrics.RawStatementAlias
// returns. No source column or aggregate output begins with it, so a
// result column carrying it is always one the statement selected as
// stored, and never reaches the caller.
var rawStatementPrefix = metrics.RawStatementAlias("")

// statementColumn reports whether name is a statement-text column of
// src.
func statementColumn(src datastoreSource, name string) bool {
	c, ok := src.column(name)
	return ok && c.RedactStatement
}

// rawStatementNames returns the stored-text alias of each statement-text
// column of src among columns, in order.
func rawStatementNames(src datastoreSource, columns []string) []string {
	var out []string
	for _, name := range columns {
		if statementColumn(src, name) {
			out = append(out, metrics.RawStatementAlias(name))
		}
	}
	return out
}

// statementOutput reports whether name, a result column of a query on
// src, may hold statement text: a statement-text column of src, as the
// rows and latest shapes and group_by select it, or the min or max of
// one. It reads the result's own column names rather than the request,
// so that masking does not depend on the query builder naming outputs in
// the same way. count of one is a number, and avg and sum refuse it.
func statementOutput(src datastoreSource, name string) bool {
	if statementColumn(src, name) {
		return true
	}
	for _, agg := range []string{"min_", "max_"} {
		if rest, ok := strings.CutPrefix(name, agg); ok && statementColumn(src, rest) {
			return true
		}
	}
	return false
}

// redactDatastoreStatements masks the statement text in qr, a result of
// a query on src, in Go from the text as stored. Every result column X
// for which the result also holds metrics.RawStatementAlias(X) is masked
// from that stored text (metrics.RedactStatementValue). It fails closed:
// a column that may hold statement text (statementOutput) but has no
// stored text beside it is replaced whole with sqlredact.Redacted,
// rather than returned with only the SQL patterns applied. It then drops
// every stored-text column and formats the result again.
func redactDatastoreStatements(qr *queryResult, src datastoreSource) {
	for i, name := range qr.ColumnNames {
		if strings.HasPrefix(name, rawStatementPrefix) {
			continue
		}
		raw := slices.Index(qr.ColumnNames, metrics.RawStatementAlias(name))
		if raw < 0 && !statementOutput(src, name) {
			continue
		}
		for _, row := range qr.Rows {
			var stored any
			if raw >= 0 {
				stored = row[raw]
			}
			row[i] = metrics.RedactStatementValue(row[i], stored)
		}
	}

	var keep []int
	for i, name := range qr.ColumnNames {
		if !strings.HasPrefix(name, rawStatementPrefix) {
			keep = append(keep, i)
		}
	}
	if len(keep) < len(qr.ColumnNames) {
		names := make([]string, len(keep))
		for k, i := range keep {
			names[k] = qr.ColumnNames[i]
		}
		for r, row := range qr.Rows {
			kept := make([]any, len(keep))
			for k, i := range keep {
				kept[k] = row[i]
			}
			qr.Rows[r] = kept
		}
		qr.ColumnNames = names
	}
	qr.ResultsTSV = tsv.FormatResults(qr.ColumnNames, qr.Rows)
}
