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

// maxExplainDepth bounds the recursion IsReadOnly performs
// through nested EXPLAIN statements. PostgreSQL rejects EXPLAIN EXPLAIN
// ..., so anything approaching this depth is malformed or hostile and
// the classifier fails closed by calling it a write.
const maxExplainDepth = 8

// IsReadOnly returns true if the SQL statement (after stripping
// leading comments) begins with a read-only keyword: SELECT, WITH, SHOW,
// EXPLAIN, or TABLE. Writable CTEs (WITH ... INSERT/UPDATE/DELETE/MERGE)
// are classified as non-read-only, as are a query with a writing clause
// (see hasWritingSelectClause), a query that calls a side-effecting
// function such as pg_terminate_backend or set_config (see
// CallsSideEffectingFunction, issue #646) and an EXPLAIN that may
// execute a writing inner statement (issue #530).
//
// PostgreSQL lexes a plain '...' literal as a standard string when
// standard_conforming_strings is on and as an escape string, in which a
// backslash escapes the next character, when it is off. The setting can
// be off for the database or the role, and an earlier statement in the
// same request can turn it off, as set_config does from inside a
// SELECT, so the reading in force when sql runs cannot be known here.
// A literal can end in a different place under each reading, and the
// one the classifier does not use can then hide the code that follows:
//
//	WITH x AS (SELECT '\'' AS c), d AS (DELETE FROM t RETURNING *)
//	SELECT * FROM d --'
//
// is one literal running to the final quote under a standard reading,
// which hides the DELETE, whereas under an escape reading the literal
// ends after the escaped quote and the DELETE is code. sql is therefore
// classified under both readings, and split into statements under each
// so that a semicolon only one reading treats as code is respected too;
// it is read-only only when every statement is read-only under both.
func IsReadOnly(sql string) bool {
	for _, reading := range StringReadings {
		pieces := SplitStatements(sql, reading)
		if len(pieces) == 0 {
			pieces = []string{sql}
		}
		for _, piece := range pieces {
			if !isReadOnlyAtDepth(piece, 0, reading) {
				return false
			}
		}
	}
	return true
}

// isReadOnlyAtDepth is IsReadOnly for a single
// statement, under one reading of a plain '...' literal, with the
// EXPLAIN nesting depth reached so far.
func isReadOnlyAtDepth(sql string, depth int, reading StringReading) bool {
	body := strings.TrimSpace(StripLeadingComments(sql))

	// The EXPLAIN test runs against the original text rather than an
	// uppercased copy, because strings.ToUpper is not length-preserving
	// for every input (U+0131 gets shorter), so an offset measured on
	// the copy cannot safely slice the original.
	if HasKeywordPrefix(body, "EXPLAIN") {
		return isReadOnlyExplain(body, depth, reading)
	}

	// Mask the quoted literals, quoted identifiers and comments before
	// uppercasing, so the keyword scans below read code only and a
	// statement such as SELECT * FROM audit WHERE msg LIKE '%signed
	// into%' is not classified as a write. The masking is for
	// classification alone; the text that is executed is untouched.
	upper := strings.ToUpper(MaskNonCode(body, reading))

	if strings.HasPrefix(upper, "WITH") {
		// Writable CTEs can perform data modification, e.g.
		// WITH deleted AS (DELETE FROM t RETURNING *) SELECT * FROM deleted.
		// Check for DML keywords as standalone words in the body.
		dmlKeywords := []string{"INSERT", "UPDATE", "DELETE", "MERGE"}
		for _, kw := range dmlKeywords {
			if ContainsKeyword(upper, kw) {
				return false
			}
		}
		return !hasWritingSelectClause(upper) &&
			!CallsSideEffectingFunction(body, reading)
	}

	if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "TABLE ") {
		return !hasWritingSelectClause(upper) &&
			!CallsSideEffectingFunction(body, reading)
	}

	return strings.HasPrefix(upper, "SHOW")
}

// hasWritingSelectClause reports whether an otherwise reading query
// carries a clause that writes: SELECT ... INTO, which creates a table,
// or a row-locking clause (FOR UPDATE, FOR NO KEY UPDATE, FOR SHARE,
// FOR KEY SHARE), which stamps the lock onto every row it returns.
// PostgreSQL refuses both inside a read-only transaction, so classifying
// them as writes makes this gate agree with the database rather than
// leaving the transaction to reject them later. upperSQL must already
// have had its non-code regions masked (see MaskNonCode) and been
// uppercased. Like the writable-CTE scan, this reads a keyword wherever
// it appears in the remaining code, so it can only over-report, which is
// the safe direction.
func hasWritingSelectClause(upperSQL string) bool {
	return ContainsKeyword(upperSQL, "INTO") ||
		containsLockingClause(upperSQL)
}

