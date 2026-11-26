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

import "testing"

// TestRequiresNoTransaction pins the statements PostgreSQL 18 refuses
// inside a transaction block (each was confirmed against a live server).
func TestRequiresNoTransaction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stmt string
		want bool
	}{
		{"vacuum", "VACUUM", true},
		{"bare alter", "ALTER", false},
		{"bare create unique", "CREATE UNIQUE", false},
		{"vacuum full", "VACUUM FULL t", true},
		{"vacuum options lowercase", "vacuum (analyze) t", true},
		{"create index concurrently", "CREATE INDEX CONCURRENTLY i ON t (a)", true},
		{"create unique index concurrently", "CREATE UNIQUE INDEX CONCURRENTLY i ON t (a)", true},
		{"drop index concurrently", "DROP INDEX CONCURRENTLY i", true},
		{"reindex table concurrently", "REINDEX TABLE CONCURRENTLY t", true},
		{"reindex option concurrently", "REINDEX (CONCURRENTLY) INDEX i", true},
		{"reindex database", "REINDEX DATABASE d", true},
		{"reindex schema", "REINDEX SCHEMA public", true},
		{"reindex system", "REINDEX SYSTEM d", true},
		{"reindex verbose option schema", "REINDEX (VERBOSE) SCHEMA s", true},
		{"reindex concurrently option true", "REINDEX (VERBOSE, CONCURRENTLY true) TABLE t", true},
		{"reindex concurrently option on", "REINDEX (CONCURRENTLY ON) INDEX i", true},
		{"reindex concurrently option comment", "REINDEX (CONCURRENTLY /* on */) INDEX i", true},
		{"reindex concurrently quoted name", `REINDEX TABLE CONCURRENTLY "t"`, true},
		{"reindex concurrently quoted on", "REINDEX (CONCURRENTLY 'on') TABLE t", true},
		{"reindex concurrently escape string true", "REINDEX (CONCURRENTLY E'TRUE') TABLE t", true},
		{"reindex concurrently unicode string on", "REINDEX (CONCURRENTLY U&'on') TABLE t", true},
		{"reindex concurrently unicode string on with uescape", "REINDEX (CONCURRENTLY U&'on' UESCAPE '!') TABLE t", true},
		{"reindex concurrently quoted identifier on", `REINDEX (CONCURRENTLY "ON") TABLE t`, true},
		{"reindex concurrently dollar quoted true", "REINDEX (CONCURRENTLY $$true$$) TABLE t", true},
		{"reindex concurrently tagged dollar quote", "REINDEX (CONCURRENTLY $t$on$t$) TABLE t", true},
		{"reindex last quoted on wins", "REINDEX (CONCURRENTLY off, CONCURRENTLY 'on') TABLE t", true},
		{"reindex database lowercase comment", "reindex /* c */ database d", true},
		{"alter system", "ALTER SYSTEM SET work_mem = '4MB'", true},
		{"create database", "CREATE DATABASE d", true},
		{"drop database", "DROP DATABASE d", true},
		{"create tablespace", "CREATE TABLESPACE s LOCATION '/x'", true},
		{"drop tablespace", "DROP TABLESPACE s", true},
		{"alter database set tablespace", "ALTER DATABASE d SET TABLESPACE s", true},
		{"cluster bare", "CLUSTER", true},
		{"cluster bare semicolon", "CLUSTER;", true},
		{"cluster verbose", "CLUSTER VERBOSE", true},
		{"cluster options", "CLUSTER (VERBOSE)", true},
		{"cluster options verbose", "CLUSTER (VERBOSE) /* c */ ", true},
		{"discard all", "DISCARD ALL", true},
		{"create subscription", "CREATE SUBSCRIPTION s CONNECTION 'x' PUBLICATION p", true},
		{"drop subscription", "DROP SUBSCRIPTION s", true},
		{"detach concurrently", "ALTER TABLE t DETACH PARTITION p CONCURRENTLY", true},
		{"leading comments and mixed case", "-- note\n/* x */ Create Index Concurrently i ON t (a)", true},

		{"empty", "", false},
		{"drop index", "DROP INDEX i", false},
		{"drop table", "DROP TABLE t", false},
		{"comment only", "-- nothing", false},
		{"update", "UPDATE t SET a = 1", false},
		{"create index", "CREATE INDEX i ON t (a)", false},
		{"alter table", "ALTER TABLE t ADD COLUMN c int", false},
		{"detach without concurrently", "ALTER TABLE t DETACH PARTITION p", false},
		{"alter database set parameter", "ALTER DATABASE d SET work_mem = '4MB'", false},
		{"reindex table", "REINDEX TABLE t", false},
		{"reindex table named system", "REINDEX TABLE system", false},
		{"reindex index in schema named schema", "REINDEX INDEX schema.i", false},
		{"reindex table named database", "REINDEX TABLE public.database", false},
		{"reindex tablespace named schema", "REINDEX (TABLESPACE schema) TABLE t", false},
		{"reindex tablespace named concurrently", "REINDEX (TABLESPACE concurrently) TABLE t", false},
		{"reindex concurrently option false", "REINDEX (CONCURRENTLY false) TABLE t", false},
		{"reindex concurrently option off", "REINDEX (VERBOSE, CONCURRENTLY OFF) INDEX i", false},
		{"reindex concurrently option zero", "REINDEX (CONCURRENTLY 0) INDEX i", false},
		{"reindex last concurrently off wins", "REINDEX (CONCURRENTLY ON, CONCURRENTLY OFF) TABLE t", false},
		{"reindex last concurrently false wins", "REINDEX (CONCURRENTLY, VERBOSE, CONCURRENTLY false) TABLE t", false},
		{"reindex concurrently invalid value", "REINDEX (CONCURRENTLY maybe) TABLE t", false},
		{"reindex concurrently quoted junk", "REINDEX (CONCURRENTLY 'maybe') TABLE t", false},
		{"reindex concurrently quoted one", "REINDEX (CONCURRENTLY '1') TABLE t", false},
		// An escape is not decoded, so E'\157n' ('on') reads as off and
		// runs in a transaction block, where PostgreSQL refuses it.
		{"reindex concurrently escaped on", `REINDEX (CONCURRENTLY E'\157n') TABLE t`, false},
		{"reindex concurrently empty quoted value", "REINDEX (CONCURRENTLY '') TABLE t", false},
		{"reindex concurrently unbalanced dollar tag", "REINDEX (CONCURRENTLY $a$on$b$) TABLE t", false},
		{"reindex concurrently lone dollar", "REINDEX (CONCURRENTLY $", false},
		{"reindex concurrently quoted tablespace on", `REINDEX (TABLESPACE "on") TABLE t`, false},
		{"reindex concurrently quoted off", "REINDEX (CONCURRENTLY 'off') TABLE t", false},
		{"reindex quoted tablespace", `REINDEX (TABLESPACE "concurrently") TABLE t`, false},
		{"reindex quoted schema table", `REINDEX TABLE "schema"."system"`, false},
		{"reindex unbalanced options", "REINDEX (CONCURRENTLY TABLE t", false},
		{"reindex alone", "REINDEX", false},
		{"cluster table", "CLUSTER t USING i", false},
		{"cluster quoted table", `CLUSTER "t"`, false},
		{"cluster option table", "CLUSTER (VERBOSE) t", false},
		{"cluster verbose table", "CLUSTER VERBOSE t", false},
		{"cluster unbalanced options", "CLUSTER (VERBOSE", false},
		{"discard plans", "DISCARD PLANS", false},
		{"alter type add value", "ALTER TYPE e ADD VALUE 'b'", false},
		{"alter type rename", "ALTER TYPE e RENAME TO f", false},
		{"keyword inside literal", "SELECT 'VACUUM'", false},
		{"keyword inside comment", "SELECT 1 -- CREATE DATABASE", false},
		{"create table named concurrently", "CREATE TABLE concurrently (a int)", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RequiresNoTransaction(tt.stmt); got != tt.want {
				t.Errorf("RequiresNoTransaction(%q) = %v, want %v",
					tt.stmt, got, tt.want)
			}
		})
	}
}

