/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package anomaly

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// lsnWeightTerm matches one decoded hex digit in lsnHalfSQL's output,
// capturing its position and its weight.
var lsnWeightTerm = regexp.MustCompile(`, (\d), 1\)\) - 1\)::bigint \* (\d+)`)

// TestLSNHalfSQLWeights checks without a database that lsnHalfSQL
// weights the eight padded hex digits by descending powers of 16. The
// alerter's TestLSNDiffBytesSQLMatchesPgLSN checks the whole expression
// against pg_lsn subtraction on a database.
func TestLSNHalfSQLWeights(t *testing.T) {
	sql := lsnHalfSQL("r.sent_lsn", 2)
	if !strings.Contains(sql, "split_part(r.sent_lsn, '/', 2)") {
		t.Fatalf("lsnHalfSQL does not split the column: %s", sql)
	}
	terms := lsnWeightTerm.FindAllStringSubmatch(sql, -1)
	if len(terms) != 8 {
		t.Fatalf("lsnHalfSQL has %d digit terms, want 8: %s", len(terms), sql)
	}
	for i, m := range terms {
		pos, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("digit term %d position %q: %v", i, m[1], err)
		}
		weight, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			t.Fatalf("digit term %d weight %q: %v", i, m[2], err)
		}
		if pos != i+1 || weight != int64(1)<<(4*(7-i)) {
			t.Errorf("digit term %d = position %d weight %d", i, pos, weight)
		}
	}
}

// TestLSNDiffBytesSQL checks that the difference combines the two halves
// of each LSN and never casts to pg_lsn, which DuckDB lacks.
func TestLSNDiffBytesSQL(t *testing.T) {
	sql := LSNDiffBytesSQL("a", "b")
	for _, want := range []string{
		lsnHalfSQL("a", 1), lsnHalfSQL("b", 1),
		lsnHalfSQL("a", 2), lsnHalfSQL("b", 2),
		"* 4294967296 +", ")::float",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("LSNDiffBytesSQL lacks %q: %s", want, sql)
		}
	}
	if strings.Contains(sql, "pg_lsn") {
		t.Errorf("LSNDiffBytesSQL uses pg_lsn: %s", sql)
	}
	if ReplicationLagBytesExpr != LSNDiffBytesSQL("r.sent_lsn", "r.replay_lsn") {
		t.Error("ReplicationLagBytesExpr does not diff r.sent_lsn and r.replay_lsn")
	}
}
