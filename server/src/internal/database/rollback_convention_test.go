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
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/rollback"
)

// TestNoDirectRollbacksInModule enforces the issue #381 convention over
// the server module: every pgx rollback goes through pkg/rollback, so
// that it runs on a bounded, non-cancelable context. The module root
// is found relative to this file so the check needs no repository
// layout knowledge and runs in the module's own CI workflow.
func TestNoDirectRollbacksInModule(t *testing.T) {
	offenders, err := rollback.Scan(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("scanning module: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("rollbacks must go through pkg/rollback (rollback.Tx or "+
			"rollback.ToSavepoint); offending sites:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
