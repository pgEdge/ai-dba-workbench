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
		return reindexRequiresNoTransaction(strings.ToUpper(maskKeepingValues(stmt, reading, false)))
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

// maskKeepingValues masks s as MaskNonCode does, except that a quoted
// literal or identifier leaves a word in its place rather than only
// spaces, so that an option given a quoted value, such as
// CONCURRENTLY 'off', still reads as an option with a value. The word
// is 1 when the quoted text is TRUE or ON in any case, which PostgreSQL
// accepts as a boolean option value however it is quoted ('on', E'on',
// "ON", $$true$$), and X when it is anything else. A value spelled with
// an escape, which is not decoded, may still be on, and reads as on
// when undecidedOn is set and as off when not: see quotedValue. A
// comment is masked to spaces, as it is whitespace to PostgreSQL. The
// result has the same length as s.
func maskKeepingValues(s string, reading StringReading, undecidedOn bool) string {
	masked := []byte(MaskNonCode(s, reading))
	for i := 0; i < len(s); {
		j := SkipNonCode(s, i, reading)
		if j == i {
			i++
			continue
		}
		if !isCommentStart(s, i) && j-i >= 2 {
			masked[i+1] = 'X'
			if v := quotedValue(s[i:j]); v == valueOn || (v == valueUndecided && undecidedOn) {
				masked[i+1] = '1'
			}
			// A UESCAPE clause belongs to the U& value before it, so it
			// is masked with it rather than read as another word.
			if end := uescapeEnd(s, i, j, reading); end > j {
				for k := j; k < end; k++ {
					masked[k] = ' '
				}
				j = end
			}
		}
		i = j
	}
	return string(masked)
}

// uescapeEnd returns the index just past the UESCAPE clause that
// follows the U&'...' or U&"..." value s[start:end], or end when the
// value has no U& prefix or no complete clause follows it. White space
// and comments may sit either side of the UESCAPE keyword, and the
// escape character may be given as an E'...' or dollar-quoted literal,
// which PostgreSQL accepts as it does a plain one.
func uescapeEnd(s string, start, end int, reading StringReading) int {
	if end-start < 3 || (s[start] != 'U' && s[start] != 'u') || s[start+1] != '&' {
		return end
	}
	const kw = "UESCAPE"
	k := skipSpaceAndComments(s, end)
	if len(s)-k < len(kw) || !strings.EqualFold(s[k:k+len(kw)], kw) ||
		(k+len(kw) < len(s) && IsIdentChar(s[k+len(kw)])) {
		return end
	}
	k = skipSpaceAndComments(s, k+len(kw))
	q := k
	if q < len(s) && (s[q] == 'E' || s[q] == 'e') {
		q++
	}
	if (q >= len(s) || s[q] != '\'') && ScanDollarTag(s, k) == "" {
		return end
	}
	return SkipNonCode(s, k, reading)
}

// optionValue is how a quoted boolean option value reads.
type optionValue int

const (
	valueOff optionValue = iota
	valueOn
	// valueUndecided is a value that may decode to on through an escape
	// that is not decoded here.
	valueUndecided
)

// quotedValue reads the quoted literal or identifier q as a boolean
// option value: on when its text is TRUE or ON in any case. It strips
// the E and U& prefixes, the quotes, and a dollar quote's tags, but does
// not decode escapes, so any other text holding a backslash (E'\157n'
// is 'on', as is a plain literal read with backslash escapes), any
// other U& text (whose UESCAPE clause may name any escape character),
// and a single-quoted literal holding a quote (a doubled quote, or a
// literal continued on another line, which PostgreSQL joins to the one
// before it: 'o' <newline> 'n' is 'on') is undecided rather than off. Deciding where a statement runs reads
// an undecided value as off, so it runs in a transaction block, where
// PostgreSQL refuses a concurrent rebuild and nothing is applied;
// deciding whether a failure may have left an invalid index reads it
// as on, so the warning is not lost.
func quotedValue(q string) optionValue {
	stripped := stripStringPrefix(q)
	body, ok := quotedBody(stripped)
	switch {
	case !ok:
		return valueOff
	case strings.EqualFold(body, "on") || strings.EqualFold(body, "true"):
		return valueOn
	case strings.ContainsRune(body, '\\') || len(q)-len(stripped) == 2:
		return valueUndecided
	case stripped[0] == '\'' && strings.ContainsRune(body, '\''):
		return valueUndecided
	}
	return valueOff
}

