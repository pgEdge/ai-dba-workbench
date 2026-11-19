/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package runner

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/incident"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/investigate"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/stage"
)

type fakeStaging struct {
	data     incident.PromptData
	torn     *int
	teardown error
	deadline *bool
}

func (f *fakeStaging) PromptData() incident.PromptData { return f.data }

func (f *fakeStaging) Teardown(ctx context.Context) error {
	*f.torn++
	if f.deadline != nil {
		_, *f.deadline = ctx.Deadline()
	}
	return f.teardown
}

type fakeInvestigator struct {
	prompts []string
	answer  func(req investigate.Request) (investigate.Transcript, error)
}

func (f *fakeInvestigator) Investigate(_ context.Context, req investigate.Request) (investigate.Transcript, error) {
	f.prompts = append(f.prompts, req.Prompt)
	return f.answer(req)
}

func testIncident(id string) *incident.Incident {
	return &incident.Incident{
		ID: id, Title: "Title " + id,
		Prompt: "Investigate connection {{connection_id}} ({{connection_name}}, {{database}})",
		Expect: incident.Expectations{
			Diagnosis: incident.Diagnosis{AllOf: []string{"idle in transaction"}},
			Tools:     []string{"query_database"},
		},
	}
}

func goodTranscript(req investigate.Request) (investigate.Transcript, error) {
	return investigate.Transcript{
		Outcome: investigate.OutcomeCompleted, Iterations: 2,
		FinalText: "A session is idle in transaction on " + req.Target.String(),
		ToolCalls: []investigate.ToolCall{{Name: "query_database"}},
		Duration:  3 * time.Second,
	}, nil
}

func TestRun(t *testing.T) {
	t.Parallel()
	torn := 0
	var gotDeadline bool
	inv := &fakeInvestigator{answer: goodTranscript}
	var progress bytes.Buffer
	r := &Runner{
		Stage: func(_ context.Context, inc *incident.Incident) (Staging, error) {
			if inc.ID == "broken" {
				return nil, errors.New("setup failed")
			}
			return &fakeStaging{data: incident.PromptData{ConnectionID: 7, ConnectionName: "eval-" + inc.ID, Database: "db_" + inc.ID},
				torn: &torn, deadline: &gotDeadline}, nil
		},
		Investigator:  inv,
		Targets:       []investigate.Target{{Provider: "anthropic"}, {Provider: "openai", Model: "gpt-x"}},
		Runs:          2,
		MaxIterations: 5,
		Progress:      &progress,
	}
	runs, err := r.Run(context.Background(), []*incident.Incident{testIncident("lock"), testIncident("broken")})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 8 || torn != 4 || !gotDeadline {
		t.Fatalf("%d runs, %d teardowns, deadline %v", len(runs), torn, gotDeadline)
	}
	if runs[0].Target != "anthropic" || runs[1].Index != 2 || runs[2].Target != "openai:gpt-x" || !runs[0].Result.Passed {
		t.Errorf("runs %+v", runs[:3])
	}
	if inv.prompts[0] != "Investigate connection 7 (eval-lock, db_lock)" {
		t.Errorf("prompt %q", inv.prompts[0])
	}
	b := runs[4]
	if b.Incident != "broken" || b.Result.Passed || b.Transcript.Outcome != investigate.OutcomeError ||
		b.Transcript.Error != "staging: setup failed" {
		t.Errorf("staging failure run %+v", b)
	}
	out := progress.String()
	if !strings.Contains(out, "[1/8] lock on anthropic, run 1: passed (score 1.00, completed, 2 iterations, 3s)") ||
		!strings.Contains(out, "[8/8] broken on openai:gpt-x, run 2: FAILED") {
		t.Errorf("progress:\n%s", out)
	}
}

