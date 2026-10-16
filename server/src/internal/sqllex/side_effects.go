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

import (
	"strconv"
	"strings"
)

// This file finds calls to functions that change server, session or
// database state from inside a query that otherwise only reads, so that
// the query endpoint (isReadOnlyStatement in package api) and the MCP
// query tools (package tools) can classify such a query as a write
// (issue #646). Most of these functions work inside a read-only
// transaction (pg_terminate_backend, pg_reload_conf, set_config and the
// advisory locks among them), so the read-only transaction both open
// does not stop them; the classification is what puts them behind the
// write-access check, and on the query endpoint the confirmation prompt.
//
// The lists below were built from the PostgreSQL 18 documentation (the
// System Administration Functions chapter, the sequence, large object,
// text search, XML and statistics function tables, and the contrib
// module pages), from the PostgreSQL 9.6 and 14 documentation for the
// names older servers still use, and from the Spock 3.3, 4.0, 4.1, 5.0
// and 6.0 install scripts, the pg_cron README, the pglogical 2
// documentation and the pg_background README. The match errs towards
// calling a query a write: a false positive costs one confirmation
// click, whereas a false negative runs the function without one.
//
// What this cannot see: a user-defined function, view, operator or cast
// that calls one of these functions internally. Those are reached
// through a name that is not on any list, and only the database knows
// what they do.

// sideEffectingFunctionFamilies holds the exact names, lower case, of
// the functions that make a query a write, grouped by the family each
// belongs to. Every family named "core" lists functions shipped with
// current PostgreSQL, which a database test checks against pg_proc.
var sideEffectingFunctionFamilies = []struct {
	family string
	core   bool
	names  []string
}{
	{"server signaling", true, []string{
		"pg_cancel_backend", "pg_terminate_backend", "pg_reload_conf",
		"pg_rotate_logfile", "pg_log_backend_memory_contexts",
	}},
	{"server signaling before PostgreSQL 17", false, []string{
		"pg_rotate_logfile_old",
	}},
	{"configuration settings", true, []string{"set_config"}},
	{"backup and WAL control", true, []string{
		"pg_create_restore_point", "pg_switch_wal", "pg_backup_start",
		"pg_backup_stop",
	}},
	{"backup and WAL control before PostgreSQL 15", false, []string{
		"pg_start_backup", "pg_stop_backup", "pg_switch_xlog",
	}},
	{"recovery control", true, []string{
		"pg_promote", "pg_wal_replay_pause", "pg_wal_replay_resume",
	}},
	{"recovery control before PostgreSQL 10", false, []string{
		"pg_xlog_replay_pause", "pg_xlog_replay_resume",
	}},
	{"snapshot synchronization", true, []string{
		"pg_log_standby_snapshot", "pg_export_snapshot",
	}},
	{"replication slots", true, []string{
		"pg_create_physical_replication_slot",
		"pg_create_logical_replication_slot", "pg_drop_replication_slot",
		"pg_copy_physical_replication_slot",
		"pg_copy_logical_replication_slot", "pg_replication_slot_advance",
		"pg_sync_replication_slots", "pg_logical_emit_message",
	}},
	{"statistics manipulation", true, []string{
		"pg_restore_relation_stats", "pg_clear_relation_stats",
		"pg_restore_attribute_stats", "pg_clear_attribute_stats",
		"pg_stat_force_next_flush",
	}},
	{"index maintenance", true, []string{
		"brin_summarize_new_values", "brin_summarize_range",
		"brin_desummarize_range", "gin_clean_pending_list",
	}},
	{"collation management", true, []string{"pg_import_system_collations"}},
	{"transaction ID assignment", true, []string{
		"pg_current_xact_id", "txid_current",
	}},
	{"notifications", true, []string{"pg_notify"}},
	{"sequences", true, []string{"nextval", "setval"}},
	{"large objects", true, []string{
		"lo_creat", "lo_create", "lo_from_bytea", "lo_import", "lo_export",
		"lo_open", "lo_put", "lo_truncate", "lo_truncate64", "lo_unlink",
		"lowrite",
	}},
	{"functions that run a query given as text", true, []string{
		"query_to_xml", "query_to_xmlschema", "query_to_xml_and_xmlschema",
		"cursor_to_xml", "cursor_to_xmlschema", "ts_stat", "ts_rewrite",
	}},
	{"contrib modules", false, []string{
		"pg_stat_statements_reset",
		"pg_buffercache_evict", "pg_buffercache_evict_relation",
		"pg_buffercache_evict_all",
		"pg_prewarm", "autoprewarm_start_worker", "autoprewarm_dump_now",
		"heap_force_kill", "heap_force_freeze",
		"pg_truncate_visibility_map",
		"postgres_fdw_disconnect", "postgres_fdw_disconnect_all",
		"pg_file_write", "pg_file_sync", "pg_file_rename", "pg_file_unlink",
		"pg_logfile_rotate",
		// pg_cron, for a call that relies on search_path rather than
		// naming the cron schema.
		"schedule", "schedule_in_database", "unschedule", "alter_job",
		// pglogical management, likewise. The show_* status functions
		// and the wait_* functions, which only block, are left out.
		"create_node", "drop_node",
		"alter_node_add_interface", "alter_node_drop_interface",
		"create_subscription", "drop_subscription",
		"alter_subscription_disable", "alter_subscription_enable",
		"alter_subscription_interface",
		"alter_subscription_synchronize",
		"alter_subscription_resynchronize_table",
		"alter_subscription_add_replication_set",
		"alter_subscription_remove_replication_set",
		"create_replication_set", "alter_replication_set",
		"drop_replication_set",
		"replication_set_add_table", "replication_set_add_all_tables",
		"replication_set_remove_table",
		"replication_set_add_sequence", "replication_set_add_all_sequences",
		"replication_set_remove_sequence",
		"synchronize_sequence", "replicate_ddl_command",
	}},
	{"Spock management", false, []string{
		"cleanup_resolutions", "convert_column_to_int8",
		"convert_sequence_to_snowflake",
		"node_add_interface", "node_alter", "node_create", "node_drop",
		"node_drop_interface", "node_info_apply", "node_info_emit",
		"node_refresh_info", "node_refresh_info_one",
		"pause_apply_workers", "resume_apply_workers", "repair_mode",
		"replicate_ddl",
		"repset_add_all_seqs", "repset_add_all_tables",
		"repset_add_partition", "repset_add_seq", "repset_add_table",
		"repset_alter", "repset_create", "repset_drop",
		"repset_remove_partition", "repset_remove_seq",
		"repset_remove_table",
		"reserved_object_add", "reserved_object_remove",
		"reset_channel_stats", "reset_subscription_stats",
		"slot_enable_failover",
		"sub_add_repset", "sub_alter_interface", "sub_alter_options",
		"sub_alter_skiplsn", "sub_alter_sync", "sub_create", "sub_disable",
		"sub_drop", "sub_enable", "sub_remove_repset", "sub_resync_table",
		"sync_event", "sync_seq", "terminate_active_transactions",
		// Spock 3.3 only.
		"set_cluster_readonly", "unset_cluster_readonly",
		"prune_conflict_tracking",
	}},
}

