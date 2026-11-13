/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"os"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/testutil/pkgdb"
)

// TestMain runs the package's tests against a database of its own, named
// after the configured test database plus "_pkg_metrics", so that they can
// run alongside other packages' tests; see pkgdb.
func TestMain(m *testing.M) {
	os.Exit(pkgdb.Run(m, "metrics"))
}