// stripStringPrefix removes a leading U& or E string prefix from q.
func stripStringPrefix(q string) string {
	switch {
	case len(q) > 2 && (q[0] == 'U' || q[0] == 'u') && q[1] == '&':
		return q[2:]
	case len(q) > 1 && (q[0] == 'E' || q[0] == 'e'):
		return q[1:]
	}
	return q
}

// quotedBody returns the text between the quotes of q, a single-quoted,
// double-quoted or dollar-quoted string with no prefix; ok is false when
// q is not one, or its closing quote does not match its opening one.
func quotedBody(q string) (body string, ok bool) {
	if q == "" {
		return "", false
	}
	if q[0] == '$' {
		return dollarQuotedBody(q)
	}
	if len(q) >= 2 && (q[0] == '\'' || q[0] == '"') && q[len(q)-1] == q[0] {
		return q[1 : len(q)-1], true
	}
	return "", false
}

// dollarQuotedBody returns the text between the tags of the
// dollar-quoted string q; ok is false when the closing tag is missing or
// differs from the opening one.
func dollarQuotedBody(q string) (body string, ok bool) {
	end := strings.IndexByte(q[1:], '$')
	if end < 0 {
		return "", false
	}
	tag := q[:end+2]
	if len(q) < 2*len(tag) || !strings.HasSuffix(q, tag) {
		return "", false
	}
	return q[len(tag) : len(q)-len(tag)], true
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
			if _, concurrent := parseReindex(strings.ToUpper(maskKeepingValues(stmt, reading, true))); concurrent {
				return true
			}
		}
	}
	return false
}

// ChangesSequence reports whether stmt calls nextval or setval, whose
// effect on a sequence a rollback does not undo. It matches the names
// as calls, a name followed by an opening parenthesis, read with the
// same tokens as CallsSideEffectingFunction: in any case, with or
// without a schema, quoted ("nextval", folded as a match is safe in
// that direction) and with whitespace or comments before the
// parenthesis. A column, literal or comment that merely holds the name
// does not count, nor does a name the grammar never reads as a call
// (see namesThatAreNotCalls); a sequence advanced by a column default is
// not visible in the text and is not matched. A statement that cannot
// be tokenised counts, since the note is the cautious answer. The
// statement counts if either reading of a plain '...' literal says so.
func ChangesSequence(stmt string) bool {
	for _, reading := range StringReadings {
		tokens, ok := tokenizeForCalls(stmt, reading)
		if !ok {
			return true
		}
		notCalls := namesThatAreNotCalls(tokens)
		for k, tok := range tokens {
			if tok.kind != nameToken || notCalls[k] ||
				(tok.text != "nextval" && tok.text != "setval") {
				continue
			}
			if k+1 < len(tokens) && tokens[k+1].kind == openParenToken {
				return true
			}
		}
	}
	return false
}

// reindexOptionsConcurrent reports whether a REINDEX option list turns
// CONCURRENTLY on. Each comma-separated option is a name and an
// optional value; the tablespace name in TABLESPACE name is a value,
// not an option, so a tablespace called concurrently does not count.
// PostgreSQL applies the options in order, so the last CONCURRENTLY
// decides. It takes no value or TRUE, ON or 1 to turn it on (a quoted
// TRUE or ON, which maskKeepingValues leaves as 1, too) and FALSE, OFF
// or 0 to turn it off, and refuses any other value. Any other value
// reads as off, so the statement runs in a transaction block, where
// PostgreSQL refuses it and nothing in the batch is applied.
func reindexOptionsConcurrent(options string) bool {
	concurrent := false
	for _, opt := range strings.Split(options, ",") {
		words := sqlWords(opt)
		if len(words) == 0 || words[0] != "CONCURRENTLY" {
			continue
		}
		concurrent = len(words) == 1
		if len(words) == 2 {
			switch words[1] {
			case "TRUE", "ON", "1":
				concurrent = true
			}
		}
	}
	return concurrent
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