func TestIsTransactionControl(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stmt string
		want bool
	}{
		{"begin", "BEGIN", true},
		{"begin options", "BEGIN ISOLATION LEVEL SERIALIZABLE", true},
		{"start transaction", "START TRANSACTION", true},
		{"start transaction lowercase", "start  transaction read only", true},
		{"commit", "COMMIT", true},
		{"commit prepared", "COMMIT PREPARED 'x'", true},
		{"end", "END", true},
		{"rollback", "ROLLBACK", true},
		{"rollback to", "ROLLBACK TO SAVEPOINT s", true},
		{"rollback prepared", "ROLLBACK PREPARED 'x'", true},
		{"abort", "ABORT", true},
		{"savepoint", "SAVEPOINT s", true},
		{"release", "RELEASE SAVEPOINT s", true},
		{"prepare transaction", "PREPARE TRANSACTION 'x'", true},
		{"leading comment", "/* c */ -- d\nCommit", true},

		{"empty", "", false},
		{"select", "SELECT 1", false},
		{"update", "UPDATE t SET a = 1", false},
		{"prepare statement", "PREPARE p AS SELECT 1", false},
		{"start alone", "START", false},
		{"set transaction", "SET TRANSACTION READ ONLY", false},
		{"keyword in literal", "SELECT 'COMMIT'", false},
		{"begin as identifier prefix", "BEGINNING", false},
		{"parenthesised", "(SELECT 1)", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsTransactionControl(tt.stmt); got != tt.want {
				t.Errorf("IsTransactionControl(%q) = %v, want %v",
					tt.stmt, got, tt.want)
			}
		})
	}
}

