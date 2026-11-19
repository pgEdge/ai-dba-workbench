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
