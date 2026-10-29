/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package anomaly holds the Tier 1 statistical anomaly definitions that
// more than one component needs: which baseline row a value is scored
// against, how the z-score divisor is floored, when a value counts as
// within the sensitivity band, the shipped scoring defaults, and the
// historical SQL each baselined metric is computed from.
//
// The alerter is the reference consumer and owns the detection loop;
// this package carries no state and performs no I/O, so any component
// that reads metric_baselines can reproduce the alerter's verdict for a
// value without drifting from it.
package anomaly
