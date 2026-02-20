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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// detectAnomalies runs the tiered anomaly detection
func (e *Engine) detectAnomalies(ctx context.Context) {
	e.debugLog("Running anomaly detection...")

	if !e.config.Anomaly.Tier1.Enabled {
		return
	}

	// Get all active connections
	connections, err := e.datastore.GetActiveConnections(ctx)
	if err != nil {
		e.log("ERROR: Failed to get active connections: %v", err)
		return
	}

	// Get all enabled alert rules
	rules, err := e.datastore.GetEnabledAlertRules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get alert rules: %v", err)
		return
	}

	sensitivity := e.config.Anomaly.Tier1.DefaultSensitivity

	// For each connection and metric, check for anomalies
	for _, connID := range connections {
		if ctx.Err() != nil {
			return
		}

		for _, rule := range rules {
			// Get current metric value
			values, err := e.datastore.GetLatestMetricValues(ctx, rule.MetricName)
			if err != nil {
				continue
			}

			// Find value for this connection
			var currentValue *database.MetricValue
			for i := range values {
				if values[i].ConnectionID == connID {
					currentValue = &values[i]
					break
				}
			}
			if currentValue == nil {
				continue
			}

			// Get baseline for this metric/connection
			baselines, err := e.datastore.GetMetricBaselines(ctx, connID, rule.MetricName)
			if err != nil || len(baselines) == 0 {
				continue
			}

			baseline := baselines[0]
			if baseline.StdDev == 0 {
				continue
			}

			// Calculate z-score
			zScore := (currentValue.Value - baseline.Mean) / baseline.StdDev

			// Check if z-score exceeds threshold
			if zScore > sensitivity || zScore < -sensitivity {
				e.debugLog("Tier 1 anomaly detected: %s on connection %d (z-score: %.2f)",
					rule.MetricName, connID, zScore)

				// Create anomaly candidate for further processing
				candidate := &database.AnomalyCandidate{
					ConnectionID: connID,
					MetricName:   rule.MetricName,
					MetricValue:  currentValue.Value,
					ZScore:       zScore,
					DetectedAt:   time.Now(),
					Context:      fmt.Sprintf(`{"baseline_mean": %.2f, "baseline_stddev": %.2f, "period_type": "%s"}`, baseline.Mean, baseline.StdDev, baseline.PeriodType),
					Tier1Pass:    true,
				}

				if err := e.datastore.CreateAnomalyCandidate(ctx, candidate); err != nil {
					e.log("ERROR: Failed to create anomaly candidate: %v", err)
				}
			}
		}
	}

	// Process tier 2 and tier 3 if enabled
	if e.config.Anomaly.Tier2.Enabled || e.config.Anomaly.Tier3.Enabled {
		e.processTier2And3(ctx)
	}
}

// processTier2And3 processes anomaly candidates through tier 2 and tier 3
func (e *Engine) processTier2And3(ctx context.Context) {
	candidates, err := e.datastore.GetUnprocessedAnomalyCandidates(ctx, 100)
	if err != nil {
		e.log("ERROR: Failed to get anomaly candidates: %v", err)
		return
	}

	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}

		var similarAnomalies []*database.SimilarAnomaly
		var embedding []float32

		// Tier 2: Embedding similarity
		if e.config.Anomaly.Tier2.Enabled && e.embeddingProvider != nil {
			embedding, similarAnomalies = e.processTier2(ctx, candidate)
		} else {
			// Skip Tier 2, pass through to Tier 3
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
		}

		// Tier 3: LLM classification (only if Tier 2 passed or was skipped)
		if e.config.Anomaly.Tier3.Enabled && e.reasoningProvider != nil &&
			(candidate.Tier2Pass == nil || *candidate.Tier2Pass) {
			e.processTier3(ctx, candidate, similarAnomalies)
		}

		// Determine final decision
		e.determineFinalDecision(candidate)

		// If final decision is alert, create an alert record
		if candidate.FinalDecision != nil && *candidate.FinalDecision == "alert" {
			e.createAnomalyAlert(ctx, candidate)
		}

		// Store embedding if we have one
		if len(embedding) > 0 {
			if err := e.datastore.StoreAnomalyEmbedding(ctx, candidate.ID, embedding, e.embeddingProvider.ModelName()); err != nil {
				e.debugLog("Failed to store embedding for candidate %d: %v", candidate.ID, err)
			}
		}

		// Mark as processed
		now := time.Now()
		candidate.ProcessedAt = &now

		if err := e.datastore.UpdateAnomalyCandidate(ctx, candidate); err != nil {
			e.log("ERROR: Failed to update anomaly candidate: %v", err)
		}
	}
}

