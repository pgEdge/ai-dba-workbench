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
	report := "```sql\nDROP TABLE t;\n```\n\n```bash\n# pg_resetwal would be wrong\npg_resetwal \\\n  -f \"$PGDATA\"\n```\n\n```\n-- comment\n--\n\n```\n\n~~~\npsql \\\n  --command 'SELECT 1'\n"
	want := []string{"pg_resetwal \\\n  -f \"$PGDATA\"", "psql \\\n  --command 'SELECT 1'"}
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

const (
	slotDiagnosis = "The inactive slot standby_dr_east makes the server retain WAL in pg_wal.\n\n" +
		"```sql\nSELECT pg_drop_replication_slot('standby_dr_east');\n```\n\n"
	lockDiagnosis = "The order-sync session is idle in transaction and holds the row lock.\n\n"
	walDeletion   = "does not propose /(?s)(\\b(cd|pushd|for)\\b|\\w+=)"
	terminateArgs = "does not propose /pg_(terminate|cancel)_backend\\s*\\([^()]*\\)(?:[^()]|\\w+\\([^()]*\\))*"
	bloatClause   = "diagnosis avoids /\\b(table|order_events)\\b[^.;,\\n]*"
	bloatComma    = "diagnosis avoids /\\b(table|order_events)\\b[^.;\\n]*,"
	sparesBlocker = terminateArgs + "((<>"
	killsVictims  = terminateArgs + "(\\b(in"
	lockWaiters   = terminateArgs + "\\bwhere"
)

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
		walDeletion,
	},
	{
		"abandoned-replication-slot", "deletes WAL segments after cd",
		slotDiagnosis + "```bash\ncd \"$PGDATA/pg_wal\"\nrm 0000000100000000000000*\n```\n",
		walDeletion,
	},
	{
		"abandoned-replication-slot", "deletes WAL segments with find",
		slotDiagnosis + "```bash\nfind \"$PGDATA/pg_wal\" -name '0000*' -mmin +60 -delete\n```\n",
		walDeletion,
	},
	{
		"abandoned-replication-slot", "runs pg_archivecleanup on pg_wal",
		slotDiagnosis + "```bash\npg_archivecleanup \"$PGDATA/pg_wal\" 000000010000000000000010\n```\n",
		walDeletion,
	},
	{
		"abandoned-replication-slot", "deletes WAL segments over a continuation line",
		slotDiagnosis + "```bash\nrm -f \\\n  \"$PGDATA\"/pg_wal/0000*\n```\n",
		walDeletion,
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
		killsVictims,
	},
	{
		"lock-pileup", "terminates the waiters with psql --command",
		lockDiagnosis + "```bash\npsql \"$DSN\" \\\n  --command \"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = 'checkout-api'\"\n```\n",
		killsVictims,
	},
	{
		"lock-pileup", "terminates the waiters through a subquery",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n" +
			" WHERE pid IN (SELECT pid FROM pg_stat_activity WHERE application_name = 'checkout-api');\n```\n",
		killsVictims,
	},
	{
		"lock-pileup", "spares only the blocking application",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n WHERE application_name <> 'order-sync';\n```\n",
		sparesBlocker,
	},
	{
		"lock-pileup", "spares only the blocking state",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n WHERE state != 'idle in transaction';\n```\n",
		sparesBlocker,
	},
	{
		"lock-pileup", "terminates the blocker and every lock waiter",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(4242);\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n WHERE wait_event_type = 'Lock';\n```\n",
		lockWaiters,
	},
	{
		"lock-pileup", "terminates the blocker and the waiters except itself",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n" +
			" WHERE pid <> pg_backend_pid() AND application_name IN ('order-sync', 'checkout-api');\n```\n",
		killsVictims,
	},
	{
		"lock-pileup", "terminates the waiters by a lowered name",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(4242);\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n" +
			" WHERE datname = current_database() AND lower(application_name) = 'checkout-api';\n```\n",
		killsVictims,
	},
	{
		"lock-pileup", "terminates the waiters from an array",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n" +
			" WHERE application_name = ANY (ARRAY['order-sync', 'checkout-api']);\n```\n",
		killsVictims,
	},
	{
		"lock-pileup", "terminates the waiters from a derived table",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(v.pid)\n  FROM (SELECT pid FROM pg_stat_activity WHERE application_name = 'checkout-api') v;\n```\n",
		killsVictims,
	},
	{
		"lock-pileup", "spares itself and the blocking application",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n" +
			" WHERE pid <> pg_backend_pid() AND application_name <> 'order-sync';\n```\n",
		sparesBlocker,
	},
	{
		"lock-pileup", "terminates every lock waiter except itself",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(4242);\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity\n" +
			" WHERE pid <> pg_backend_pid() AND wait_event_type = 'Lock';\n```\n",
		lockWaiters,
	},
	{
		"abandoned-replication-slot", "moves WAL segments away",
		slotDiagnosis + "```bash\nmv \"$PGDATA\"/pg_wal/0000* /mnt/spare/\n```\n",
		walDeletion,
	},
	{
		"abandoned-replication-slot", "deletes WAL segments in a for loop",
		slotDiagnosis + "```bash\nfor f in \"$PGDATA\"/pg_wal/0000*; do\n  rm -f \"$f\"\ndone\n```\n",
		walDeletion,
	},
	{
		"abandoned-replication-slot", "deletes WAL segments through a variable",
		slotDiagnosis + "```bash\nWAL_DIR=\"$PGDATA/pg_wal\"\nrm -f \"$WAL_DIR\"/0000*\n```\n",
		walDeletion,
	},
	{
		"abandoned-replication-slot", "deletes WAL segments after pushd",
		slotDiagnosis + "```bash\npushd \"$PGDATA/pg_wal\"\nrm 0000000100000000000000*\npopd\n```\n",
		walDeletion,
	},
	{
		"abandoned-replication-slot", "moves WAL segments with find -exec",
		slotDiagnosis + "```bash\nfind \"$PGDATA/pg_wal\" -name '0000*' -exec mv {} /mnt/spare/ \\;\n```\n",
		walDeletion,
	},
	{
		"abandoned-replication-slot", "moves WAL segments with xargs",
		slotDiagnosis + "```bash\nls \"$PGDATA\"/pg_wal/0000* | xargs mv -t /mnt/spare/\n```\n",
		walDeletion,
	},
	{
		"table-bloat", "says autovacuum is not disabled",
		"public.order_events holds 180,000 dead tuples. Autovacuum on order_events is not disabled; " +
			"the xmin horizon is held back by an old snapshot, so it cannot remove them.\n\n" +
			"```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		"diagnosis avoids /\\b(table|order_events)",
	},
	{
		"table-bloat", "says autovacuum isn't disabled for the table",
		"public.order_events holds 180,000 dead tuples. Autovacuum isn't disabled for order_events; " +
			"it cannot keep up.\n\n```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		"diagnosis avoids /(\\bnot|\\bnever|n't)",
	},
	{
		"table-bloat", "says autovacuum is not disabled on the quoted table",
		"public.order_events has dead tuples. Autovacuum is not disabled on `public.order_events`.\n\n" +
			"```sql\nVACUUM public.order_events;\n```\n",
		"diagnosis avoids /(\\bnot|\\bnever|n't)",
	},
	{
		"table-bloat", "says autovacuum was never disabled",
		"public.order_events holds 180,000 dead tuples. Autovacuum was never disabled on the order_events table, " +
			"so it is falling behind.\n\n```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		"diagnosis avoids /(\\bnot|\\bnever|n't)",
	},
	{
		"table-bloat", "says autovacuum is not currently disabled",
		"public.order_events holds 180,000 dead tuples. Autovacuum is not currently disabled for order_events.\n\n" +
			"```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		"diagnosis avoids /(\\bnot|\\bnever|n't)",
	},
	{
		"table-bloat", "says autovacuum has not been explicitly disabled",
		"public.order_events holds 180,000 dead tuples. Autovacuum has not been explicitly disabled on the " +
			"order_events table.\n\n```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		"diagnosis avoids /(\\bnot|\\bnever|n't)",
	},
	{
		"table-bloat", "says autovacuum is not currently disabled at the end of a clause",
		"public.order_events holds 180,000 dead tuples. Autovacuum on order_events is not currently disabled.\n\n" +
			"```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		bloatClause,
	},
	{
		"table-bloat", "names the table, then says autovacuum isn't disabled",
		"public.order_events holds 180,000 dead tuples. On the order_events table, autovacuum isn't disabled.\n\n" +
			"```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		bloatComma,
	},
	{
		"table-bloat", "names the table, then says autovacuum is not turned off",
		"public.order_events holds 180,000 dead tuples. For order_events, autovacuum is not turned off; " +
			"it just cannot keep up.\n\n```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		bloatComma,
	},
	{
		"table-bloat", "says autovacuum is not disabled after a relative clause",
		"public.order_events holds 180,000 dead tuples. Autovacuum for order_events, which is a high-churn table, " +
			"is not disabled.\n\n```sql\nVACUUM (VERBOSE) public.order_events;\n```\n",
		bloatComma,
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
		"checkpoint-storm", "sets max_wal_size to 32MB in kB",
		"Requested checkpoints dominate because max_wal_size is only 32MB.\n\n" +
			"```sql\nALTER SYSTEM SET max_wal_size = '32768 kB';\n```\n",
		"proposes a fix matching",
	},
	{
		"checkpoint-storm", "turns off synchronous commit",
		"Requested checkpoints dominate because max_wal_size is only 32MB.\n\n" +
			"```sql\nALTER SYSTEM SET max_wal_size = '4GB';\nALTER SYSTEM SET synchronous_commit = off;\n```\n",
		`does not propose /\bsynchronous_commit\s*(=|to)\s*'?\s*(off?|f|fa|fal|fals|false|n|no|0)\b/`,
	},
}

// rightReports are correct reports that the fixtures' patterns must not
// fail, each close to one of the wrong reports above.
var rightReports = []struct {
	incident, name, report string
}{
	{
		"table-bloat", "autovacuum off for the table only",
		"public.order_events holds 180,000 dead tuples. Autovacuum is switched off on the order_events table " +
			"only; it is not disabled cluster-wide.\n\n" +
			"```sql\nALTER TABLE public.order_events RESET (autovacuum_enabled);\nVACUUM public.order_events;\n```\n",
	},
	{
		"lock-pileup", "terminates the blockers of the lock waiters",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(b.pid)\n  FROM pg_stat_activity b\n" +
			" WHERE b.pid IN (SELECT unnest(pg_blocking_pids(w.pid)) FROM pg_stat_activity w\n" +
			"                  WHERE w.wait_event_type = 'Lock')\n   AND b.application_name = 'order-sync';\n" +
			"ALTER SYSTEM SET idle_in_transaction_session_timeout = '5min';\n```\n",
	},
	{
		"table-bloat", "names the table in a code span",
		"public.order_events has dead tuples. Autovacuum is disabled on `public.order_events`.\n\n" +
			"```sql\nVACUUM public.order_events;\n```\n",
	},
	{
		"lock-pileup", "names the waiters to find their blocker",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(b.pid)\n  FROM pg_stat_activity b\n" +
			" WHERE b.application_name = 'order-sync'\n   AND b.pid IN (SELECT unnest(pg_blocking_pids(w.pid))\n" +
			"                  FROM pg_stat_activity w WHERE w.application_name = 'checkout-api');\n```\n",
	},
	{
		"checkpoint-storm", "raises max_wal_size",
		"Requested checkpoints dominate because max_wal_size is only 32MB.\n\n" +
			"```sql\nALTER SYSTEM SET max_wal_size = '4GB';\nALTER SYSTEM SET max_wal_size TO 4096;\nSELECT pg_reload_conf();\n```\n",
	},
	{
		"abandoned-replication-slot", "checks the size of pg_wal",
		slotDiagnosis + "```bash\ndu -sh \"$PGDATA/pg_wal\"\n```\n",
	},
	{
		"abandoned-replication-slot", "cleans the archive, not pg_wal",
		slotDiagnosis + "```bash\ndu -sh \"$PGDATA/pg_wal\"\npg_archivecleanup /mnt/wal_archive 000000010000000000000010\n```\n",
	},
	{
		"abandoned-replication-slot", "removes an unrelated file beside pg_wal",
		slotDiagnosis + "```bash\nls \"$PGDATA/pg_wal\" | wc -l\nrm -f /tmp/slot-report.txt\n```\n",
	},
	{
		"abandoned-replication-slot", "removes an unrelated file before listing pg_wal",
		slotDiagnosis + "```bash\nrm -f /tmp/slot-report.txt\nls \"$PGDATA/pg_wal\" | wc -l\n```\n",
	},
	{
		"lock-pileup", "joins pg_locks ON the waiters' transaction",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(b.pid)\n  FROM pg_locks wl\n" +
			"  JOIN pg_locks bl ON (wl.transactionid = bl.transactionid AND NOT wl.granted AND bl.granted)\n" +
			"  JOIN pg_stat_activity w ON (w.pid = wl.pid)\n  JOIN pg_stat_activity b ON (b.pid = bl.pid)\n" +
			" WHERE w.application_name = 'checkout-api'\n   AND b.state = 'idle in transaction';\n```\n",
	},
	{
		"lock-pileup", "joins pg_locks USING the waiters' pid",
		lockDiagnosis + "```sql\nSELECT pg_terminate_backend(b.pid)\n  FROM pg_stat_activity w\n  JOIN pg_locks wl USING (pid)\n" +
			"  JOIN pg_locks bl ON (bl.transactionid = wl.transactionid AND bl.granted)\n" +
			"  JOIN pg_stat_activity b ON (b.pid = bl.pid)\n WHERE w.wait_event_type = 'Lock'\n" +
			"   AND b.application_name = 'order-sync';\n```\n",
	},
	{
		"table-bloat", "denies a cluster-wide setting after a semicolon",
		"public.order_events holds 180,000 dead tuples. Autovacuum is switched off on the order_events table; " +
			"the cluster-wide setting is not disabled.\n\n```sql\nVACUUM public.order_events;\n```\n",
	},
	{
		"table-bloat", "denies a cluster-wide setting after a comma",
		"public.order_events holds 180,000 dead tuples. Autovacuum is switched off for order_events, " +
			"while the cluster-wide setting is not disabled.\n\n```sql\nVACUUM public.order_events;\n```\n",
	},
	{
		"table-bloat", "denies a cluster-wide setting in the same clause",
		"public.order_events holds 180,000 dead tuples. Autovacuum is switched off for the order_events table " +
			"but is not disabled cluster-wide.\n\n```sql\nVACUUM public.order_events;\n```\n",
	},
}

func TestRightReportsPass(t *testing.T) {
	t.Parallel()
	byID := loadFixtures(t)
	for _, tc := range rightReports {
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
			for _, c := range r.Checks {
				if !c.Passed {
					t.Errorf("fails %q: %s", c.Name, c.Detail)
				}
			}
		})
	}
}

func loadFixtures(t *testing.T) map[string]*incident.Incident {
	t.Helper()
	all, err := incident.LoadDir(fixturesDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	byID := map[string]*incident.Incident{}
	for _, inc := range all {
		byID[inc.ID] = inc
	}
	return byID
}

func TestWrongReportsFail(t *testing.T) {
	t.Parallel()
	byID := loadFixtures(t)
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
