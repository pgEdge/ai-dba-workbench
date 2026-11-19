/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package runner replays incidents against one or more models: for each
// incident, target and repetition it stages the incident afresh, asks the
// Investigator to diagnose it, scores the transcript and tears the
// incident down again.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/incident"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/investigate"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/report"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/scoring"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/stage"
)

// DefaultTeardownTimeout bounds how long tearing one incident down may
// take, including after the run has been canceled.
const DefaultTeardownTimeout = 2 * time.Minute

// Staging is a staged incident.
type Staging interface {
	PromptData() incident.PromptData
	Teardown(ctx context.Context) error
}

// StageFunc stages one incident. On error it must leave nothing behind.
type StageFunc func(ctx context.Context, inc *incident.Incident) (Staging, error)

// FromStager adapts a stage.Stager to a StageFunc.
func FromStager(s *stage.Stager) StageFunc {
	return func(ctx context.Context, inc *incident.Incident) (Staging, error) {
		st, err := s.Stage(ctx, inc)
		if err != nil {
			return nil, err
		}
		return st, nil
	}
}

// Runner replays incidents. Runs are sequential: incidents change
// instance-wide state (replication slots, ALTER SYSTEM settings), so two
// staged at once could disturb each other.
type Runner struct {
	Stage        StageFunc
	Investigator investigate.Investigator
	Targets      []investigate.Target
	// Runs is the number of repetitions of each incident per target.
	Runs int
	// MaxIterations bounds the LLM calls in one investigation.
	MaxIterations int
	// TeardownTimeout defaults to DefaultTeardownTimeout.
	TeardownTimeout time.Duration
	// Progress, if set, receives one line per run.
	Progress io.Writer
}

// Run replays every incident Runs times against every target and
// returns one report.Run per replay. It stops early, returning the runs
// completed so far, when ctx is done or an incident cannot be torn down
// (since what it left behind could distort the runs after it).
func (r *Runner) Run(ctx context.Context, incidents []*incident.Incident) ([]report.Run, error) {
	if r.Runs < 1 || len(r.Targets) == 0 {
		return nil, errors.New("at least one target and one run are required")
	}
	total := len(incidents) * len(r.Targets) * r.Runs
	runs := make([]report.Run, 0, total)
	for _, inc := range incidents {
		for _, target := range r.Targets {
			for i := 1; i <= r.Runs; i++ {
				run, err := r.replay(ctx, inc, target, i)
				if run != nil {
					runs = append(runs, *run)
					r.progress(len(runs), total, run)
				}
				if err != nil {
					return runs, err
				}
			}
		}
	}
	return runs, nil
}

// replay stages, investigates, scores and tears down one incident. It
// returns a nil Run only when ctx was done before the investigation
// finished.
func (r *Runner) replay(ctx context.Context, inc *incident.Incident, target investigate.Target, index int) (*report.Run, error) {
	run := &report.Run{Incident: inc.ID, Title: inc.Title, Target: target.String(), Index: index}
	staged, err := r.Stage(ctx, inc)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		run.Transcript = investigate.Transcript{
			Outcome: investigate.OutcomeError,
			Error:   "staging: " + err.Error(),
		}
		return run, r.score(run, inc)
	}

	transcript, invErr := r.Investigator.Investigate(ctx, investigate.Request{
		Prompt:        inc.RenderPrompt(staged.PromptData()),
		Target:        target,
		MaxIterations: r.MaxIterations,
	})

	timeout := r.TeardownTimeout
	if timeout <= 0 {
		timeout = DefaultTeardownTimeout
	}
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := staged.Teardown(tctx); err != nil {
		err = fmt.Errorf("tearing down %s: %w", inc.ID, err)
		if invErr != nil {
			return nil, errors.Join(invErr, err)
		}
		run.Transcript = transcript
		return run, errors.Join(r.score(run, inc), err)
	}
	if invErr != nil {
		return nil, invErr
	}
	run.Transcript = transcript
	return run, r.score(run, inc)
}

func (r *Runner) score(run *report.Run, inc *incident.Incident) error {
	res, err := scoring.Score(inc.Expect, run.Transcript)
	if err != nil {
		return fmt.Errorf("scoring %s: %w", inc.ID, err)
	}
	run.Result = res
	return nil
}

func (r *Runner) progress(n, total int, run *report.Run) {
	if r.Progress == nil {
		return
	}
	verdict := "FAILED"
	if run.Result.Passed {
		verdict = "passed"
	}
	_, _ = fmt.Fprintf(r.Progress, "[%d/%d] %s on %s, run %d: %s (score %.2f, %s, %d iterations, %s)\n",
		n, total, run.Incident, run.Target, run.Index, verdict, run.Result.Score,
		run.Transcript.Outcome, run.Transcript.Iterations, run.Transcript.Duration.Round(time.Second))
}