// processTier2 handles Tier 2 embedding similarity processing
func (e *Engine) processTier2(ctx context.Context, candidate *database.AnomalyCandidate) ([]float32, []*database.SimilarAnomaly) {
	e.debugLog("Tier 2: Processing candidate %d for metric %s", candidate.ID, candidate.MetricName)

	// Build the context text for embedding
	contextText := e.buildContextText(candidate)

	// Generate embedding
	embedding, err := e.embeddingProvider.GenerateEmbedding(ctx, contextText)
	if err != nil {
		e.log("ERROR: Failed to generate embedding for candidate %d: %v", candidate.ID, err)
		// On embedding failure, pass through to Tier 3
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		return nil, nil
	}

	// Search for similar past anomalies
	threshold := e.config.Anomaly.Tier2.SimilarityThreshold
	if threshold <= 0 {
		threshold = 0.3 // Default minimum similarity
	}

	similarAnomalies, err := e.datastore.FindSimilarAnomalies(ctx, embedding, candidate.ID, threshold, 10)
	if err != nil {
		e.debugLog("Failed to find similar anomalies: %v", err)
		// On search failure, pass through to Tier 3
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		return embedding, nil
	}

	// Analyze similar anomalies
	if len(similarAnomalies) > 0 {
		// Find the highest similarity score
		var maxSimilarity float64
		var suppressCount, alertCount int

		for _, sa := range similarAnomalies {
			if sa.Similarity > maxSimilarity {
				maxSimilarity = sa.Similarity
			}
			if sa.FinalDecision != nil {
				switch *sa.FinalDecision {
				case "suppress", "suppressed", "false_positive":
					suppressCount++
				case "alert", "anomaly":
					alertCount++
				}
			}
		}

		candidate.Tier2Score = &maxSimilarity

		// Apply suppression logic based on similar anomalies
		suppressionThreshold := e.config.Anomaly.Tier2.SuppressionThreshold
		if suppressionThreshold <= 0 {
			suppressionThreshold = 0.85 // Default high similarity threshold for suppression
		}

		if maxSimilarity >= suppressionThreshold && suppressCount > alertCount {
			// High similarity to suppressed anomalies -> suppress this one too
			tier2Pass := false
			candidate.Tier2Pass = &tier2Pass
			e.debugLog("Tier 2: Suppressing candidate %d (similarity %.2f to %d suppressed anomalies)",
				candidate.ID, maxSimilarity, suppressCount)
		} else if maxSimilarity >= suppressionThreshold && alertCount > suppressCount {
			// High similarity to real anomalies -> this is likely a real issue
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
			e.debugLog("Tier 2: Passing candidate %d (similarity %.2f to %d alerted anomalies)",
				candidate.ID, maxSimilarity, alertCount)
		} else {
			// Low similarity or mixed results -> needs LLM review
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
			e.debugLog("Tier 2: Passing candidate %d to Tier 3 for review (similarity %.2f)",
				candidate.ID, maxSimilarity)
		}
	} else {
		// No similar anomalies found -> needs LLM review
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		score := 0.0
		candidate.Tier2Score = &score
		e.debugLog("Tier 2: No similar anomalies found for candidate %d, passing to Tier 3",
			candidate.ID)
	}

	return embedding, similarAnomalies
}

// processTier3 handles Tier 3 LLM classification
func (e *Engine) processTier3(ctx context.Context, candidate *database.AnomalyCandidate, similarAnomalies []*database.SimilarAnomaly) {
	e.debugLog("Tier 3: Processing candidate %d with LLM", candidate.ID)

	// Build the classification prompt
	prompt := e.buildClassificationPrompt(candidate, similarAnomalies)

	// Create a timeout context for Tier 3
	timeout := time.Duration(e.config.Anomaly.Tier3.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = DefaultTier3Timeout
	}
	tier3Ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Call the LLM for classification
	response, err := e.reasoningProvider.Classify(tier3Ctx, prompt)
	if err != nil {
		e.log("ERROR: Tier 3 LLM classification failed for candidate %d: %v", candidate.ID, err)
		errStr := err.Error()
		candidate.Tier3Error = &errStr
		// On LLM failure, default to alert (fail safe)
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		result := "LLM classification failed, defaulting to alert"
		candidate.Tier3Result = &result
		return
	}

	// Store the raw response
	candidate.Tier3Result = &response

	// Parse the LLM response
	decision, _ := e.parseLLMResponse(response)

	switch strings.ToLower(decision) {
	case "alert", "anomaly":
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		e.debugLog("Tier 3: LLM classified candidate %d as ALERT", candidate.ID)
	case "suppress", "suppressed", "false_positive":
		tier3Pass := false
		candidate.Tier3Pass = &tier3Pass
		e.debugLog("Tier 3: LLM classified candidate %d as SUPPRESS", candidate.ID)
	default:
		// Unknown response, default to alert
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		e.debugLog("Tier 3: Unknown LLM response for candidate %d, defaulting to alert", candidate.ID)
	}
}

