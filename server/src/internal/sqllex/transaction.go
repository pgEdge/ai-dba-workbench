/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package sqllex

import "strings"

// IsTransactionControl reports whether stmt is an explicit transaction
// control command: BEGIN, START TRANSACTION, COMMIT (also COMMIT
// PREPARED), END, ROLLBACK (also ROLLBACK TO and ROLLBACK PREPARED),
// ABORT, SAVEPOINT, RELEASE and PREPARE TRANSACTION. A caller that wraps
// a batch in a transaction of its own refuses these, because they would
// end or split that transaction. PREPARE name AS ... is a prepared
// statement, not transaction control, and is not matched.
func IsTransactionControl(stmt string) bool {
	switch FirstKeyword(stmt) {
	case "BEGIN", "COMMIT", "END", "ROLLBACK", "ABORT", "SAVEPOINT", "RELEASE":
		return true
	case "START", "PREPARE":
		words := codeWords(stmt, StandardStrings)
		return len(words) > 1 && words[1] == "TRANSACTION"
	}
	return false
}

// codeWords returns the upper-cased identifier-character runs of stmt's
// code, with comments, literals and quoted identifiers masked out.
func codeWords(stmt string, reading StringReading) []string {
	return sqlWords(strings.ToUpper(MaskNonCode(stmt, reading)))
}

// RequiresNoTransaction reports whether stmt is one PostgreSQL refuses
// to run inside a transaction block, so that a batch containing it
// cannot be made atomic. The list was checked against PostgreSQL 18,
// which reports "cannot run inside a transaction block" for each:
//
//   - VACUUM (every form, VACUUM FULL included)
//   - CREATE INDEX CONCURRENTLY, DROP INDEX CONCURRENTLY and
//     ALTER TABLE ... DETACH PARTITION ... CONCURRENTLY
//   - REINDEX DATABASE, SCHEMA and SYSTEM, and REINDEX with CONCURRENTLY
//     after the target kind or in its option list (REINDEX TABLE and
//     INDEX without CONCURRENTLY are allowed). The target kind is read
//     from its place after the option list, so a table, index or
//     tablespace named like a target kind does not count.
//   - ALTER SYSTEM
//   - CREATE and DROP DATABASE, CREATE and DROP TABLESPACE
//   - ALTER DATABASE ... SET TABLESPACE
//   - CLUSTER with no table name (CLUSTER on a named table is allowed)
//   - DISCARD ALL
//   - CREATE SUBSCRIPTION and DROP SUBSCRIPTION. PostgreSQL only refuses
//     these when a replication slot is created or dropped, which is the
//     default; they are reported as true in every form.
//
// The two ways of being wrong cost different things. A false true runs
// the batch outside a transaction, so it loses atomicity: a later
// failure leaves the earlier statements applied. A false false runs the
// statement in a transaction block, where PostgreSQL refuses it and the
// whole batch rolls back with nothing applied. A false true is accepted
// only where the form cannot be told from the text (the subscription
// slot options); every other entry is matched exactly.
//
// ALTER TYPE ... ADD VALUE is allowed in a transaction block from
// PostgreSQL 12, and PostgreSQL 13 and later are the supported
// versions, so it returns false. The statement is read under both
// readings of a plain '...' literal and counts if either reading says
// it needs no transaction.
func RequiresNoTransaction(stmt string) bool {
	for _, reading := range StringReadings {
		if requiresNoTransactionAs(stmt, reading) {
			return true
		}
	}
	return false
}

func requiresNoTransactionAs(stmt string, reading StringReading) bool {
	words := stmtWords(codeWords(stmt, reading))
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "VACUUM":
		return true
	case "ALTER":
		return alterRequiresNoTransaction(words)
	case "CREATE":
		return createRequiresNoTransaction(words)
	case "DROP":
		return dropRequiresNoTransaction(words)
	case "REINDEX":
		return reindexRequiresNoTransaction(strings.ToUpper(MaskNonCode(stmt, reading)))
	case "DISCARD":
		return words.at(1) == "ALL"
	case "CLUSTER":
		return clusterHasNoTable(stmt)
	}
	return false
}

