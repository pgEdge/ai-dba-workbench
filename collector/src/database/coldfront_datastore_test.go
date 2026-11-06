/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"strings"
	"testing"
)

// TestNewDatastore_ColdFrontRefusedWithoutExtension checks that the
// storage check runs before any migration: with ColdFront enabled on a
// server without the coldfront extension, NewDatastore fails naming the
// missing extension and leaves the schema untouched.
func TestNewDatastore_ColdFrontRefusedWithoutExtension(t *testing.T) {
	if testDBName == "" {
		t.Skip("test database not provisioned")
	}
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()
	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	cfg := parseTestServerURL(t)
	cfg.database = testDBName
	cfg.useColdFront = true
	ds, err := NewDatastore(cfg)
	if err == nil {
		ds.Close()
		t.Fatal("NewDatastore succeeded without the coldfront extension")
	}
	if !strings.Contains(err.Error(), "coldfront extension is not installed") {
		t.Errorf("NewDatastore error = %v", err)
	}

	var schemaVersionExists bool
	if err := conn.QueryRow(context.Background(),
		"SELECT to_regclass('public.schema_version') IS NOT NULL").Scan(&schemaVersionExists); err != nil {
		t.Fatal(err)
	}
	if schemaVersionExists {
		t.Error("a migration ran before the ColdFront check refused the datastore")
	}
}