// determineFinalDecision sets the final decision based on tier results
func (e *Engine) determineFinalDecision(candidate *database.AnomalyCandidate) {
	// If Tier 2 explicitly suppressed, suppress the anomaly
	if candidate.Tier2Pass != nil && !*candidate.Tier2Pass {
		decision := "suppress"
		candidate.FinalDecision = &decision
		return
	}

	// If Tier 3 was run, use its decision
	if candidate.Tier3Pass != nil {
		if *candidate.Tier3Pass {
			decision := "alert"
			candidate.FinalDecision = &decision
		} else {
			decision := "suppress"
			candidate.FinalDecision = &decision
		}
		return
	}

	// If only Tier 2 passed, treat as anomaly
	if candidate.Tier2Pass != nil && *candidate.Tier2Pass {
		decision := "alert"
		candidate.FinalDecision = &decision
		return
	}

	// Default to anomaly (Tier 1 already passed)
	decision := "alert"
	candidate.FinalDecision = &decision
}

// createAnomalyAlert creates an alert record for a confirmed anomaly candidate.
// It deduplicates against existing active anomaly alerts for the same metric
// and connection to prevent duplicate alerts.
func (e *Engine) createAnomalyAlert(ctx context.Context, candidate *database.AnomalyCandidate) {
	// Check for an existing active anomaly alert on this metric/connection
	existing, err := e.datastore.GetActiveAnomalyAlert(ctx, candidate.MetricName, candidate.ConnectionID, candidate.DatabaseName)
	if err == nil && existing != nil {
		e.debugLog("Active anomaly alert already exists for %s on connection %d (alert %d), skipping",
			candidate.MetricName, candidate.ConnectionID, existing.ID)
		candidate.AlertID = &existing.ID
		return
	}

	// Determine severity based on z-score magnitude
	absZScore := candidate.ZScore
	if absZScore < 0 {
		absZScore = -absZScore
	}
	severity := "warning"
	if absZScore >= 4.0 {
		severity = "critical"
	} else if absZScore >= 3.0 {
		severity = "high"
	}

	// Build anomaly details from tier results
	anomalyDetails := fmt.Sprintf(
		`{"z_score": %.2f, "baseline_context": %s, "tier2_score": %s, "tier3_result": %s}`,
		candidate.ZScore,
		candidate.Context,
		formatOptionalFloat(candidate.Tier2Score),
		formatOptionalString(candidate.Tier3Result),
	)

	title := fmt.Sprintf("Anomaly detected: %s on connection %d", candidate.MetricName, candidate.ConnectionID)
	description := fmt.Sprintf(
		"Statistical anomaly detected for metric %s (value: %.4f, z-score: %.2f).",
		candidate.MetricName, candidate.MetricValue, candidate.ZScore,
	)

	alert := &database.Alert{
		AlertType:      "anomaly",
		ConnectionID:   candidate.ConnectionID,
		DatabaseName:   candidate.DatabaseName,
		MetricName:     &candidate.MetricName,
		MetricValue:    &candidate.MetricValue,
		AnomalyScore:   &candidate.ZScore,
		AnomalyDetails: &anomalyDetails,
		Severity:       severity,
		Title:          title,
		Description:    description,
		Status:         "active",
		TriggeredAt:    time.Now(),
	}

	if err := e.datastore.CreateAlert(ctx, alert); err != nil {
		e.log("ERROR: Failed to create anomaly alert for candidate %d: %v", candidate.ID, err)
		return
	}

	candidate.AlertID = &alert.ID
	e.log("Anomaly alert created: %s (z-score: %.2f, severity: %s)", title, candidate.ZScore, severity)

	// Queue alert notification for async processing
	e.queueNotification(alert, database.NotificationTypeAlertFire)
}

// formatOptionalFloat formats a *float64 as a JSON value string.
func formatOptionalFloat(v *float64) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%.4f", *v)
}