func TestRunErrors(t *testing.T) {
	t.Parallel()
	ok := func(_ context.Context, _ *incident.Incident) (Staging, error) {
		n := 0
		return &fakeStaging{torn: &n}, nil
	}
	targets := []investigate.Target{{Provider: "anthropic"}}

	t.Run("no targets", func(t *testing.T) {
		t.Parallel()
		if _, err := (&Runner{Runs: 1}).Run(context.Background(), nil); err == nil {
			t.Error("no error")
		}
	})

	t.Run("teardown failure stops the run", func(t *testing.T) {
		t.Parallel()
		torn := 0
		r := &Runner{
			Stage: func(context.Context, *incident.Incident) (Staging, error) {
				return &fakeStaging{torn: &torn, teardown: errors.New("drop failed")}, nil
			},
			Investigator: &fakeInvestigator{answer: goodTranscript}, Targets: targets, Runs: 3,
		}
		runs, err := r.Run(context.Background(), []*incident.Incident{testIncident("a")})
		if err == nil || !strings.Contains(err.Error(), "tearing down a: drop failed") || len(runs) != 1 || torn != 1 {
			t.Errorf("err %v, %d runs, %d teardowns", err, len(runs), torn)
		}
	})

	t.Run("canceled investigation still tears down", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		torn := 0
		r := &Runner{
			Stage: func(context.Context, *incident.Incident) (Staging, error) {
				return &fakeStaging{torn: &torn}, nil
			},
			Investigator: &fakeInvestigator{answer: func(investigate.Request) (investigate.Transcript, error) {
				cancel()
				return investigate.Transcript{}, context.Canceled
			}},
			Targets: targets, Runs: 2,
		}
		runs, err := r.Run(ctx, []*incident.Incident{testIncident("a")})
		if !errors.Is(err, context.Canceled) || len(runs) != 0 || torn != 1 {
			t.Errorf("err %v, %d runs, %d teardowns", err, len(runs), torn)
		}
	})

	t.Run("canceled investigation and failed teardown", func(t *testing.T) {
		t.Parallel()
		torn := 0
		r := &Runner{
			Stage: func(context.Context, *incident.Incident) (Staging, error) {
				return &fakeStaging{torn: &torn, teardown: errors.New("drop failed")}, nil
			},
			Investigator: &fakeInvestigator{answer: func(investigate.Request) (investigate.Transcript, error) {
				return investigate.Transcript{}, context.DeadlineExceeded
			}},
			Targets: targets, Runs: 1,
		}
		_, err := r.Run(context.Background(), []*incident.Incident{testIncident("a")})
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "drop failed") {
			t.Errorf("err %v", err)
		}
	})

	t.Run("canceled during staging", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := &Runner{
			Stage: func(ctx context.Context, _ *incident.Incident) (Staging, error) {
				return nil, ctx.Err()
			},
			Investigator: &fakeInvestigator{answer: goodTranscript}, Targets: targets, Runs: 1,
		}
		if runs, err := r.Run(ctx, []*incident.Incident{testIncident("a")}); !errors.Is(err, context.Canceled) || len(runs) != 0 {
			t.Errorf("err %v, %d runs", err, len(runs))
		}
	})

	t.Run("bad pattern", func(t *testing.T) {
		t.Parallel()
		inc := testIncident("a")
		inc.Expect.Diagnosis.AllOf = []string{"("}
		r := &Runner{Stage: ok, Investigator: &fakeInvestigator{answer: goodTranscript}, Targets: targets, Runs: 1}
		if _, err := r.Run(context.Background(), []*incident.Incident{inc}); err == nil || !strings.Contains(err.Error(), "scoring a") {
			t.Errorf("err %v", err)
		}
	})
}

func TestFromStager(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := &stage.Stager{AdminDSN: "postgres://%zz", DatastoreDSN: "postgres://%zz"}
	st, err := FromStager(s)(ctx, testIncident("a"))
	if err == nil || st != nil {
		t.Errorf("FromStager with a bad DSN = %v, %v", st, err)
	}
}
