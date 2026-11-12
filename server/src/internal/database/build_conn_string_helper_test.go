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

import "testing"

// mustBuildConnString calls BuildConnectionString and fails the test on
// an error, for tests whose connection is known to be acceptable.
func mustBuildConnString(t *testing.T, ds *Datastore, conn *MonitoredConnection,
	password, databaseOverride string) string {
	t.Helper()
	dsn, err := ds.BuildConnectionString(conn, password, databaseOverride)
	if err != nil {
		t.Fatalf("BuildConnectionString() error = %v", err)
	}
	return dsn
}
