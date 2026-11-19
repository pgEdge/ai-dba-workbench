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
			// The any_of check passes: "deadlock" contains "lock".
			score: 1.0 / 8,
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
			score:   7.0 / 8,
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
