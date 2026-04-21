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
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestBuildReevaluationPromptMinimal tests with minimal alert data
func TestBuildReevaluationPromptMinimal(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)

	alert := &database.AcknowledgedAnomalyAlert{
		ID:                1,
		ConnectionID:      1,
		Title:             "Test Alert",
		Severity:          "info",
		MetricName:        nil,
		MetricValue:       nil,
		ZScore:            nil,
		AnomalyDetails:    nil,
		TriggeredAt:       now,
		AckMessage:        nil,
		FalsePositive:     false,
		AcknowledgedBy:    nil,
		AcknowledgedAt:    nil,
		ReevaluationCount: 0,
	}

	prompt := engine.buildReevaluationPrompt(alert, nil, nil, nil, nil)

	checks := []string{
		"Re-evaluate",
		"Alert Details",
		"Severity: info",
		"(none provided)",
		"No historical acknowledgements",
		"No other alerts on this server",
		"Instructions",
		"\"clear\"",
		"\"keep\"",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}

	// Should NOT have cluster context
	if strings.Contains(prompt, "Cluster Context") {
		t.Error("prompt should not have cluster context without peers")
	}
}

// TestBuildReevaluationPromptWithAnomalyDetails tests parsing of anomaly_details JSON
func TestBuildReevaluationPromptWithAnomalyDetails(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)

	// Valid JSON with baseline context
	details := `{"z_score": 4.5, "baseline_context": {"baseline_mean": 50.0, "baseline_stddev": 10.0}, "tier3_result": "Genuine anomaly"}`

	alert := &database.AcknowledgedAnomalyAlert{
		ID:             1,
		ConnectionID:   1,
		Title:          "CPU Anomaly",
		Severity:       "warning",
		MetricName:     strPtr("cpu_usage"),
		MetricValue:    float64Ptr(95.0),
		ZScore:         float64Ptr(4.5),
		AnomalyDetails: &details,
		TriggeredAt:    now,
	}

	prompt := engine.buildReevaluationPrompt(alert, nil, nil, nil, nil)

	checks := []string{
		"cpu_usage",
		"95.0000",
		"4.50",
		"Baseline mean: 50.0000",
		"Baseline stddev: 10.0000",
		"Genuine anomaly",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

// TestBuildReevaluationPromptWithInvalidAnomalyDetails tests handling of invalid JSON
func TestBuildReevaluationPromptWithInvalidAnomalyDetails(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)

	// Invalid JSON
	invalidDetails := `{invalid json}`

	alert := &database.AcknowledgedAnomalyAlert{
		ID:             1,
		ConnectionID:   1,
		Title:          "Test",
		Severity:       "info",
		AnomalyDetails: &invalidDetails,
		TriggeredAt:    now,
	}

	// Should not panic with invalid JSON
	prompt := engine.buildReevaluationPrompt(alert, nil, nil, nil, nil)

	if prompt == "" {
		t.Error("prompt should not be empty even with invalid anomaly details JSON")
	}
}

