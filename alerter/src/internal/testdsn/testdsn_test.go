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

import (
	"fmt"
	"testing"
)

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

// recorder stands in for *testing.T so the guard's refusals can be
// exercised without skipping or failing the test running them. It
// records the first skip or failure and nothing else.
type recorder struct {
	skipped string
	failed  string
}

func (r *recorder) Helper() {}

func (r *recorder) Skip(args ...any) {
	if r.skipped == "" {
		r.skipped = fmt.Sprint(args...)
	}
}

func (r *recorder) Skipf(format string, args ...any) {
	if r.skipped == "" {
		r.skipped = fmt.Sprintf(format, args...)
	}
}

func (r *recorder) Fatalf(format string, args ...any) {
	if r.failed == "" {
		r.failed = fmt.Sprintf(format, args...)
	}
}

// TestRequire covers each way the guard can answer: the two skips, the
// three refusals, and the DSN this project's tests actually use. A
// refusal must return no DSN at all, because a caller that ran on with
// one would be pointing destructive DDL at whatever the guard just
// rejected.
func TestRequire(t *testing.T) {
	const local = "postgresql://postgres@127.0.0.1:5432/ai_workbench_pr407"

	tests := []struct {
		name     string
		skipVar  string
		dsn      string
		wantDSN  string
		wantSkip bool
		wantFail bool
	}{
		{
			name:     "SKIP_DB_TESTS is set",
			skipVar:  "1",
			dsn:      local,
			wantSkip: true,
		},
		{
			name:     "no server configured",
			dsn:      "",
			wantSkip: true,
		},
		{
			name:     "unparseable DSN",
			dsn:      "postgresql://user:pass@host:notaport/db",
			wantFail: true,
		},
		{
			name:     "remote host",
			dsn:      "postgresql://postgres@db.example.com:5432/ai_workbench",
			wantFail: true,
		},
		{
			name:     "production-shaped database",
			dsn:      "postgresql://postgres@127.0.0.1:5432/ai_workbench_prod2",
			wantFail: true,
		},
		{
			name:    "a local test database",
			dsn:     local,
			wantDSN: local,
		},
		{
			name:    "the CI default database",
			dsn:     "postgresql://postgres@localhost:5432/postgres",
			wantDSN: "postgresql://postgres@localhost:5432/postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SKIP_DB_TESTS", tt.skipVar)
			t.Setenv("TEST_AI_WORKBENCH_SERVER", tt.dsn)

			rec := &recorder{}
			got := Require(rec, "the guard's own test")

			if got != tt.wantDSN {
				t.Errorf("Require returned %q, want %q", got, tt.wantDSN)
			}
			if (rec.skipped != "") != tt.wantSkip {
				t.Errorf("skipped = %q, want a skip: %v", rec.skipped, tt.wantSkip)
			}
			if (rec.failed != "") != tt.wantFail {
				t.Errorf("failed = %q, want a failure: %v", rec.failed, tt.wantFail)
			}
		})
	}
}