func TestIsTimeoutOverride(t *testing.T) {
	t.Parallel()
	tests := []struct {
		stmt string
		want bool
	}{
		{"SET lock_timeout = 0", true},
		{"set statement_timeout TO 0", true},
		{"SET SESSION statement_timeout = 0", true},
		{"SET LOCAL lock_timeout = '1h'", true},
		{"SET \"Lock_Timeout\" = 0", true},
		{"/* c */ SET -- x\n SESSION /* y */ \"statement_timeout\" = 0", true},
		{"RESET lock_timeout", true},
		{"reset \"statement_timeout\"", true},
		{"RESET ALL", true},
		{"DISCARD ALL", true},
		{"DISCARD PLANS", true},
		{"discard temp", true},
		{"SET \"a\"\"b\" = 1", false},
		{"SET work_mem = '4MB'", false},
		{"SET lock_timeout_other = 1", false},
		{"RESET work_mem", false},
		{"SET search_path = public", false},
		{"SET \"unterminated", false},
		{"SET SESSION", false},
		{"UPDATE t SET lock_timeout = 1", false},
		{"SELECT 1", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsTimeoutOverride(tc.stmt); got != tc.want {
			t.Errorf("IsTimeoutOverride(%q) = %v, want %v", tc.stmt, got, tc.want)
		}
	}
}

func TestBuildsIndexConcurrently(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stmt string
		want bool
	}{
		{"create index concurrently", "CREATE INDEX CONCURRENTLY i ON t (a)", true},
		{"create unique index concurrently", "create unique index concurrently i on t (a)", true},
		{"leading comment", "/* c */ CREATE INDEX CONCURRENTLY i ON t (a)", true},
		{"reindex concurrently", "REINDEX INDEX CONCURRENTLY i", true},
		{"reindex option", "REINDEX (CONCURRENTLY) TABLE t", true},
		{"reindex option on", "REINDEX (VERBOSE, CONCURRENTLY ON) TABLE t", true},
		{"reindex option true", "REINDEX (CONCURRENTLY TRUE) TABLE t", true},
		{"reindex option one", "REINDEX (CONCURRENTLY 1) TABLE t", true},
		{"reindex last option on wins", "REINDEX (CONCURRENTLY OFF, CONCURRENTLY ON) TABLE t", true},
		{"reindex quoted option on", "REINDEX (CONCURRENTLY 'on') TABLE t", true},
		// An escaped value may be on, so a failure keeps its warning,
		// even for a kind that runs outside a transaction anyway.
		{"reindex escaped option", `REINDEX (CONCURRENTLY E'\157n') SCHEMA public`, true},
		{"reindex unicode-escaped option", `REINDEX (CONCURRENTLY U&'o\006E') TABLE t`, true},
		{"reindex uescape option", `REINDEX (CONCURRENTLY U&'o!006E' UESCAPE '!') SCHEMA public`, true},
		{"reindex uescape option with comments", "REINDEX (CONCURRENTLY U&'o!006E' /* c */ uescape -- d\n '!', VERBOSE) SCHEMA public", true},
		{"reindex uescape escape-string option", `REINDEX (CONCURRENTLY U&'o!006E' UESCAPE E'!') SCHEMA public`, true},
		{"reindex uescape lower-case escape-string option", `REINDEX (CONCURRENTLY U&'o!006E' uescape e'!', VERBOSE) TABLE t`, true},
		{"reindex uescape dollar-quoted option", "REINDEX (CONCURRENTLY U&'o!006E' UESCAPE $$!$$) SCHEMA public", true},
		{"reindex uescape tagged dollar-quoted option", "REINDEX (CONCURRENTLY U&'o!006E' UESCAPE $t$!$t$, VERBOSE) TABLE t", true},
		{"reindex uescape identifier option", `REINDEX (CONCURRENTLY U&"o!006E" UESCAPE '!') SCHEMA public`, true},
		{"reindex continued literal option", "REINDEX (CONCURRENTLY 'o'\n'n') SCHEMA public", true},
		{"reindex doubled quote option", "REINDEX (CONCURRENTLY 'o''n') SCHEMA public", true},

		{"create index", "CREATE INDEX i ON t (a)", false},
		{"create unique index", "CREATE UNIQUE INDEX i ON t (a)", false},
		{"create table", "CREATE TABLE concurrently (a int)", false},
		{"reindex", "REINDEX TABLE t", false},
		{"reindex database", "REINDEX DATABASE d", false},
		{"reindex option off", "REINDEX (CONCURRENTLY OFF) TABLE t", false},
		{"reindex quoted option off", "REINDEX (CONCURRENTLY 'off') TABLE t", false},
		{"reindex last option off wins", "REINDEX (CONCURRENTLY ON, CONCURRENTLY OFF) TABLE t", false},
		{"reindex last option off wins after uescape", "REINDEX (CONCURRENTLY U&'o!006E' UESCAPE '!', CONCURRENTLY OFF) TABLE t", false},
		{"reindex uescape without a literal", "REINDEX (CONCURRENTLY U&'on' UESCAPE x) TABLE t", false},
		{"reindex uescape before a comma", "REINDEX (CONCURRENTLY U&'on' UESCAPE, VERBOSE) TABLE t", false},
		{"reindex uescape with a bare dollar", "REINDEX (CONCURRENTLY U&'on' UESCAPE $1) TABLE t", false},
		{"reindex uescape with a bare E", "REINDEX (CONCURRENTLY U&'on' UESCAPE E) TABLE t", false},
		{"reindex uescape at the end", "REINDEX (CONCURRENTLY U&'on' UESCAPE", false},
		{"reindex word starting uescape", "REINDEX (CONCURRENTLY U&'on' UESCAPED '!') TABLE t", false},
		{"reindex unbalanced", "REINDEX (CONCURRENTLY TABLE t", false},
		{"bare reindex", "REINDEX", false},
		{"drop index concurrently", "DROP INDEX CONCURRENTLY i", false},
		{"quoted", `CREATE INDEX "CONCURRENTLY" ON t (a)`, false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := BuildsIndexConcurrently(tt.stmt); got != tt.want {
				t.Errorf("BuildsIndexConcurrently(%q) = %v, want %v", tt.stmt, got, tt.want)
			}
		})
	}
}

