/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package sqlmarker provides a shared marker used to tag SQL statements
// that the Workbench itself issues against its own metadata datastore,
// so that monitoring panels can distinguish Workbench overhead from the
// user's own workload.
//
// The collector wraps its read-only probe queries against monitored
// databases with the separate "ai_dba_wb_probe" column alias (see
// WrapQuery in the collector's probes package). That marker cannot be
// used for the Workbench's own statements against the datastore,
// because those statements are inserts, deletes, DDL, and multi-column
// reads whose shape must not change. This package supplies the
// complementary comment marker for those statements.
//
// Almost all tagged statements run against the datastore, but not
// quite all: the collector also tags the database-list query in its
// scheduler, which runs against each monitored server rather than
// against the datastore.
package sqlmarker

import "strings"

// Marker is the token identifying a statement as Workbench-internal.
// The server's "Hide monitoring queries" filter on the Top Queries
// panel matches on this value, so it must not be changed without
// updating that filter (and accepting that already-collected
// pg_stat_statements rows will keep the old text).
const Marker = "ai_dba_wb_internal"

// ProbeAlias is the synthetic column alias the collector wraps around
// every read-only probe query it runs against a monitored database;
// see WrapQuery in the collector's probes package. It lives here, next
// to Marker, because the server's "Hide monitoring queries" filter
// matches on both and the two values were previously hand-copied
// across module boundaries with nothing to catch a drift.
//
// The same caveat as Marker applies: changing it leaves
// already-collected pg_stat_statements rows carrying the old value.
const ProbeAlias = "ai_dba_wb_probe"

// Comment is the SQL block comment carrying Marker. A block comment is
// used rather than a line comment so that the marker is safe to embed
// in single-line SQL and in statements built by string concatenation.
const Comment = "/* " + Marker + " */"

// Tag returns sql with Comment inserted immediately after its leading
// keyword token, for example:
//
//	INSERT /* ai_dba_wb_internal */ INTO "metrics"."pg_stat_statements" ...
//
// Placement matters, and it is far from obvious why, so do not "tidy"
// the marker to the front of the string: PostgreSQL does not preserve a
// comment in every position when it normalizes a statement for
// pg_stat_statements. Measured on PostgreSQL 18, with the marker in
// each of four positions:
//
//	/* m */ INSERT INTO t ...        marker is stripped
//	INSERT /* m */ INTO t ...        marker is preserved
//	INSERT INTO t VALUES (1) /* m */ marker is preserved
//	INSERT INTO t VALUES (1); -- m   marker is stripped
//
// Only a marker inside the statement survives, and the position
// immediately after the leading keyword is the robust choice because it
// does not depend on where the statement happens to end.
//
// Note also that queryid is computed from the parse tree and ignores
// comments, so a tagged statement and an otherwise identical untagged
// one share a queryid, and pg_stat_statements keeps the text of
// whichever form it saw first.
//
// That has a consequence for upgrades, and it is the opposite of
// reassuring: because these statements are unique to the Workbench,
// the untagged entry already in pg_stat_statements is the one that
// stays. It was recorded before the upgrade, it runs on every
// collection cycle so is never evicted, and its stored text is frozen
// at the untagged form. Tagging the statement therefore does not hide
// it; the marker never appears in the text the view returns, and the
// filter never matches. Only statements first seen after the upgrade
// are hidden, so an existing deployment needs a one-off
// SELECT pg_stat_statements_reset() on the monitored instance for the
// filter to take effect on everything else.
//
// Measured on PostgreSQL 18: five untagged executions followed by 25
// tagged ones leave a single row, 30 calls, with the untagged text.
// The only statements exempt are those whose parse tree also changed,
// which in practice means ones that gained a bind parameter in the
// same change.
//
// Tag is idempotent: sql already containing Marker is returned
// unchanged. Leading whitespace and newlines are tolerated, so
// indented raw string literals are handled correctly. Tag deliberately
// does not parse SQL; it only locates the leading token, and returns
// sql unchanged when the input is empty, whitespace-only, or does not
// begin with an alphabetic keyword (for example a parenthesised
// sub-select or a statement already prefixed with a comment). Where sql
// is a semicolon-separated batch, only the first statement is tagged,
// because Tag locates a single leading token.
//
// One input class would be corrupted rather than merely left alone: a
// string-literal prefix letter at the very start of sql, as in E'x',
// B'1011', X'1f', U&'x', or N'x'. The prefix letter and the opening
// quote must be adjacent, so inserting the marker between them changes
// the meaning; on PostgreSQL 18, SELECT E /* m */ 'x' fails with
// `type "e" does not exist`. No guard is needed because the class is
// unreachable: no valid statement begins with a bare string literal, and
// every call site passes SQL that begins with a keyword. Do not start
// passing caller-composed fragments to Tag without revisiting this.
func Tag(sql string) string {
	if strings.Contains(sql, Marker) {
		return sql
	}

	// Locate the first non-whitespace byte; raw string literals in the
	// collector and alerter commonly begin with a newline and indent.
	start := -1
	for i := 0; i < len(sql); i++ {
		if !isSpaceByte(sql[i]) {
			start = i
			break
		}
	}
	if start < 0 {
		return sql
	}

	// The leading token must be a bare alphabetic keyword. Anything
	// else (a comment introducer, an open parenthesis, a placeholder)
	// means we cannot place the marker safely, so leave sql alone
	// rather than risk corrupting it.
	end := start
	for end < len(sql) && isAlphaByte(sql[end]) {
		end++
	}
	if end == start {
		return sql
	}

	return sql[:end] + " " + Comment + sql[end:]
}

// isSpaceByte reports whether b is an ASCII whitespace byte. SQL
// keywords and whitespace are ASCII, so a byte-wise test is both
// correct and cheap here; multi-byte UTF-8 sequences never match
// because their bytes all have the high bit set.
func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' ||
		b == '\v' || b == '\f'
}

// isAlphaByte reports whether b is an ASCII letter.
func isAlphaByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
