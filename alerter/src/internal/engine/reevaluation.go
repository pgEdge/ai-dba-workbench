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
	"github.com/pgedge/ai-workbench/pkg/worker"
)

// reevaluateAcknowledgedAlerts reviews acknowledged anomaly alerts using LLM
// reasoning to decide whether they should be cleared based on user feedback,
// historical patterns, and server context.
func (e *Engine) reevaluateAcknowledgedAlerts(ctx context.Context) {
	e.debugLog("Running re-evaluation of acknowledged anomaly alerts...")

	cfg := e.getConfig()
	if !cfg.Anomaly.Reevaluation.Enabled {
		return
	}

	alerts, err := e.datastore.GetAcknowledgedAnomalyAlerts(
		ctx,
		cfg.Anomaly.Reevaluation.IntervalSeconds,
		cfg.Anomaly.Reevaluation.MaxPerCycle,
	)
	if err != nil {
		e.log("ERROR: Failed to get acknowledged anomaly alerts: %v", err)
		return
	}

	if len(alerts) == 0 {
		e.debugLog("No acknowledged anomaly alerts due for re-evaluation")
		return
	}

	e.debugLog("Re-evaluating %d acknowledged anomaly alerts", len(alerts))

	for _, alert := range alerts {
		if ctx.Err() != nil {
			return
		}

		// Skip alerts without a metric name; re-evaluation requires
		// metric context for meaningful LLM analysis.
		if alert.MetricName == nil {
			e.debugLog("Skipping re-evaluation for alert %d: no metric name", alert.ID)
			if err := e.datastore.UpdateAlertReevaluation(ctx, alert.ID); err != nil {
				e.log("ERROR: Failed to update re-evaluation for alert %d: %v", alert.ID, err)
			}
			continue
		}

		// Fetch historical acknowledgements for the same metric and connection
		historicalAcks, err := e.datastore.GetAcknowledgmentHistoryForMetric(
			ctx, *alert.MetricName, alert.ConnectionID, alert.ID, 10,
		)
		if err != nil {
			e.log("ERROR: Failed to get acknowledgment history for alert %d: %v", alert.ID, err)
			historicalAcks = nil
		}

		// Fetch all active/acknowledged alerts on the same connection
		connectionAlerts, err := e.datastore.GetAlertsByConnection(ctx, alert.ConnectionID)
		if err != nil {
			e.log("ERROR: Failed to get connection alerts for alert %d: %v", alert.ID, err)
			connectionAlerts = nil
		}

		// Fetch cluster context for the LLM prompt
		clusterPeers, err := e.datastore.GetClusterPeers(ctx, alert.ConnectionID)
		if err != nil {
			e.log("ERROR: Failed to get cluster peers for alert %d: %v", alert.ID, err)
			clusterPeers = nil
		}
		clusterAlerts, err := e.datastore.GetAlertsByCluster(ctx, alert.ConnectionID)
		if err != nil {
			e.log("ERROR: Failed to get cluster alerts for alert %d: %v", alert.ID, err)
			clusterAlerts = nil
		}

		// Build the LLM prompt
		prompt := e.buildReevaluationPrompt(alert, historicalAcks, connectionAlerts, clusterPeers, clusterAlerts)

		// Create a timeout context for the LLM call
		timeout := time.Duration(cfg.Anomaly.Reevaluation.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = DefaultTier3Timeout
		}
		tier3Ctx, cancel := context.WithTimeout(ctx, timeout)

		// Call the reasoning provider
		response, err := e.reasoningProvider.Classify(tier3Ctx, prompt)
		cancel()

		if err != nil {
			e.log("ERROR: Re-evaluation LLM call failed for alert %d: %v", alert.ID, err)
			if err := e.datastore.UpdateAlertReevaluation(ctx, alert.ID); err != nil {
				e.log("ERROR: Failed to update re-evaluation for alert %d: %v", alert.ID, err)
			}
			continue
		}

		// Parse the LLM response
		decision, confidence := parseReevaluationResponse(response)

		if decision == "clear" {
			if err := e.datastore.ClearAlert(ctx, alert.ID); err != nil {
				e.log("ERROR: Failed to clear alert %d during re-evaluation: %v", alert.ID, err)
			} else {
				// Fetch the updated alert to include cleared_at in the notification
				updatedAlert, err := e.datastore.GetAlert(ctx, alert.ID)
				if err != nil {
					e.log("ERROR: Failed to fetch updated alert %d after clearing: %v", alert.ID, err)
				} else {
					e.queueNotification(updatedAlert, database.NotificationTypeAlertClear)
				}
				e.log("Re-evaluation cleared alert %d (%s, confidence: %.2f)",
					alert.ID, *alert.MetricName, confidence)
			}
		} else {
			e.debugLog("Re-evaluation keeping alert %d (%s, decision: %s, confidence: %.2f)",
				alert.ID, *alert.MetricName, decision, confidence)
		}

		// Always update the re-evaluation tracking regardless of outcome
		if err := e.datastore.UpdateAlertReevaluation(ctx, alert.ID); err != nil {
			e.log("ERROR: Failed to update re-evaluation for alert %d: %v", alert.ID, err)
		}
	}
}