func TestChangesSequence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stmt string
		want bool
	}{
		{"nextval", "SELECT nextval('s')", true},
		{"setval", "SELECT SetVal('s', 10)", true},
		{"in insert", "INSERT INTO t VALUES (nextval('s'))", true},
		{"spaced call", "SELECT nextval ('s')", true},
		{"either literal reading", `SELECT 'a\'' , nextval('s')`, true},
		{"quoted lower-case name", `INSERT INTO t VALUES (pg_catalog."nextval"('s'))`, true},
		{"schema-qualified", "SELECT pg_catalog.setval('s', 1)", true},
		{"comment before parenthesis", "SELECT nextval /* c */ ('s')", true},
		{"undecodable unicode identifier", `SELECT U&"\zzzz"`, true},

		{"plain select", "SELECT 1", false},
		{"in literal", "SELECT 'nextval'", false},
		{"in comment", "SELECT 1 -- setval", false},
		{"identifier containing it", "SELECT nextval_count FROM t", false},
		{"quoted identifier", `SELECT "nextval" FROM t`, false},
		{"bare column", "SELECT nextval FROM t", false},
		{"qualified column", "UPDATE t SET a = t.setval", false},
		{"alias with column list", "SELECT * FROM f() AS nextval(a)", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ChangesSequence(tt.stmt); got != tt.want {
				t.Errorf("ChangesSequence(%q) = %v, want %v", tt.stmt, got, tt.want)
			}
		})
	}
}

// TestQuotedTurnsOn covers quoted forms the statement tests cannot
// reach, since the lexer never hands maskKeepingValues them whole.
func TestQuotedValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		q    string
		want optionValue
	}{
		{"'on'", valueOn},
		{"", valueOff},
		{`u&"True"`, valueOn},
		{"e'on'", valueOn},
		{"$x$on$x$", valueOn},
		{"'off'", valueOff},
		{`E'\157n'`, valueUndecided},   // an escape may spell on
		{`'\157n'`, valueUndecided},    // so may a plain literal read with escapes
		{`U&'o!006E'`, valueUndecided}, // UESCAPE may name any character
		{"'o''n'", valueUndecided},     // a doubled quote is not decoded
		{"'o'\n'n'", valueUndecided},   // nor is a continued literal
		{`"o""n"`, valueOff},           // an identifier is never continued
		{`"o'n"`, valueOff},            // nor is a quote in one a doubled quote
		{"$$on", valueOff},             // unterminated dollar quote
		{"$on", valueOff},              // no closing tag delimiter
		{"$x$", valueOff},              // shorter than its two tags
		{"$x$on$y$", valueOff},         // closing tag differs
		{"B'1'", valueOff},             // a bit string is not a boolean
		{"'on", valueOff},              // unterminated literal
		{"'on\"", valueOff},            // mismatched quotes
	}
	for _, tt := range tests {
		if got := quotedValue(tt.q); got != tt.want {
			t.Errorf("quotedValue(%q) = %v, want %v", tt.q, got, tt.want)
		}
	}
}