// containsLockingClause reports whether upperSQL contains a row-locking
// clause: the word FOR followed by UPDATE or SHARE, with the optional NO
// and KEY qualifiers in between.
func containsLockingClause(upperSQL string) bool {
	words := sqlWords(upperSQL)
	for i, word := range words {
		if word != "FOR" {
			continue
		}
		for j := i + 1; j < len(words); j++ {
			switch words[j] {
			case "NO", "KEY":
				continue
			case "UPDATE", "SHARE":
				return true
			}
			break
		}
	}
	return false
}

// sqlWords splits s into its runs of identifier characters, discarding
// everything else.
func sqlWords(s string) []string {
	var words []string
	i := 0
	for i < len(s) {
		if !IsIdentChar(s[i]) {
			i++
			continue
		}
		start := i
		for i < len(s) && IsIdentChar(s[i]) {
			i++
		}
		words = append(words, s[start:i])
	}
	return words
}

// explainNonExecutingOptions are the EXPLAIN option names that only
// change how the plan is reported. Every other name, ANALYZE included,
// means the inner statement may really run.
var explainNonExecutingOptions = map[string]bool{
	"VERBOSE":      true,
	"COSTS":        true,
	"SETTINGS":     true,
	"GENERIC_PLAN": true,
	"BUFFERS":      true,
	"WAL":          true,
	"TIMING":       true,
	"SUMMARY":      true,
	"MEMORY":       true,
	"SERIALIZE":    true,
	"FORMAT":       true,
}

// isReadOnlyExplain classifies an EXPLAIN statement by what it explains.
// body must already start with the EXPLAIN keyword and have had its
// leading comments stripped. Without ANALYZE, EXPLAIN only plans the
// inner statement and executes nothing, so it is read-only; with ANALYZE
// the inner statement really runs and decides the answer.
//
// In the parenthesised option list, ANALYZE is detected by an
// allow-list that fails closed rather than by searching for the word,
// because an option name there is a ColId: PostgreSQL accepts it as a
// quoted identifier and decodes its Unicode escapes before comparing,
// so EXPLAIN (U&"\0061nalyze") DELETE ... turns ANALYZE on without the
// text ever appearing. Only a bare, unquoted, recognized option name
// counts as non-executing; anything quoted, escaped or unrecognized is
// taken as an execution, and the inner statement then decides the
// classification. The legacy form needs no such care, because its
// option words are keywords that cannot be quoted or escaped.
func isReadOnlyExplain(body string, depth int, reading StringReading) bool {
	executes, rest, ok := parseExplainHead(body, reading)
	if !ok {
		// Malformed option list: fail closed rather than guess at it.
		return false
	}
	if !executes {
		return true
	}
	if rest == "" || depth >= maxExplainDepth {
		return false
	}
	return isReadOnlyAtDepth(rest, depth+1, reading)
}

// parseExplainHead reads the options of an EXPLAIN statement, under one
// reading of a plain '...' literal, and reports whether they make the
// inner statement run, along with the inner statement's text. body must
// already start with the EXPLAIN keyword. ok is false when the option
// list's parentheses do not balance. See isReadOnlyExplain for why the
// two option forms are read as they are.
func parseExplainHead(body string, reading StringReading) (executes bool, rest string, ok bool) {
	rest = strings.TrimSpace(StripLeadingComments(body[len("EXPLAIN"):]))

	if strings.HasPrefix(rest, "(") {
		// Parenthesised form: EXPLAIN ( option [, ...] ) statement.
		options, remainder, balanced := splitExplainOptions(rest, reading)
		if !balanced {
			// Unbalanced parentheses: the statement is malformed.
			return false, "", false
		}
		executes = explainOptionsExecute(options, reading)
		rest = strings.TrimSpace(StripLeadingComments(remainder))
	} else {
		// Legacy form: EXPLAIN [ANALYZE] [VERBOSE] statement, in either
		// order. This grammar is closed: ANALYZE, its British
		// spelling and VERBOSE are keywords rather than a ColId, so a
		// quoted or escaped spelling of one is a syntax error rather
		// than an option, and the first word that is none of them
		// really is the start of the inner statement. A plain EXPLAIN
		// only plans it.
	keywords:
		for {
			word, remainder := nextSQLWord(rest)
			switch strings.ToUpper(word) {
			case "ANALYZE", "ANALYSE": //nolint:misspell // British spelling accepted by PostgreSQL
				executes = true
			case "VERBOSE":
			default:
				break keywords
			}
			rest = strings.TrimSpace(StripLeadingComments(remainder))
		}
	}

	return executes, rest, true
}