// sideEffectingFunctionPrefixes are the name prefixes of whole families
// of side-effecting functions, so that a member added in a later release
// is caught too. Read-only members of a family, such as
// pg_replication_origin_oid, are caught along with the rest, which is
// the safe direction.
var sideEffectingFunctionPrefixes = []string{
	"pg_advisory_",           // advisory locks (session and transaction)
	"pg_try_advisory_",       // advisory locks
	"pg_replication_origin_", // replication origins
	"pg_logical_slot_",       // logical decoding, which consumes changes
	"pg_stat_reset",          // statistics resets
	"dblink",                 // dblink, which can run SQL anywhere
	"pg_background_",         // pg_background, which runs SQL in a worker
}

// sideEffectingSchemas are the schemas in which every function call is
// taken as a write: those of the replication and job-scheduling
// extensions, whose functions mostly reconfigure the cluster or run SQL
// later. A call qualified with one of these schemas is a write whatever
// the function is called.
var sideEffectingSchemas = map[string]bool{
	"spock":     true,
	"pglogical": true,
	"cron":      true,
}

// sideEffectingFunctions is the union of the family lists, built once.
var sideEffectingFunctions = func() map[string]bool {
	names := make(map[string]bool)
	for _, family := range sideEffectingFunctionFamilies {
		for _, name := range family.names {
			names[name] = true
		}
	}
	return names
}()

