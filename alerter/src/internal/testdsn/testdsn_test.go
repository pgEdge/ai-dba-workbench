/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package testdsn

import "testing"

// TestAllowedTestDatabase pins the allowlist. It matters that plausible
// real database names are refused, because the loopback host check alone
// would happily let a test wipe a production schema reached over an SSH
// tunnel or a local port forward.
func TestAllowedTestDatabase(t *testing.T) {
	allowed := []string{
		"ai_workbench", "ai_workbench_pr407", "ai_workbench_issue407",
		"ai_workbench_sess2", "postgres",
	}
	refused := []string{
		"ai_workbench_prod", "ai_workbench_live", "ai_workbench_staging",
		"ai_workbench_demo", "ai_workbench_", "ai_workbench_PR407",
		"workbench", "postgres_prod", "",
		// Numbered production-shaped names, which the earlier
		// "any tag holding a digit" rule admitted.
		"ai_workbench_prod2", "ai_workbench_live1", "ai_workbench_v2",
		"ai_workbench_demo9",
	}
	for _, name := range allowed {
		if !allowedTestDatabase.MatchString(name) {
			t.Errorf("database %q should be allowed", name)
		}
	}
	for _, name := range refused {
		if allowedTestDatabase.MatchString(name) {
			t.Errorf("database %q should be refused", name)
		}
	}
}
