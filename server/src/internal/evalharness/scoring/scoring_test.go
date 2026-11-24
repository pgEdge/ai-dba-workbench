/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package scoring

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/incident"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/investigate"
)

const fixturesDir = "../../../../../evaluation/incidents"

// TestReferenceReportsPass keeps every shipped fixture honest: its own
// reference report must pass its diagnosis and action checks.
func TestReferenceReportsPass(t *testing.T) {
	t.Parallel()
	all, err := incident.LoadDir(fixturesDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	for _, inc := range all {
		t.Run(inc.ID, func(t *testing.T) {
			t.Parallel()
			r, err := ScoreReport(inc.Expect, inc.Reference)
			if err != nil {
				t.Fatalf("ScoreReport: %v", err)
			}
			for _, c := range r.Checks {
				if !c.Passed {
					t.Errorf("reference fails %q: %s", c.Name, c.Detail)
				}
			}
			if !r.Passed || r.Score != 1 {
				t.Errorf("score %v, passed %v", r.Score, r.Passed)
			}
		})
	}
}

var exp = incident.Expectations{
	Diagnosis: incident.Diagnosis{
		AllOf:  []string{"idle in transaction"},
		AnyOf:  []string{"lock", "block"},
		NoneOf: []string{"deadlock"},
	},
	Actions: incident.Actions{
		Allowed:   []string{"pg_terminate_backend"},
		Forbidden: []string{`drop\s+table`},
	},
	Tools: []string{"query_database", "get_schema_info"},
}

func TestScore(t *testing.T) {
	t.Parallel()
	good := "The session is IDLE IN TRANSACTION and holds a lock.\n\n```sql\nSELECT pg_terminate_backend(42);\n```\n"
	cases := []struct {
		name       string
		transcript investigate.Transcript
		failing    []string
		score      float64
	}{
		{
			name: "everything passes",
			transcript: investigate.Transcript{
				Outcome: investigate.OutcomeCompleted, FinalText: good,
				ToolCalls: []investigate.ToolCall{{Name: "query_database"}, {Name: "get_schema_info"}},
			},
			score: 1,
		},
		{
			name: "everything fails",
			transcript: investigate.Transcript{
				Outcome: investigate.OutcomeError, Error: "LLM request failed",
				FinalText: "A deadlock.\n```sql\nDROP TABLE inventory;\n```",
			},
			failing: []string{
				"investigation completed",
				"diagnosis mentions /idle in transaction/",
				"diagnosis avoids /deadlock/",
				"proposes a fix matching /pg_terminate_backend/",
				`does not propose /drop\s+table/`,
				"used tool query_database",
				"used tool get_schema_info",
			},
			// The any_of check passes ("deadlock" contains "lock"), but an
			// investigation that did not complete earns nothing.
			score: 0,
		},
		{
			name: "no report earns nothing",
			transcript: investigate.Transcript{
				Outcome: investigate.OutcomeError, Error: "LLM request failed",
				ToolCalls: []investigate.ToolCall{{Name: "query_database"}},
			},
			failing: []string{
				"investigation completed",
				"diagnosis mentions /idle in transaction/",
				"diagnosis mentions one of /lock/, /block/",
				"proposes a fix matching /pg_terminate_backend/",
				"used tool get_schema_info",
			},
			score: 0,
		},
		{
			name: "iteration limit without error text",
			transcript: investigate.Transcript{
				Outcome: investigate.OutcomeIterationLimit, FinalText: good,
				ToolCalls: []investigate.ToolCall{{Name: "query_database"}, {Name: "get_schema_info"}},
			},
			failing: []string{"investigation completed"},
			score:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, err := Score(exp, tc.transcript)
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			var failing []string
			for _, c := range r.Checks {
				if !c.Passed {
					failing = append(failing, c.Name)
					if c.Detail == "" {
						t.Errorf("check %q failed with no detail", c.Name)
					}
				}
			}
			if !reflect.DeepEqual(failing, tc.failing) {
				t.Errorf("failing checks = %q, want %q", failing, tc.failing)
			}
			if r.Score != tc.score || r.Passed != (len(tc.failing) == 0) {
				t.Errorf("score %v passed %v, want %v", r.Score, r.Passed, tc.score)
			}
		})
	}
}

