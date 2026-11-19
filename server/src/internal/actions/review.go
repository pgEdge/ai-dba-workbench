/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package actions

import "context"

// Review decisions and review statuses.
const (
	decisionPass = "pass"
	decisionVeto = "veto"

	reviewStatusPending = "pending"
	reviewStatusPassed  = "passed"
	reviewStatusVetoed  = "vetoed"
)

// Reviewer examines a proposed action and decides whether it may go
// forward. An error means no verdict was reached; the caller must leave
// the action pending so that it cannot be executed.
type Reviewer interface {
	Review(ctx context.Context, a *Action) (Verdict, error)
}

// StubReviewer passes every action as low danger. It stands in until a
// model-backed reviewer is wired up.
type StubReviewer struct{}

// Review returns a passing, low-danger verdict.
func (StubReviewer) Review(_ context.Context, _ *Action) (Verdict, error) {
	return Verdict{Decision: decisionPass, Danger: "low", Reviewer: "none"}, nil
}

// dangerRank orders danger levels; an unknown level ranks worst.
func dangerRank(d string) int {
	switch d {
	case "low":
		return 0
	case "moderate":
		return 1
	case "high":
		return 2
	case "critical":
		return 3
	}
	return 4
}