// alterRequiresNoTransaction reports whether an ALTER statement must run
// outside a transaction block.
func alterRequiresNoTransaction(words stmtWords) bool {
	switch words.at(1) {
	case "SYSTEM":
		return true
	case "DATABASE":
		return words.follows("SET", "TABLESPACE")
	case "TABLE":
		return words.follows("DETACH", "PARTITION") && words.has("CONCURRENTLY")
	}
	return false
}

// createRequiresNoTransaction reports whether a CREATE statement must
// run outside a transaction block.
func createRequiresNoTransaction(words stmtWords) bool {
	switch words.at(1) {
	case "DATABASE", "TABLESPACE", "SUBSCRIPTION":
		return true
	case "INDEX", "UNIQUE":
		return createsIndexConcurrently(words)
	}
	return false
}

// createsIndexConcurrently reports whether a CREATE statement is
// CREATE [ UNIQUE ] INDEX CONCURRENTLY.
func createsIndexConcurrently(words stmtWords) bool {
	if words.at(1) == "UNIQUE" {
		words = words[1:]
	}
	return words.at(1) == "INDEX" && words.at(2) == "CONCURRENTLY"
}

// dropRequiresNoTransaction reports whether a DROP statement must run
// outside a transaction block.
func dropRequiresNoTransaction(words stmtWords) bool {
	switch words.at(1) {
	case "DATABASE", "TABLESPACE", "SUBSCRIPTION":
		return true
	case "INDEX":
		return words.at(2) == "CONCURRENTLY"
	}
	return false
}

// stmtWords holds the upper-cased code words of one statement.
type stmtWords []string

// at returns the word at index i, or "" past the end.
func (w stmtWords) at(i int) string {
	if i < len(w) {
		return w[i]
	}
	return ""
}

// has reports whether word appears anywhere in the statement.
func (w stmtWords) has(word string) bool {
	for _, x := range w {
		if x == word {
			return true
		}
	}
	return false
}

// follows reports whether second appears immediately after first.
func (w stmtWords) follows(first, second string) bool {
	for i := 0; i+1 < len(w); i++ {
		if w[i] == first && w[i+1] == second {
			return true
		}
	}
	return false
}

// reindexRequiresNoTransaction reads a masked, upper-cased REINDEX
// statement and reports whether PostgreSQL refuses it in a transaction
// block: the kind is DATABASE, SCHEMA or SYSTEM, or the rebuild is
// concurrent (see parseReindex).
func reindexRequiresNoTransaction(masked string) bool {
	kind, concurrent := parseReindex(masked)
	switch kind {
	case "DATABASE", "SCHEMA", "SYSTEM":
		return true
	}
	return concurrent
}

// parseReindex reads a masked, upper-cased REINDEX statement,
// REINDEX [ ( option [, ...] ) ] kind [ CONCURRENTLY ] name, and returns
// the target kind and whether the rebuild is concurrent: CONCURRENTLY
// follows the kind, or the option list turns it on. An unbalanced option
// list is a syntax error PostgreSQL reports either way, so it reads as
// no kind and not concurrent.
func parseReindex(masked string) (kind string, concurrent bool) {
	rest := strings.TrimSpace(masked)
	rest = strings.TrimSpace(rest[len("REINDEX"):])
	if strings.HasPrefix(rest, "(") {
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return "", false
		}
		concurrent = reindexOptionsConcurrent(rest[1:end])
		rest = rest[end+1:]
	}
	words := stmtWords(sqlWords(rest))
	if len(words) == 0 {
		return "", false
	}
	return words[0], concurrent || words.at(1) == "CONCURRENTLY"
}

// BuildsIndexConcurrently reports whether stmt builds an index
// concurrently: CREATE [ UNIQUE ] INDEX CONCURRENTLY, or REINDEX with
// CONCURRENTLY after the target kind or in its option list. A failed
// concurrent build can leave an invalid index behind, which PostgreSQL
// does not remove. The statement counts if either reading of a plain
// '...' literal says so.
func BuildsIndexConcurrently(stmt string) bool {
	for _, reading := range StringReadings {
		masked := strings.ToUpper(MaskNonCode(stmt, reading))
		words := stmtWords(sqlWords(masked))
		switch words.at(0) {
		case "CREATE":
			if createsIndexConcurrently(words) {
				return true
			}
		case "REINDEX":
			if _, concurrent := parseReindex(masked); concurrent {
				return true
			}
		}
	}
	return false
}

