/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/pkg/redact"
)

// Values of anomaly_candidates.tier3_source.
const (
	tier3SourceDecision  = "decision"
	tier3SourceReasoning = "reasoning"
)

// decisionSuppressThreshold returns the configured decision suppress
// threshold, or the default when it is outside (0, 1). Validation rejects
// such a value whilst a decision provider is configured, so the default
// applies only to a configuration built without validation.
func decisionSuppressThreshold(cfg *config.Config) float64 {
	t := cfg.Anomaly.Tier3.DecisionSuppressThreshold
	if math.IsNaN(t) || t <= 0 || t >= 1 {
		return config.DefaultDecisionSuppressThreshold
	}
	return t
}

// decisionSummary is the Tier 3 result recorded for a decision model
// verdict. A decision model returns a probability rather than text, so
// the result says what was decided and why in place of a model response;
// it reaches the alert's anomaly_details, and re-evaluation reads it from
// there as the Tier 3 analysis.
func decisionSummary(provider, model string, alert bool, realIssueProbability, threshold float64) string {
	verdict := "suppress"
	if alert {
		verdict = "alert"
	}
	return fmt.Sprintf("Decision model %s/%s: %s (P(real issue)=%.2f, suppress threshold %.2f)",
		provider, model, verdict, realIssueProbability, threshold)
}

// classifyWithDecision classifies candidate with the decision model,
// giving it classificationContext as its input. On success it records the
// verdict and returns true. On failure it records nothing on the
// candidate beyond its Tier 3 source, logs the error, and returns a
// description of the failure for the caller to store. The error is
// redacted once, as the alerter's own log is, and the same text is
// logged and returned.
func (e *Engine) classifyWithDecision(ctx context.Context, candidate *database.AnomalyCandidate,
	classificationContext string, cfg *config.Config, timeout time.Duration) (string, bool) {
	provider, model := e.decisionProvider.ProviderName(), e.decisionProvider.ModelName()
	setTier3Source(candidate, tier3SourceDecision, provider, model)
	threshold := decisionSuppressThreshold(cfg)

	decisionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	verdict, err := e.decisionProvider.ClassifyDecision(decisionCtx, classificationContext, threshold)
	if err != nil {
		msg := redact.ProviderLogError(err.Error(), configuredSecrets(cfg))
		e.log("ERROR: Tier 3 decision model classification failed for candidate %d: %s", candidate.ID, msg)
		return fmt.Sprintf("decision model %s/%s failed: %s", provider, model, msg), false
	}

	result := decisionSummary(provider, model, verdict.Alert, verdict.RealIssueProbability, threshold)
	tier3Pass := verdict.Alert
	candidate.Tier3Pass = &tier3Pass
	candidate.Tier3Result = &result
	e.debugLog("Tier 3: %s for candidate %d", result, candidate.ID)
	return "", true
}