// ExplainExecutes reports whether an EXPLAIN statement would run the
// statement it explains, under either reading of a plain '...' literal.
// body must already start with the EXPLAIN keyword and have had its
// leading comments stripped. A malformed option list counts as an
// execution, so a caller that refuses executing EXPLAINs fails closed.
func ExplainExecutes(body string) bool {
	for _, reading := range StringReadings {
		executes, _, ok := parseExplainHead(body, reading)
		if executes || !ok {
			return true
		}
	}
	return false
}

// explainOptionsExecute reports whether a parenthesised EXPLAIN option
// list may cause the inner statement to run. It inspects option names
// only, never their values, and treats a name it cannot read as a bare
// recognized identifier as an execution.
func explainOptionsExecute(options string, reading StringReading) bool {
	entries := splitExplainOptionEntries(options, reading)
	if len(entries) == 0 {
		// EXPLAIN () is not valid SQL, so fail closed.
		return true
	}
	for _, entry := range entries {
		name, _ := nextSQLWord(strings.TrimSpace(StripLeadingComments(entry)))
		if !explainNonExecutingOptions[strings.ToUpper(name)] {
			return true
		}
	}
	return false
}

// splitExplainOptionEntries splits an EXPLAIN option list on the commas
// that separate its entries. Quoted strings, quoted identifiers,
// comments and nested parentheses are skipped so that a comma inside one
// does not start an entry. The result is nil for an empty list.
func splitExplainOptionEntries(options string, reading StringReading) []string {
	var entries []string
	depth := 0
	start := 0
	i := 0

	for i < len(options) {
		if j := SkipNonCode(options, i, reading); j != i {
			i = j
			continue
		}

		ch := options[i]
		switch {
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case ch == ',' && depth == 0:
			entries = append(entries, options[start:i])
			start = i + 1
		}
		i++
	}
	entries = append(entries, options[start:])

	if len(entries) == 1 &&
		strings.TrimSpace(StripLeadingComments(entries[0])) == "" {
		return nil
	}
	return entries
}

// splitExplainOptions splits an EXPLAIN option list from the statement
// that follows it. s must start with the opening parenthesis. It
// returns the option text, the remainder after the matching closing
// parenthesis, and whether a matching parenthesis was found. Quoted
// strings, quoted identifiers and comments are skipped so that a
// parenthesis inside one does not end the list early.
func splitExplainOptions(s string, reading StringReading) (options, remainder string, ok bool) {
	depth := 0
	i := 0
	for i < len(s) {
		if j := SkipNonCode(s, i, reading); j != i {
			i = j
			continue
		}

		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], true
			}
		}
		i++
	}
	return "", "", false
}

// nextSQLWord splits the leading identifier-character run off s,
// returning that word and the rest of the string. The word is empty
// when s does not start with an identifier character.
func nextSQLWord(s string) (word, rest string) {
	i := 0
	for i < len(s) && IsIdentChar(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

// HasKeywordPrefix reports whether sql starts with keyword, ignoring
// case, followed by a non-identifier character, so that EXPLAINABLE does
// not match EXPLAIN. The comparison is made on the caller's own string
// rather than an uppercased copy, so that the keyword's length is a
// valid offset into it.
func HasKeywordPrefix(sql, keyword string) bool {
	if len(sql) < len(keyword) ||
		!strings.EqualFold(sql[:len(keyword)], keyword) {
		return false
	}
	return len(sql) == len(keyword) || !IsIdentChar(sql[len(keyword)])
}

// ContainsKeyword checks whether a SQL keyword appears as a standalone
// word in the given uppercase SQL string. This prevents false positives
// from identifiers that contain a keyword as a substring (e.g.,
// "updated_at" should not match "UPDATE").
func ContainsKeyword(upperSQL, keyword string) bool {
	idx := 0
	for {
		pos := strings.Index(upperSQL[idx:], keyword)
		if pos < 0 {
			return false
		}
		pos += idx
		end := pos + len(keyword)

		startOK := pos == 0 || !IsIdentChar(upperSQL[pos-1])
		endOK := end >= len(upperSQL) || !IsIdentChar(upperSQL[end])

		if startOK && endOK {
			return true
		}
		idx = end
	}
}