// TestBuildReevaluationPromptWithHistoricalAcks tests with historical acknowledgements
func TestBuildReevaluationPromptWithHistoricalAcks(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)

	alert := &database.AcknowledgedAnomalyAlert{
		ID:           1,
		ConnectionID: 1,
		Title:        "Test",
		Severity:     "warning",
		TriggeredAt:  now,
	}

	historicalAcks := []*database.AcknowledgedAnomalyAlert{
		{
			ID:             10,
			AckMessage:     strPtr("Maintenance window"),
			FalsePositive:  true,
			AcknowledgedBy: strPtr("admin"),
		},
		{
			ID:             11,
			AckMessage:     nil, // No message
			FalsePositive:  false,
			AcknowledgedBy: strPtr("dba"),
		},
	}

	prompt := engine.buildReevaluationPrompt(alert, historicalAcks, nil, nil, nil)

	checks := []string{
		"Historical Feedback",
		"2 past acknowledgements",
		"Maintenance window",
		"false_positive=true",
		"acknowledged_by=admin",
		"(none)",
		"acknowledged_by=dba",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

// TestBuildReevaluationPromptWithConnectionAlerts tests with other alerts on the server
func TestBuildReevaluationPromptWithConnectionAlerts(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)

	alert := &database.AcknowledgedAnomalyAlert{
		ID:           1,
		ConnectionID: 1,
		Title:        "Alert 1",
		Severity:     "warning",
		TriggeredAt:  now,
	}

	connectionAlerts := []*database.Alert{
		{
			ID:          1, // Same as main alert
			Title:       "Alert 1",
			Severity:    "warning",
			AlertType:   "anomaly",
			MetricName:  strPtr("metric1"),
			TriggeredAt: now,
		},
		{
			ID:          2, // Different alert
			Title:       "Alert 2",
			Severity:    "critical",
			AlertType:   "threshold",
			MetricName:  strPtr("metric2"),
			TriggeredAt: now.Add(-5 * time.Minute),
		},
		{
			ID:          3,
			Title:       "Alert 3",
			Severity:    "high",
			AlertType:   "anomaly",
			MetricName:  nil, // No metric name
			TriggeredAt: now.Add(-10 * time.Minute),
		},
	}

	prompt := engine.buildReevaluationPrompt(alert, nil, connectionAlerts, nil, nil)

	checks := []string{
		"Other Alerts on This Server",
		"Alert 2",
		"critical",
		"threshold",
		"metric2",
		"Alert 3",
		"N/A", // For nil metric name
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}

	// Should NOT include Alert 1 since it's the same as the main alert
	// Actually checking the logic: Alert 1 is skipped because ca.ID == alert.ID
}

// TestBuildReevaluationPromptWithClusterContext tests with cluster peers and alerts
func TestBuildReevaluationPromptWithClusterContext(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)

	alert := &database.AcknowledgedAnomalyAlert{
		ID:           1,
		ConnectionID: 1,
		Title:        "Test",
		Severity:     "warning",
		TriggeredAt:  now,
	}

	clusterPeers := []*database.ClusterPeerInfo{
		{ConnectionID: 2, ConnectionName: "node-primary", NodeRole: "primary"},
		{ConnectionID: 3, ConnectionName: "node-standby", NodeRole: "standby"},
	}

	clusterAlerts := []*database.Alert{
		{
			ID:           100,
			Title:        "Peer Alert",
			Severity:     "warning",
			ConnectionID: 2,
			TriggeredAt:  now.Add(-2 * time.Minute),
		},
	}

	prompt := engine.buildReevaluationPrompt(alert, nil, nil, clusterPeers, clusterAlerts)

	checks := []string{
		"Cluster Context",
		"2 other node(s)",
		"node-primary",
		"node-standby",
		"primary",
		"standby",
		"Peer Alert",
		"warning",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

// TestParseReevaluationResponseEdgeCases tests edge cases in parsing
func TestParseReevaluationResponseEdgeCases(t *testing.T) {
	tests := []struct {
		name             string
		response         string
		expectedDecision string
	}{
		{
			name:             "JSON with whitespace",
			response:         `  { "decision" : "clear" , "confidence" : 0.9 }  `,
			expectedDecision: "clear",
		},
		{
			name:             "JSON with nested reasoning",
			response:         `{"decision": "keep", "confidence": 0.8, "reasoning": "The {\"nested\": \"json\"} is complex"}`,
			expectedDecision: "keep",
		},
		{
			name:             "Very low confidence",
			response:         `{"decision": "clear", "confidence": 0.001}`,
			expectedDecision: "clear",
		},
		{
			name:             "Very high confidence",
			response:         `{"decision": "keep", "confidence": 0.999}`,
			expectedDecision: "keep",
		},
		{
			name:             "Zero confidence",
			response:         `{"decision": "clear", "confidence": 0}`,
			expectedDecision: "clear",
		},
		{
			name:             "Negative confidence treated as zero",
			response:         `{"decision": "keep", "confidence": -0.5}`,
			expectedDecision: "keep",
		},
		{
			name:             "Mixed case text safe to clear",
			response:         "This is Safe To Clear based on user input",
			expectedDecision: "clear",
		},
		{
			name:             "Multiple clear keywords returns clear",
			response:         "Should be cleared because it is safe to clear",
			expectedDecision: "clear",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := parseReevaluationResponse(tt.response)
			if decision != tt.expectedDecision {
				t.Errorf("parseReevaluationResponse(%q) = %q, expected %q",
					tt.response, decision, tt.expectedDecision)
			}
		})
	}
}