// isSideEffectingFunctionName reports whether name, already folded to
// lower case, is a listed function or starts with a listed prefix.
func isSideEffectingFunctionName(name string) bool {
	if sideEffectingFunctions[name] {
		return true
	}
	for _, prefix := range sideEffectingFunctionPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// notFieldSelectable holds the names the lists above match that
// field-selection notation can never call, but that a query reads as a
// column or relation after a dot: pg_cron's cron.job table has a
// schedule column, whilst cron.schedule takes two arguments, and
// pg_replication_origin_status is a catalog view that the
// pg_replication_origin_ prefix matches.
var notFieldSelectable = map[string]bool{
	"schedule":                     true,
	"pg_replication_origin_status": true,
}

// isFieldSelectedFunctionName reports whether name, already folded to
// lower case and written after a dot with no parenthesis following,
// calls a side-effecting function in field-selection notation.
func isFieldSelectedFunctionName(name string) bool {
	return !notFieldSelectable[name] && isSideEffectingFunctionName(name)
}

// callTokenKind is the kind of a token CallsSideEffectingFunction reads.
type callTokenKind int

const (
	// nameToken is an unquoted word or a quoted identifier.
	nameToken callTokenKind = iota
	// openParenToken is "(".
	openParenToken
	// closeParenToken is ")".
	closeParenToken
	// dotToken is ".".
	dotToken
	// commaToken is ",".
	commaToken
	// semicolonToken is ";".
	semicolonToken
	// otherToken is any other operator, punctuation or literal.
	otherToken
)

// callToken is one token of a statement, as far as finding function
// calls needs: a name (folded to lower case, with a quoted identifier's
// quotes and escapes decoded), the punctuation that calls, qualified
// names and WITH lists are built from, and everything else. Whitespace
// and comments produce no token, so they are transparent between a
// name and its parenthesis, and between a dot and the name after it,
// as they are to PostgreSQL.
type callToken struct {
	kind callTokenKind
	text string
	// quoted is set for a quoted identifier, which is never a keyword.
	quoted bool
	// numeric is set for a word that starts with a digit. PostgreSQL
	// before 15 lexes 1pg_reload_conf as a number followed by an
	// identifier, so every suffix of such a word is a candidate name.
	numeric bool
}

// isKeyword reports whether tok is the unquoted word keyword, which is
// given in lower case.
func (tok callToken) isKeyword(keyword string) bool {
	return tok.kind == nameToken && !tok.quoted && tok.text == keyword
}

// CallsSideEffectingFunctionUnderAnyReading reports whether sql calls a
// side-effecting function under either reading of a plain '...'
// literal (see StringReadings). A caller that does not know the
// server's standard_conforming_strings setting uses it, because a call
// that one reading hides inside a literal is code under the other.
func CallsSideEffectingFunctionUnderAnyReading(sql string) bool {
	for _, reading := range StringReadings {
		if CallsSideEffectingFunction(sql, reading) {
			return true
		}
	}
	return false
}

// CallsSideEffectingFunction reports whether the code in sql, read under
// the given reading of a plain '...' literal, calls a side-effecting
// function. A call is either a name on the lists above, or any function
// qualified with a schema in sideEffectingSchemas, followed by an
// opening parenthesis; or a listed name written after a dot with no
// parenthesis, since PostgreSQL reads arg.func as func(arg) for any
// function of one argument (field-selection notation), so that
// (pid).pg_terminate_backend, and alias.pg_reload_conf where alias is a
// scalar FROM item, are calls too. The second form also matches a
// column or table that shares a listed name, such as t.nextval, which
// costs only a confirmation.
//
// Text inside string literals, dollar quotes and comments is never read
// as a name. A name is matched however it is written: in any case, with
// or without a schema, quoted, as a U&"..." identifier with Unicode
// escapes, and with whitespace or comments before its parenthesis or
// after its dot. A name followed by a parenthesis that the grammar
// never reads as a call is skipped (see namesThatAreNotCalls).
//
// It fails closed, reporting a call, when it cannot decode a U&"..."
// identifier or when the statement uses UESCAPE, which changes the
// escape character of such an identifier.
func CallsSideEffectingFunction(sql string, reading StringReading) bool {
	tokens, ok := tokenizeForCalls(sql, reading)
	if !ok {
		return true
	}
	notCalls := namesThatAreNotCalls(tokens)
	for k, tok := range tokens {
		if tok.kind != nameToken {
			continue
		}
		if k+1 < len(tokens) && tokens[k+1].kind == openParenToken {
			if !notCalls[k] && callsWithParenthesis(tokens, k) {
				return true
			}
			continue
		}
		if k >= 1 && tokens[k-1].kind == dotToken &&
			isFieldSelectedFunctionName(tok.text) {
			return true
		}
	}
	return false
}

// callsWithParenthesis reports whether the name at tokens[k], which an
// opening parenthesis follows, calls a side-effecting function: it is
// qualified with a schema in sideEffectingSchemas, it is listed, or it
// is a word starting with a digit with a listed suffix.
func callsWithParenthesis(tokens []callToken, k int) bool {
	tok := tokens[k]
	if k >= 2 && tokens[k-1].kind == dotToken &&
		tokens[k-2].kind == nameToken &&
		sideEffectingSchemas[tokens[k-2].text] {
		return true
	}
	if isSideEffectingFunctionName(tok.text) {
		return true
	}
	if tok.numeric {
		for i := 1; i < len(tok.text); i++ {
			if isSideEffectingFunctionName(tok.text[i:]) {
				return true
			}
		}
	}
	return false
}

// withListState is how far namesThatAreNotCalls has read into a WITH
// list of common table expressions at one parenthesis depth.
type withListState int

const (
	// noWithList: no WITH list is open at this depth.
	noWithList withListState = iota
	// expectCTEName: the next name is a common table expression's,
	// after WITH, WITH RECURSIVE or a comma that separates two of them.
	expectCTEName
	// inCTE: inside one common table expression's definition at this
	// depth, which holds no function call outside its parentheses.
	inCTE
)

// withListOpeners are the unquoted words after which WITH starts a
// list of common table expressions: the EXPLAIN forms. WITH also starts
// one at the start of a statement and after an opening parenthesis or
// a semicolon. Elsewhere it is part of another clause (WITH ORDINALITY,
// WITH TIME ZONE, WITH CHECK OPTION and so on) or a column label
// (SELECT 1 AS with), and opening a list there could hide a call after
// a later comma. AS is deliberately not an opener, because AS WITH may
// be a column label; CREATE TABLE ... AS WITH and the like lose only
// the CTE name skip, which costs a confirmation when that name is a
// listed function's.
var withListOpeners = map[string]bool{
	"explain": true, "analyze": true,
	"analyse": true, //nolint:misspell // PostgreSQL accepts ANALYSE
	"verbose": true,
}

// withListClosers are the unquoted words that start the statement a
// WITH list belongs to, and so end the list.
var withListClosers = map[string]bool{
	"select": true, "values": true, "table": true, "insert": true,
	"update": true, "delete": true, "merge": true,
}

// namesThatAreNotCalls marks the names that an opening parenthesis may
// follow but that PostgreSQL's grammar never reads as a function call,
// so that a listed name used there is not taken for one: a name directly
// after AS, which is an alias with a column list (AS t(a, b)) or a type
// with a modifier (CAST(x AS numeric(10))), and the name of a common
// table expression, which may take a column list (WITH t(a) AS (...)).
//
// A common table expression's name is the name that follows WITH, WITH
// RECURSIVE or a comma in the WITH list, at the WITH's parenthesis
// depth, and only when a definition follows it (see isCTEDefinitionAt);
// otherwise the list ends there and the name is checked like any other.
// The list stays open at that depth only through the tokens a
// definition holds there (names, commas, and the parentheses of a
// column list or a body), so a call after the list ends, such as one in
// an ORDER BY after a parenthesised main query, is never skipped.
func namesThatAreNotCalls(tokens []callToken) []bool {
	notCalls := make([]bool, len(tokens))
	w := withListTracker{lists: []withListState{noWithList}}
	for k, tok := range tokens {
		prev := callToken{kind: semicolonToken}
		if k > 0 {
			prev = tokens[k-1]
		}
		switch tok.kind {
		case nameToken:
			notCalls[k] = w.name(tokens, k, prev) || prev.isKeyword("as")
		case openParenToken:
			w.openParen(prev, k > 0 && notCalls[k-1])
		case closeParenToken:
			w.closeParen()
		case commaToken:
			w.comma()
		case semicolonToken:
			w.semicolon()
		default:
			w.set(noWithList)
		}
	}
	return notCalls
}

// withListTracker follows namesThatAreNotCalls through nested
// parentheses, holding the WITH list state at each depth.
type withListTracker struct {
	lists []withListState
	depth int
}

func (w *withListTracker) state() withListState { return w.lists[w.depth] }

func (w *withListTracker) set(s withListState) { w.lists[w.depth] = s }

// name updates the state for the name token at tokens[k] and reports
// whether the name is a common table expression's.
func (w *withListTracker) name(tokens []callToken, k int, prev callToken) bool {
	tok := tokens[k]
	state := w.state()
	switch {
	case state == expectCTEName && tok.isKeyword("recursive") &&
		prev.isKeyword("with"):
	case state == expectCTEName && isCTEDefinitionAt(tokens, k):
		w.set(inCTE)
		return true
	case state == expectCTEName:
		w.set(noWithList)
	case state == inCTE && !tok.quoted && withListClosers[tok.text]:
		w.set(noWithList)
	case tok.isKeyword("with") && opensWithList(prev):
		w.set(expectCTEName)
	}
	return false
}

// isCTEDefinitionAt reports whether the name at tokens[k] starts a
// common table expression's definition: an optional column list of
// names, then AS, an optional MATERIALIZED or NOT MATERIALIZED, and the
// opening parenthesis of the body. Nothing else may follow a CTE name,
// so a name the grammar reads any other way is never skipped.
func isCTEDefinitionAt(tokens []callToken, k int) bool {
	i := k + 1
	if i < len(tokens) && tokens[i].kind == openParenToken {
		i = skipColumnList(tokens, i+1)
		if i < 0 {
			return false
		}
	}
	if i >= len(tokens) || !tokens[i].isKeyword("as") {
		return false
	}
	i++
	if i < len(tokens) && tokens[i].isKeyword("not") {
		i++
		if i >= len(tokens) || !tokens[i].isKeyword("materialized") {
			return false
		}
	}
	if i < len(tokens) && tokens[i].isKeyword("materialized") {
		i++
	}
	return i < len(tokens) && tokens[i].kind == openParenToken
}

// skipColumnList reads a column list of names (not numbers) and commas from
// tokens[i], just after its opening parenthesis, and returns the index
// after its closing parenthesis, or -1 if anything else is found.
func skipColumnList(tokens []callToken, i int) int {
	for i < len(tokens) && ((tokens[i].kind == nameToken &&
		!tokens[i].numeric) || tokens[i].kind == commaToken) {
		i++
	}
	if i >= len(tokens) || tokens[i].kind != closeParenToken {
		return -1
	}
	return i + 1
}

// opensWithList reports whether a WITH after prev starts a list of
// common table expressions.
func opensWithList(prev callToken) bool {
	switch prev.kind {
	case openParenToken, semicolonToken:
		return true
	case nameToken:
		return !prev.quoted && withListOpeners[prev.text]
	default:
		return false
	}
}

// openParen updates the state for an opening parenthesis; prevNotCall
// is whether the name before it is one namesThatAreNotCalls skipped.
func (w *withListTracker) openParen(prev callToken, prevNotCall bool) {
	// Inside a definition, only a column list after the name or a body
	// after AS or MATERIALIZED opens a parenthesis.
	opensDefinition := prev.isKeyword("as") ||
		prev.isKeyword("materialized") ||
		(prev.kind == nameToken && prevNotCall)
	state := w.state()
	if state == expectCTEName || (state == inCTE && !opensDefinition) {
		w.set(noWithList)
	}
	w.depth++
	if w.depth == len(w.lists) {
		w.lists = append(w.lists, noWithList)
	}
	w.set(noWithList)
}

func (w *withListTracker) closeParen() {
	w.set(noWithList)
	if w.depth > 0 {
		w.depth--
	}
}

// comma moves from one common table expression to the next name, and
// ends anything else.
func (w *withListTracker) comma() {
	if w.state() == inCTE {
		w.set(expectCTEName)
	} else {
		w.set(noWithList)
	}
}

func (w *withListTracker) semicolon() {
	for d := range w.lists {
		w.lists[d] = noWithList
	}
	w.depth = 0
}

// tokenizeForCalls splits sql into the tokens CallsSideEffectingFunction
// reads, using the same lexer model as the rest of the classifier (see
// SkipNonCode). ok is false when the statement must be treated as a
// call without further reading: a U&"..." identifier that cannot be
// decoded, or a UESCAPE clause.
func tokenizeForCalls(sql string, reading StringReading) (tokens []callToken, ok bool) {
	i := 0
	for i < len(sql) {
		ch := sql[i]
		if IsSQLSpace(ch) {
			i++
			continue
		}

		if j := SkipNonCode(sql, i, reading); j != i {
			name, isIdent, decoded := quotedIdentifierAt(sql, i, j)
			switch {
			case isIdent && !decoded:
				return nil, false
			case isIdent:
				tokens = append(tokens, callToken{
					kind: nameToken, text: name, quoted: true,
				})
			case isCommentStart(sql, i):
				// Comments are transparent, as whitespace is.
			default:
				tokens = append(tokens, callToken{kind: otherToken})
			}
			i = j
			continue
		}

		if IsIdentChar(ch) {
			start := i
			for i < len(sql) && (IsIdentChar(sql[i]) || sql[i] == '$') {
				i++
			}
			word := strings.ToLower(sql[start:i])
			if word == "uescape" {
				return nil, false
			}
			tokens = append(tokens, callToken{
				kind:    nameToken,
				text:    word,
				numeric: ch >= '0' && ch <= '9',
			})
			continue
		}

		tokens = append(tokens, callToken{kind: punctuationKind(ch)})
		i++
	}
	return tokens, true
}

// punctuationKind is the token kind of the single character ch, which
// starts no name, literal or comment.
func punctuationKind(ch byte) callTokenKind {
	switch ch {
	case '(':
		return openParenToken
	case ')':
		return closeParenToken
	case '.':
		return dotToken
	case ',':
		return commaToken
	case ';':
		return semicolonToken
	default:
		return otherToken
	}
}

// isCommentStart reports whether a line or block comment starts at s[i].
func isCommentStart(s string, i int) bool {
	return i+1 < len(s) &&
		((s[i] == '-' && s[i+1] == '-') || (s[i] == '/' && s[i+1] == '*'))
}

// quotedIdentifierAt reports whether the non-code region s[i:end] that
// SkipNonCode found is a quoted identifier, "..." or U&"...", and if so
// returns its name folded to lower case. decoded is false when a U&"..."
// identifier holds an escape that cannot be decoded. Folding a quoted
// name, which PostgreSQL keeps as written, can only make a match more
// likely, which is the safe direction.
func quotedIdentifierAt(s string, i, end int) (name string, isIdent, decoded bool) {
	unicode := false
	switch {
	case s[i] == '"':
	case !ContinuesIdentifier(s, i):
		length, kind := literalPrefix(s, i)
		if length == 0 || kind != quotedIdentifier {
			return "", false, false
		}
		unicode = true
		i += length
	default:
		return "", false, false
	}

	body := s[i+1 : end]
	body = strings.TrimSuffix(body, `"`)
	body = strings.ReplaceAll(body, `""`, `"`)
	if unicode {
		var ok bool
		body, ok = decodeUnicodeEscapes(body)
		if !ok {
			return "", true, false
		}
	}
	return strings.ToLower(body), true, true
}

// decodeUnicodeEscapes decodes the escapes of a U&"..." identifier body
// with the default escape character, the backslash: \\ is a backslash,
// \XXXX is a code point given as four hex digits and \+XXXXXX one given
// as six. ok is false for any other use of the backslash, an invalid
// code point, or a surrogate, all of which PostgreSQL rejects or decodes
// in ways not worth modeling here.
func decodeUnicodeEscapes(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		var digits int
		switch {
		case i+1 < len(s) && s[i+1] == '\\':
			b.WriteByte('\\')
			i += 2
			continue
		case i+1 < len(s) && s[i+1] == '+':
			digits = 6
			i += 2
		default:
			digits = 4
			i++
		}
		if i+digits > len(s) {
			return "", false
		}
		code, err := strconv.ParseUint(s[i:i+digits], 16, 32)
		if err != nil || code == 0 || code > 0x10FFFF ||
			(code >= 0xD800 && code <= 0xDFFF) {
			return "", false
		}
		b.WriteRune(rune(code))
		i += digits
	}
	return b.String(), true
}
