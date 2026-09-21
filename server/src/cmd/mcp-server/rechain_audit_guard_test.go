/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// interleavedAuditStore builds a data directory whose audit log holds
// keyed rows with an unkeyed version 1 row appended above them, which
// is the shape an attacker with write access to auth.db produces and
// no upgrade ever does. It returns the directory, with the store
// closed.
func interleavedAuditStore(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create the auth store: %v", err)
	}
	if err := store.CreateUser("alice", "correct horse battery staple",
		"", "", ""); err != nil {
		t.Fatalf("Failed to create a user: %v", err)
	}
	if err := auth.SeedUnkeyedAuditLogForTesting(store, 1,
		time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC), 0); err != nil {
		t.Fatalf("Failed to write the unkeyed row: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close the store: %v", err)
	}

	return dir
}

// auditHashVersions reads every row's hash_version in id order through
// a connection of its own, because a store open on this database is
// refused.
func auditHashVersions(t *testing.T, dir string) []int {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(dir, "auth.db")+
		"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("Failed to open auth.db directly: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(
		"SELECT hash_version FROM audit_events ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to read the hash versions: %v", err)
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("Failed to scan a hash version: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Failed to walk the hash versions: %v", err)
	}

	return versions
}

// TestRechainAuditLogCommandRefusesAnInterleavedLog is the command-path
// half of VULN-301. The operator meets this having been refused a
// start-up, which is exactly the moment they are most likely to reach
// for the re-chain, so the command must refuse rather than sign the
// rows under the server secret.
func TestRechainAuditLogCommandRefusesAnInterleavedLog(t *testing.T) {
	dir := interleavedAuditStore(t)
	before := auditHashVersions(t, dir)

	var out bytes.Buffer
	err := rechainAuditLogCommand(dir, true, strings.NewReader(""), &out)
	if !errors.Is(err, auth.ErrAuditUnkeyedRow) {
		t.Fatalf("Expected the command to refuse the interleaved log, got %v",
			err)
	}
	if !strings.Contains(err.Error(), "restore auth.db from a known-good "+
		"copy") {
		t.Errorf("Expected the refusal to name the remedy, got %v", err)
	}

	// The plan is never shown, so the operator is never asked to
	// approve figures for a log that will not be re-chained.
	if out.Len() != 0 {
		t.Errorf("Expected no output before the refusal, got:\n%s",
			out.String())
	}

	after := auditHashVersions(t, dir)
	if len(before) != len(after) {
		t.Fatalf("Expected %d rows to survive, got %d", len(before),
			len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("Row %d changed hash version from %d to %d", i+1,
				before[i], after[i])
		}
	}
	if after[len(after)-1] != 1 {
		t.Errorf("Expected the appended row to still be unkeyed, got "+
			"version %d", after[len(after)-1])
	}
}
