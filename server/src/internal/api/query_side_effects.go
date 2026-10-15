/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"strconv"
	"strings"
)

// This file finds calls to functions that change server, session or
// database state from inside a query that otherwise only reads, so that
// isReadOnlyStatement can classify such a query as a write (issue
// #646). Most of these functions work inside a read-only transaction
// (pg_terminate_backend, pg_reload_conf, set_config and the advisory
// locks among them), so the read-only transaction the query endpoint
// opens does not stop them; the classification is what puts them behind
// the confirmation prompt and the write-access check.
//
// The lists below were built from the PostgreSQL 18 documentation (the
// System Administration Functions chapter, the sequence, large object,
// text search, XML and statistics function tables, and the contrib
// module pages), from the PostgreSQL 9.6 and 14 documentation for the
// names older servers still use, and from the Spock 4.1, 5.0 and 6.0
// install scripts and the pg_cron README. The match errs towards calling
// a query a write: a false positive costs one confirmation click,
// whereas a false negative runs the function without one.
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
	{"snapshot synchronization", true, []string{"pg_log_standby_snapshot"}},
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
	}},
	{"Spock management", false, []string{
		"cleanup_resolutions", "convert_column_to_int",
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

// callTokenKind is the kind of a token callsSideEffectingFunction reads.
type callTokenKind int

const (
	// nameToken is an unquoted word or a quoted identifier.
	nameToken callTokenKind = iota
	// openParenToken is "(".
	openParenToken
	// dotToken is ".".
	dotToken
	// otherToken is any other operator, punctuation or literal.
	otherToken
)

// callToken is one token of a statement, as far as finding function
// calls needs: a name (folded to lower case, with a quoted identifier's
// quotes and escapes decoded), the two punctuation marks a call is built
// from, and everything else. Whitespace and comments produce no token,
// so they are transparent between a name and its parenthesis, as they
// are to PostgreSQL.
type callToken struct {
	kind callTokenKind
	text string
	// numeric is set for a word that starts with a digit. PostgreSQL
	// before 15 lexes 1pg_reload_conf as a number followed by an
	// identifier, so every suffix of such a word is a candidate name.
	numeric bool
}

// callsSideEffectingFunction reports whether the code in sql, read under
// the given reading of a plain '...' literal, calls a side-effecting
// function: a name on the lists above, or any function qualified with a
// schema in sideEffectingSchemas, followed by an opening parenthesis.
// Text inside string literals, dollar quotes and comments is never read
// as a name. A name is matched however it is written: in any case, with
// or without a schema, quoted, as a U&"..." identifier with Unicode
// escapes, and with whitespace or comments before its parenthesis.
//
// It fails closed, reporting a call, when it cannot decode a U&"..."
// identifier or when the statement uses UESCAPE, which changes the
// escape character of such an identifier.
func callsSideEffectingFunction(sql string, reading stringReading) bool {
	tokens, ok := tokenizeForCalls(sql, reading)
	if !ok {
		return true
	}
	for k, tok := range tokens {
		if tok.kind != nameToken || k+1 >= len(tokens) ||
			tokens[k+1].kind != openParenToken {
			continue
		}
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
	}
	return false
}

// tokenizeForCalls splits sql into the tokens callsSideEffectingFunction
// reads, using the same lexer model as the rest of the classifier (see
// skipNonCode). ok is false when the statement must be treated as a
// call without further reading: a U&"..." identifier that cannot be
// decoded, or a UESCAPE clause.
func tokenizeForCalls(sql string, reading stringReading) (tokens []callToken, ok bool) {
	i := 0
	for i < len(sql) {
		ch := sql[i]
		if isSQLSpace(ch) {
			i++
			continue
		}

		if j := skipNonCode(sql, i, reading); j != i {
			name, isIdent, decoded := quotedIdentifierAt(sql, i, j)
			switch {
			case isIdent && !decoded:
				return nil, false
			case isIdent:
				tokens = append(tokens, callToken{kind: nameToken, text: name})
			case isCommentStart(sql, i):
				// Comments are transparent, as whitespace is.
			default:
				tokens = append(tokens, callToken{kind: otherToken})
			}
			i = j
			continue
		}

		if isIdentChar(ch) {
			start := i
			for i < len(sql) && (isIdentChar(sql[i]) || sql[i] == '$') {
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

		switch ch {
		case '(':
			tokens = append(tokens, callToken{kind: openParenToken})
		case '.':
			tokens = append(tokens, callToken{kind: dotToken})
		default:
			tokens = append(tokens, callToken{kind: otherToken})
		}
		i++
	}
	return tokens, true
}

// isCommentStart reports whether a line or block comment starts at s[i].
func isCommentStart(s string, i int) bool {
	return i+1 < len(s) &&
		((s[i] == '-' && s[i+1] == '-') || (s[i] == '/' && s[i+1] == '*'))
}

// quotedIdentifierAt reports whether the non-code region s[i:end] that
// skipNonCode found is a quoted identifier, "..." or U&"...", and if so
// returns its name folded to lower case. decoded is false when a U&"..."
// identifier holds an escape that cannot be decoded. Folding a quoted
// name, which PostgreSQL keeps as written, can only make a match more
// likely, which is the safe direction.
func quotedIdentifierAt(s string, i, end int) (name string, isIdent, decoded bool) {
	unicode := false
	switch {
	case s[i] == '"':
	case !continuesIdentifier(s, i):
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
