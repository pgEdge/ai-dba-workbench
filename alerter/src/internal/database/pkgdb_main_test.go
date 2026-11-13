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
	"os"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/testdsn"
)

// TestMain runs the package's tests against a database of its own, named
// after the configured test database plus "_pkg_database", so that they can
// run alongside other packages' tests; see testdsn.RunPackage.
func TestMain(m *testing.M) {
	os.Exit(testdsn.RunPackage(m, "database"))
}
