/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package tools

import (
	"os"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/testutil/pkgdb"
)

// TestMain runs the package's tests against a database of its own, named
// after the configured test database plus "_pkg_tools", so that they can
// run alongside other packages' tests; see pkgdb.
func TestMain(m *testing.M) {
	// Production holds a failed connect to a monitored server for the
	// full connect timeout (issue #850); the tests here that fail a
	// connect on purpose skip the hold, and those that check it install
	// a fake clock.
	database.SetConnectTiming(database.ConnectTiming{})
	os.Exit(pkgdb.Run(m, "tools"))
}