// formatOptionalString formats a *string as a JSON-quoted value string.
func formatOptionalString(v *string) string {
	if v == nil {
		return "null"
	}
	// Use JSON marshaling to safely escape the string
	b, err := json.Marshal(*v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// buildContextText builds a text representation of the anomaly for embedding
func (e *Engine) buildContextText(candidate *database.AnomalyCandidate) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Metric: %s\n", candidate.MetricName))
	sb.WriteString(fmt.Sprintf("Value: %.4f\n", candidate.MetricValue))
	sb.WriteString(fmt.Sprintf("Z-Score: %.2f\n", candidate.ZScore))
	sb.WriteString(fmt.Sprintf("Connection ID: %d\n", candidate.ConnectionID))

	if candidate.DatabaseName != nil {
		sb.WriteString(fmt.Sprintf("Database: %s\n", *candidate.DatabaseName))
	}

	sb.WriteString(fmt.Sprintf("Detected at: %s\n", candidate.DetectedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Context: %s\n", candidate.Context))

	return sb.String()
}

// buildClassificationPrompt builds the prompt for LLM classification
func (e *Engine) buildClassificationPrompt(candidate *database.AnomalyCandidate, similarAnomalies []*database.SimilarAnomaly) string {
	var sb strings.Builder

	sb.WriteString("Analyze the following anomaly candidate and determine if it is a real issue that requires attention (alert) or a false positive that should be suppressed.\n\n")

	sb.WriteString("## Current Anomaly\n")
	sb.WriteString(fmt.Sprintf("- Metric: %s\n", candidate.MetricName))
	sb.WriteString(fmt.Sprintf("- Value: %.4f\n", candidate.MetricValue))
	sb.WriteString(fmt.Sprintf("- Z-Score: %.2f (standard deviations from mean)\n", candidate.ZScore))
	sb.WriteString(fmt.Sprintf("- Connection ID: %d\n", candidate.ConnectionID))

	if candidate.DatabaseName != nil {
		sb.WriteString(fmt.Sprintf("- Database: %s\n", *candidate.DatabaseName))
	}

	sb.WriteString(fmt.Sprintf("- Detected at: %s\n", candidate.DetectedAt.Format(time.RFC3339)))

	// Parse and include baseline info from context
	var contextData map[string]interface{}
	if err := json.Unmarshal([]byte(candidate.Context), &contextData); err == nil {
		if mean, ok := contextData["baseline_mean"].(float64); ok {
			sb.WriteString(fmt.Sprintf("- Baseline mean: %.4f\n", mean))
		}
		if stddev, ok := contextData["baseline_stddev"].(float64); ok {
			sb.WriteString(fmt.Sprintf("- Baseline stddev: %.4f\n", stddev))
		}
		if periodType, ok := contextData["period_type"].(string); ok {
			sb.WriteString(fmt.Sprintf("- Baseline period: %s\n", periodType))
		}
	}

	// Include similar past anomalies if available
	if len(similarAnomalies) > 0 {
		sb.WriteString("\n## Similar Past Anomalies\n")
		for i, sa := range similarAnomalies {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("... and %d more similar anomalies\n", len(similarAnomalies)-5))
				break
			}
			decision := "unknown"
			if sa.FinalDecision != nil {
				decision = *sa.FinalDecision
			}
			sb.WriteString(fmt.Sprintf("- Similarity: %.2f%%, Decision: %s, Metric: %s\n",
				sa.Similarity*100, decision, sa.MetricName))
		}
	} else {
		sb.WriteString("\n## Similar Past Anomalies\nNo similar past anomalies found.\n")
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Based on the above information, respond with a JSON object containing:\n")
	sb.WriteString("- \"decision\": either \"alert\" (real issue) or \"suppress\" (false positive)\n")
	sb.WriteString("- \"confidence\": a number from 0 to 1\n")
	sb.WriteString("- \"reasoning\": a brief explanation\n")

	return sb.String()
}

// parseLLMResponse parses the LLM response to extract the decision
func (e *Engine) parseLLMResponse(response string) (string, float64) {
	// Try to parse as JSON first
	var result struct {
		Decision   string  `json:"decision"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(response), &result); err == nil {
		return result.Decision, result.Confidence
	}

	// Fall back to text parsing
	lowerResponse := strings.ToLower(response)

	// Look for explicit decision keywords
	if strings.Contains(lowerResponse, "\"decision\"") || strings.Contains(lowerResponse, "'decision'") {
		// Try to find decision value
		if strings.Contains(lowerResponse, "\"alert\"") || strings.Contains(lowerResponse, "'alert'") {
			return "alert", 0.5
		}
		if strings.Contains(lowerResponse, "\"suppress\"") || strings.Contains(lowerResponse, "'suppress'") {
			return "suppress", 0.5
		}
	}

	// Look for decision keywords in natural language
	if strings.Contains(lowerResponse, "should be suppressed") ||
		strings.Contains(lowerResponse, "false positive") ||
		strings.Contains(lowerResponse, "not a real issue") ||
		strings.Contains(lowerResponse, "normal behavior") {
		return "suppress", 0.5
	}

	if strings.Contains(lowerResponse, "real issue") ||
		strings.Contains(lowerResponse, "should alert") ||
		strings.Contains(lowerResponse, "requires attention") ||
		strings.Contains(lowerResponse, "genuine anomaly") {
		return "alert", 0.5
	}

	// Default to alert if we can't parse the response
	return "alert", 0.3
}