// ChangesSequence reports whether stmt calls nextval or setval, whose
// effect on a sequence a rollback does not undo. It matches the
// function names as words of code, so a column or literal that merely
// contains the name does not count; a sequence advanced by a column
// default is not visible in the text and is not matched. The statement
// counts if either reading of a plain '...' literal says so.
func ChangesSequence(stmt string) bool {
	for _, reading := range StringReadings {
		words := stmtWords(codeWords(stmt, reading))
		if words.has("NEXTVAL") || words.has("SETVAL") {
			return true
		}
	}
	return false
}

// reindexOptionsConcurrent reports whether a REINDEX option list turns
// CONCURRENTLY on. Each comma-separated option is a name and an
// optional value; the tablespace name in TABLESPACE name is a value,
// not an option, so a tablespace called concurrently does not count.
func reindexOptionsConcurrent(options string) bool {
	for _, opt := range strings.Split(options, ",") {
		words := sqlWords(opt)
		if len(words) == 0 || words[0] != "CONCURRENTLY" {
			continue
		}
		if len(words) == 1 {
			return true
		}
		switch words[1] {
		case "FALSE", "OFF", "0":
		default:
			return true
		}
	}
	return false
}

// clusterHasNoTable reports whether a CLUSTER statement names no table:
// only the optional parenthesised option list and the legacy VERBOSE
// keyword may follow it. It reads the original text rather than masked
// words so that a quoted table name still counts as a table.
func clusterHasNoTable(stmt string) bool {
	rest := StripLeadingComments(stmt)
	rest = StripLeadingComments(rest[len("CLUSTER"):])
	if strings.HasPrefix(rest, "(") {
		depth := 0
		end := -1
		for i := 0; i < len(rest) && end < 0; {
			if j := SkipNonCode(rest, i, StandardStrings); j != i {
				i = j
				continue
			}
			switch rest[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
			i++
		}
		if end < 0 {
			return false
		}
		rest = StripLeadingComments(rest[end+1:])
	}
	i := 0
	for i < len(rest) && IsIdentChar(rest[i]) {
		i++
	}
	if strings.EqualFold(rest[:i], "VERBOSE") {
		rest = StripLeadingComments(rest[i:])
	}
	return strings.Trim(rest, "; \t\r\n") == ""
}

// IsTimeoutOverride reports whether stmt could switch off or change the
// lock_timeout or statement_timeout of the session it runs in: SET,
// SET SESSION or SET LOCAL of either parameter, RESET of either, RESET
// ALL, and any DISCARD (DISCARD ALL resets every parameter). The
// parameter name may be quoted, in any case, and comments may sit
// between the words. Calls such as set_config() are not detected.
func IsTimeoutOverride(stmt string) bool {
	rest := StripLeadingComments(stmt)
	word, rest := nextParamWord(rest)
	switch strings.ToUpper(word) {
	case "DISCARD":
		return true
	case "RESET":
		name, _ := nextParamWord(rest)
		return isTimeoutParam(name) || strings.EqualFold(name, "ALL")
	case "SET":
		name, after := nextParamWord(rest)
		if strings.EqualFold(name, "SESSION") || strings.EqualFold(name, "LOCAL") {
			name, _ = nextParamWord(after)
		}
		return isTimeoutParam(name)
	}
	return false
}

func isTimeoutParam(name string) bool {
	return strings.EqualFold(name, "lock_timeout") ||
		strings.EqualFold(name, "statement_timeout")
}

// nextParamWord reads one bare word or double-quoted identifier from the
// start of s, after any whitespace and comments, and returns it (with
// quotes removed) and the text after it.
func nextParamWord(s string) (string, string) {
	s = StripLeadingComments(s)
	if strings.HasPrefix(s, `"`) {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			if s[i] == '"' {
				if i+1 < len(s) && s[i+1] == '"' {
					b.WriteByte('"')
					i++
					continue
				}
				return b.String(), s[i+1:]
			}
			b.WriteByte(s[i])
		}
		return "", ""
	}
	i := 0
	for i < len(s) && IsIdentChar(s[i]) {
		i++
	}
	return s[:i], s[i:]
}
