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

// datastoreStatementOutputs returns the result columns of req that hold
// statement text: each statement-text column the rows and latest shapes
// select, and for aggregate each such group_by column and each min or
// max of one. count of one is a number and needs no masking, and avg and
// sum refuse it.
func datastoreStatementOutputs(src datastoreSource, req datastoreRequest) []string {
	var out []string
	if req.Shape != datastoreShapeAggregate {
		columns := req.Columns
		if len(columns) == 0 {
			columns = src.columnNames()
		}
		for _, name := range columns {
			if statementColumn(src, name) {
				out = append(out, name)
			}
		}
		return out
	}
	for _, name := range req.GroupBy {
		if statementColumn(src, name) {
			out = append(out, name)
		}
	}
	if req.Aggregation == "min" || req.Aggregation == "max" {
		for _, name := range req.Columns {
			if statementColumn(src, name) {
				out = append(out, req.Aggregation+"_"+name)
			}
		}
	}
	return out
}

// redactDatastoreStatements masks each of outputs, the result columns of
// qr holding statement text, from the stored text the statement selected
// beside it (metrics.RedactStatementValue), which replaces a statement
// whole when there is none. It then drops every stored-text column and
// formats the result again.
func redactDatastoreStatements(qr *queryResult, outputs []string) {
	for _, name := range outputs {
		i := slices.Index(qr.ColumnNames, name)
		if i < 0 {
			continue
		}
		raw := slices.Index(qr.ColumnNames, metrics.RawStatementAlias(name))
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