// buildReevaluationPrompt builds an LLM prompt for re-evaluating an
// acknowledged anomaly alert, incorporating user feedback, historical
// patterns, and server context.
func (e *Engine) buildReevaluationPrompt(
	alert *database.AcknowledgedAnomalyAlert,
	historicalAcks []*database.AcknowledgedAnomalyAlert,
	connectionAlerts []*database.Alert,
	clusterPeers []*database.ClusterPeerInfo,
	clusterAlerts []*database.Alert,
) string {
	var sb strings.Builder

	sb.WriteString("Re-evaluate the following acknowledged anomaly alert and decide whether it should be cleared or kept active based on user feedback and context.\n\n")

	// Section 1: Alert Details
	sb.WriteString("## Alert Details\n")
	if alert.MetricName != nil {
		fmt.Fprintf(&sb, "- Metric: %s\n", *alert.MetricName)
	}
	if alert.MetricValue != nil {
		fmt.Fprintf(&sb, "- Value: %.4f\n", *alert.MetricValue)
	}
	if alert.ZScore != nil {
		fmt.Fprintf(&sb, "- Z-Score: %.2f\n", *alert.ZScore)
	}
	fmt.Fprintf(&sb, "- Severity: %s\n", alert.Severity)
	fmt.Fprintf(&sb, "- Triggered at: %s\n", alert.TriggeredAt.Format(time.RFC3339))

	// Parse anomaly_details JSON for readable context
	if alert.AnomalyDetails != nil {
		var details map[string]any
		if err := json.Unmarshal([]byte(*alert.AnomalyDetails), &details); err == nil {
			if zScore, ok := details["z_score"].(float64); ok {
				fmt.Fprintf(&sb, "- Anomaly z-score: %.2f\n", zScore)
			}
			if baselineCtx, ok := details["baseline_context"].(map[string]any); ok {
				if mean, ok := baselineCtx["baseline_mean"].(float64); ok {
					fmt.Fprintf(&sb, "- Baseline mean: %.4f\n", mean)
				}
				if stddev, ok := baselineCtx["baseline_stddev"].(float64); ok {
					fmt.Fprintf(&sb, "- Baseline stddev: %.4f\n", stddev)
				}
			}
			if tier3, ok := details["tier3_result"].(string); ok && tier3 != "" {
				fmt.Fprintf(&sb, "- Tier 3 analysis: %s\n", tier3)
			}
		}
	}

	// Section 2: User Feedback on This Alert
	sb.WriteString("\n## User Feedback on This Alert\n")
	if alert.AckMessage != nil && *alert.AckMessage != "" {
		fmt.Fprintf(&sb, "- Message: %s\n", *alert.AckMessage)
	} else {
		sb.WriteString("- Message: (none provided)\n")
	}
	fmt.Fprintf(&sb, "- Marked as false positive: %t\n", alert.FalsePositive)
	if alert.AcknowledgedBy != nil {
		fmt.Fprintf(&sb, "- Acknowledged by: %s\n", *alert.AcknowledgedBy)
	}
	if alert.AcknowledgedAt != nil {
		fmt.Fprintf(&sb, "- Acknowledged at: %s\n", alert.AcknowledgedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&sb, "- Re-evaluation count: %d\n", alert.ReevaluationCount)

	// Section 3: Historical Feedback
	sb.WriteString("\n## Historical Feedback for This Metric\n")
	if len(historicalAcks) > 0 {
		fmt.Fprintf(&sb, "Found %d past acknowledgements for the same metric on this server:\n", len(historicalAcks))
		for _, ha := range historicalAcks {
			msg := "(none)"
			if ha.AckMessage != nil && *ha.AckMessage != "" {
				msg = *ha.AckMessage
			}
			ackBy := "unknown"
			if ha.AcknowledgedBy != nil {
				ackBy = *ha.AcknowledgedBy
			}
			fmt.Fprintf(&sb, "- Alert %d: message=%q, false_positive=%t, acknowledged_by=%s\n",
				ha.ID, msg, ha.FalsePositive, ackBy)
		}
	} else {
		sb.WriteString("No historical acknowledgements found for this metric on this server.\n")
	}

	// Section 4: Other Alerts on This Server
	sb.WriteString("\n## Other Alerts on This Server\n")
	if len(connectionAlerts) > 0 {
		otherCount := 0
		for _, ca := range connectionAlerts {
			if ca.ID == alert.ID {
				continue
			}
			otherCount++
			metricName := "N/A"
			if ca.MetricName != nil {
				metricName = *ca.MetricName
			}
			fmt.Fprintf(&sb, "- %s (severity: %s, type: %s, metric: %s, triggered: %s)\n",
				ca.Title, ca.Severity, ca.AlertType, metricName,
				ca.TriggeredAt.Format(time.RFC3339))
		}
		if otherCount == 0 {
			sb.WriteString("No other alerts on this server.\n")
		}
	} else {
		sb.WriteString("No other alerts on this server.\n")
	}

	// Section 5: Cluster Context
	writeClusterContext(&sb, alert.ConnectionID, clusterPeers, clusterAlerts)

	// Section 6: Instructions
	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Based on the user feedback, historical patterns, and server context, decide whether this alert should be cleared or kept active.\n\n")
	sb.WriteString("Consider the following guidelines:\n")
	sb.WriteString("- If the user marked it as a false positive, strongly consider clearing.\n")
	sb.WriteString("- If there is a pattern of similar acknowledgements across alert instances, consider clearing.\n")
	sb.WriteString("- If the user's note explains the anomaly (e.g., \"intentional workload increase\"), consider clearing.\n")
	sb.WriteString("- Default to \"keep\" if uncertain.\n\n")
	sb.WriteString("Respond with a JSON object containing:\n")
	sb.WriteString("- \"decision\": either \"clear\" (safe to remove) or \"keep\" (should remain active)\n")
	sb.WriteString("- \"confidence\": a number from 0.0 to 1.0\n")
	sb.WriteString("- \"reasoning\": a brief explanation of your decision\n")

	return sb.String()
}

// parseReevaluationResponse parses the LLM response for a re-evaluation
// decision. It returns the decision ("clear" or "keep") and a confidence
// score. This is a package-level function for testability.
func parseReevaluationResponse(response string) (string, float64) {
	return parseLLMDecision(response, reevaluationDecisionConfig)
}

// runReevaluationWorker periodically re-evaluates acknowledged anomaly
// alerts using LLM reasoning to determine whether they should be cleared.
func (e *Engine) runReevaluationWorker(ctx context.Context) {
	task := worker.NewDynamicPeriodicTask(
		func() time.Duration {
			return time.Duration(e.getConfig().Anomaly.Reevaluation.IntervalSeconds) * time.Second
		},
		e.reevaluateAcknowledgedAlerts,
		worker.WithName("Re-evaluation worker"),
		worker.WithLogFunc(e.log),
	)
	task.Start(ctx)
	<-ctx.Done()
	task.Stop()
}