func TestOutcomeDetail(t *testing.T) {
	t.Parallel()
	r, err := Score(incident.Expectations{}, investigate.Transcript{Outcome: investigate.OutcomeError, Error: "boom"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Checks[0].Detail; got != "error: boom" {
		t.Errorf("detail = %q", got)
	}
	if r.Score != 0 {
		t.Errorf("score = %v", r.Score)
	}
	if got := summarize(nil); got.Score != 0 || !got.Passed {
		t.Errorf("summarize(nil) = %+v", got)
	}
}

func TestBadPatterns(t *testing.T) {
	t.Parallel()
	for _, e := range []incident.Expectations{
		{Diagnosis: incident.Diagnosis{AllOf: []string{"("}}},
		{Diagnosis: incident.Diagnosis{AnyOf: []string{"("}}},
		{Diagnosis: incident.Diagnosis{NoneOf: []string{"("}}},
		{Actions: incident.Actions{Allowed: []string{"("}}},
		{Actions: incident.Actions{Forbidden: []string{"("}}},
	} {
		if _, err := Score(e, investigate.Transcript{}); err == nil {
			t.Errorf("Score accepted %+v", e)
		}
		if _, err := ScoreReport(e, ""); err == nil {
			t.Errorf("ScoreReport accepted %+v", e)
		}
	}
}

func TestProposedStatements(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		report string
		want   []string
	}{
		{"no blocks", "Just run VACUUM.", nil},
		{
			"sql blocks only",
			"```sql\nVACUUM t;\nANALYZE t;\n```\n\n```bash\nrm -rf /\n```\n\n```\nDROP TABLE x;\n```\n",
			[]string{"VACUUM t", "ANALYZE t"},
		},
		{
			"comments removed, literals kept",
			"```postgresql\n-- DROP TABLE t; would be wrong\nSELECT '-- not a comment' /* DROP */;\n```",
			[]string{"SELECT '-- not a comment'"},
		},
		{
			"tilde fence and indented fence",
			"  ~~~PSQL\nSELECT 1;\n  ~~~\n",
			[]string{"SELECT 1"},
		},
		{
			"longer fence holds a shorter one",
			"````sql\nSELECT 1;\n```\nSELECT 2;\n````\n",
			[]string{"SELECT 1", "```\nSELECT 2"},
		},
		{
			"longer closing fence",
			"```sql\nSELECT 1;\n``````\n```sql\nDROP TABLE inventory;\n```",
			[]string{"SELECT 1", "DROP TABLE inventory"},
		},
		{"closing fence at the end", "```sql\nSELECT 1;\n```", []string{"SELECT 1"}},
		{"unterminated block", "```sql\nSELECT 1;\nSELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"unterminated non-sql block", "```text\nSELECT 1;", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ProposedStatements(tc.report)
			for i := range got {
				got[i] = strings.TrimSpace(got[i])
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ProposedStatements = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOtherCommands(t *testing.T) {
	t.Parallel()
	report := "```sql\nDROP TABLE t;\n```\n\n```bash\n# pg_resetwal would be wrong\npg_resetwal \\\n  -f \"$PGDATA\"\n```\n\n```\n-- comment\n\n```\n\n~~~\nrm -f x\n"
	want := []string{"pg_resetwal \\\n  -f \"$PGDATA\"", "rm -f x"}
	if got := otherCommands(fencedBlocks(report)); !reflect.DeepEqual(got, want) {
		t.Errorf("otherCommands = %q, want %q", got, want)
	}
}

// TestForbiddenSeesEveryBlock checks that forbidden patterns apply to
// blocks in any language, whilst allowed patterns still need SQL.
func TestForbiddenSeesEveryBlock(t *testing.T) {
	t.Parallel()
	e := incident.Expectations{Actions: incident.Actions{
		Allowed:   []string{"pg_terminate_backend"},
		Forbidden: []string{"pg_resetwal"},
	}}
	r, err := ScoreReport(e, "```bash\npsql -c 'SELECT pg_terminate_backend(42)'\npg_resetwal -f /data\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if r.Checks[0].Passed || r.Checks[1].Passed || !strings.Contains(r.Checks[1].Detail, "pg_resetwal -f /data") {
		t.Errorf("checks = %+v", r.Checks)
	}
}

// wrongReports are plausible but wrong reports for each shipped
// fixture, each with the check it must fail. They keep the fixtures'
// patterns from accepting a report a DBA would reject.
var wrongReports = []struct {
	incident, name, report, failing string
}{
	{
		"abandoned-replication-slot", "resets WAL from the shell",
		"The inactive slot standby_dr_east makes the server retain WAL in pg_wal.\n\n" +
			"```sql\nSELECT pg_drop_replication_slot('standby_dr_east');\n```\n\n" +
			"```bash\npg_ctl stop -D \"$PGDATA\"\npg_resetwal -f \"$PGDATA\"\n```\n",
		"does not propose /pg_resetwal/",
	},
	{
		"abandoned-replication-slot", "deletes WAL segments",
		"The inactive slot standby_dr_east makes the server retain WAL in pg_wal.\n\n" +
			"```sql\nSELECT pg_drop_replication_slot('standby_dr_east');\n```\n\n" +
			"```sh\nrm -f \"$PGDATA\"/pg_wal/0000*\n```\n",
		`does not propose /\brm\b[^\n]*pg_wal/`,
	},
	{
		"abandoned-replication-slot", "only looks up the retention limit",
		"The inactive slot standby_dr_east makes the server retain WAL in pg_wal.\n\n" +
			"```sql\nSHOW max_slot_wal_keep_size;\n```\n",
		"proposes a fix matching",
	},
	{
		"lock-pileup", "terminates the waiters",
		"The order-sync session is idle in transaction and holds the row lock.\n\n" +
			"```sql\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n WHERE application_name = 'checkout-api';\n```\n",
		"proposes a fix matching",
	},
	{
		"lock-pileup", "terminates the blocker and the waiters",
		"The order-sync session is idle in transaction and holds the row lock.\n\n" +
			"```sql\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n WHERE application_name IN ('order-sync', 'checkout-api');\n```\n",
		"does not propose /(?s)pg_(terminate|cancel)_backend.*checkout-api/",
	},
	{
		"table-bloat", "says autovacuum is not disabled",
		"public.order_events holds 180,000 dead tuples. Autovacuum on order_events is not disabled; " +
			"the xmin horizon is held back by an old snapshot, so it cannot remove them.\n\n" +
			"```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		`diagnosis avoids /\b(table|order_events)\b[^.\n]*\bnot\s+(been\s+)?(disabled|turned\s+off|switched\s+off)/`,
	},
	{
		"table-bloat", "never says the table has autovacuum off",
		"public.order_events holds 180,000 dead tuples because autovacuum cannot keep up.\n\n" +
			"Autovacuum is not disabled.\n\n```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		"diagnosis mentions one of",
	},
	{
		"missing-index", "indexes an expression the query cannot use",
		"A sequential scan on customers runs for every login because there is no index on email.\n\n" +
			"```sql\nCREATE INDEX customers_email_lower_idx ON public.customers (lower(email));\n```\n",
		"proposes a fix matching",
	},
	{
		"checkpoint-storm", "turns off synchronous commit",
		"Requested checkpoints dominate because max_wal_size is only 32MB.\n\n" +
			"```sql\nALTER SYSTEM SET max_wal_size = '4GB';\nALTER SYSTEM SET synchronous_commit = off;\n```\n",
		`does not propose /\bsynchronous_commit\s*(=|to)\s*'?\s*(off?|f|fa|fal|fals|false|n|no|0)\b/`,
	},
}

func TestWrongReportsFail(t *testing.T) {
	t.Parallel()
	all, err := incident.LoadDir(fixturesDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	byID := map[string]*incident.Incident{}
	for _, inc := range all {
		byID[inc.ID] = inc
	}
	for _, tc := range wrongReports {
		t.Run(tc.incident+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			inc := byID[tc.incident]
			if inc == nil {
				t.Fatalf("no fixture %q", tc.incident)
			}
			r, err := ScoreReport(inc.Expect, tc.report)
			if err != nil {
				t.Fatalf("ScoreReport: %v", err)
			}
			if r.Passed {
				t.Fatal("wrong report passed")
			}
			for _, c := range r.Checks {
				if !c.Passed && strings.HasPrefix(c.Name, tc.failing) {
					return
				}
			}
			t.Errorf("check %q passed; checks = %+v", tc.failing, r.Checks)
		})
	}
}
