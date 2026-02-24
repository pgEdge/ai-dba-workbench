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

// Helper functions for creating pointer values in test data
func strPtr(s string) *string        { return &s }
func float64Ptr(f float64) *float64  { return &f }
func timePtr(t time.Time) *time.Time { return &t }

// TestBuildReevaluationPrompt tests the buildReevaluationPrompt method
// with fully populated alert data, historical acknowledgements, and
// connection alerts.
func TestBuildReevaluationPrompt(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)
	ackTime := now.Add(-1 * time.Hour)

	alert := &database.AcknowledgedAnomalyAlert{
		ID:                42,
		ConnectionID:      7,
		Title:             "High CPU usage anomaly",
		Severity:          "warning",
		MetricName:        strPtr("cpu_usage_percent"),
		MetricValue:       float64Ptr(95.1234),
		ZScore:            float64Ptr(3.75),
		AnomalyDetails:    strPtr(`{"z_score": 3.75, "baseline_context": {"baseline_mean": 45.0, "baseline_stddev": 12.5}, "tier3_result": "Confirmed anomaly"}`),
		TriggeredAt:       now,
		AckMessage:        strPtr("Intentional load test"),
		FalsePositive:     true,
		AcknowledgedBy:    strPtr("admin_user"),
		AcknowledgedAt:    timePtr(ackTime),
		ReevaluationCount: 2,
	}

	historicalAcks := []*database.AcknowledgedAnomalyAlert{
		{
			ID:             30,
			AckMessage:     strPtr("Also a load test"),
			FalsePositive:  true,
			AcknowledgedBy: strPtr("admin_user"),
		},
		{
			ID:             25,
			AckMessage:     strPtr("Genuine spike"),
			FalsePositive:  false,
			AcknowledgedBy: strPtr("dba_user"),
		},
		{
			ID:            20,
			AckMessage:    nil,
			FalsePositive: false,
		},
	}

	connectionAlerts := []*database.Alert{
		{
			ID:          42,
			Title:       "High CPU usage anomaly",
			Severity:    "warning",
			AlertType:   "anomaly",
			MetricName:  strPtr("cpu_usage_percent"),
			TriggeredAt: now,
		},
		{
			ID:          50,
			Title:       "Replication lag detected",
			Severity:    "critical",
			AlertType:   "threshold",
			MetricName:  strPtr("replication_lag_seconds"),
			TriggeredAt: now.Add(-30 * time.Minute),
		},
		{
			ID:          55,
			Title:       "Connection pool exhaustion",
			Severity:    "warning",
			AlertType:   "anomaly",
			MetricName:  nil,
			TriggeredAt: now.Add(-10 * time.Minute),
		},
	}

	clusterPeers := []*database.ClusterPeerInfo{
		{ConnectionID: 8, ConnectionName: "pg15-primary", NodeRole: "binary_primary"},
		{ConnectionID: 9, ConnectionName: "pg15-replica", NodeRole: "binary_standby"},
	}

	clusterAlerts := []*database.Alert{
		{
			ID:           100,
			Title:        "High replication lag on primary",
			Severity:     "critical",
			ConnectionID: 8,
			TriggeredAt:  now.Add(-5 * time.Minute),
		},
	}

	prompt := engine.buildReevaluationPrompt(alert, historicalAcks, connectionAlerts, clusterPeers, clusterAlerts)

	// Verify the prompt contains expected alert details
	checks := []struct {
		label    string
		fragment string
	}{
		{"metric name", "cpu_usage_percent"},
		{"z-score value", "3.75"},
		{"severity", "warning"},
		{"ack message", "Intentional load test"},
		{"false positive indicator", "false positive: true"},
		{"acknowledged by", "admin_user"},
		{"historical ack message", "Also a load test"},
		{"historical ack user", "dba_user"},
		{"historical no message", "(none)"},
		{"connection alert title", "Replication lag detected"},
		{"connection alert title 2", "Connection pool exhaustion"},
		{"JSON clear instruction", "\"clear\""},
		{"JSON keep instruction", "\"keep\""},
		{"confidence instruction", "\"confidence\""},
		{"baseline mean", "45.0000"},
		{"baseline stddev", "12.5000"},
		{"tier3 result", "Confirmed anomaly"},
		{"re-evaluation count", "Re-evaluation count: 2"},
		{"cluster context heading", "Cluster Context"},
		{"cluster peer name", "pg15-primary"},
		{"cluster peer role", "binary_primary"},
		{"cluster peer replica", "pg15-replica"},
		{"cluster alert title", "High replication lag on primary"},
	}

	for _, c := range checks {
		if !strings.Contains(prompt, c.fragment) {
			t.Errorf("prompt missing %s (expected fragment %q)", c.label, c.fragment)
		}
	}

	t.Run("nil optional fields", func(t *testing.T) {
		minAlert := &database.AcknowledgedAnomalyAlert{
			ID:                1,
			ConnectionID:      1,
			Title:             "Test alert",
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

		result := engine.buildReevaluationPrompt(minAlert, nil, nil, nil, nil)

		if result == "" {
			t.Error("prompt should not be empty for minimal alert")
		}

		if !strings.Contains(result, "(none provided)") {
			t.Error("prompt should indicate no message was provided")
		}

		if !strings.Contains(result, "No historical acknowledgements") {
			t.Error("prompt should indicate no historical acknowledgements")
		}

		if !strings.Contains(result, "No other alerts on this server") {
			t.Error("prompt should indicate no other alerts")
		}

		if strings.Contains(result, "Cluster Context") {
			t.Error("prompt should not contain cluster context for nil peers")
		}
	})

	t.Run("with cluster context", func(t *testing.T) {
		peers := []*database.ClusterPeerInfo{
			{ConnectionID: 10, ConnectionName: "node-a", NodeRole: "binary_primary"},
			{ConnectionID: 11, ConnectionName: "node-b", NodeRole: "binary_standby"},
		}
		peerAlerts := []*database.Alert{
			{
				ID:           200,
				Title:        "Disk space low",
				Severity:     "warning",
				ConnectionID: 10,
				TriggeredAt:  now.Add(-2 * time.Minute),
			},
			{
				ID:           201,
				Title:        "High memory usage",
				Severity:     "high",
				ConnectionID: 11,
				TriggeredAt:  now.Add(-1 * time.Minute),
			},
		}

		result := engine.buildReevaluationPrompt(alert, nil, nil, peers, peerAlerts)

		clusterChecks := []struct {
			label    string
			fragment string
		}{
			{"cluster heading", "Cluster Context"},
			{"peer count", "2 other node(s)"},
			{"peer name a", "node-a"},
			{"peer name b", "node-b"},
			{"peer role", "binary_primary"},
			{"cluster alert title", "Disk space low"},
			{"cluster alert server", "node-a"},
			{"cluster alert title 2", "High memory usage"},
			{"cluster alert server 2", "node-b"},
		}

		for _, c := range clusterChecks {
			if !strings.Contains(result, c.fragment) {
				t.Errorf("prompt missing %s (expected fragment %q)", c.label, c.fragment)
			}
		}
	})
}

// TestParseReevaluationResponse tests the parseReevaluationResponse
// function with valid JSON, text fallbacks, and edge cases.
func TestParseReevaluationResponse(t *testing.T) {
	tests := []struct {
		name               string
		response           string
		expectedDecision   string
		expectedConfidence float64
		confidenceTol      float64
	}{
		{
			name:               "valid JSON clear",
			response:           `{"decision": "clear", "confidence": 0.9, "reasoning": "User confirmed false positive"}`,
			expectedDecision:   "clear",
			expectedConfidence: 0.9,
			confidenceTol:      0.001,
		},
		{
			name:               "valid JSON keep",
			response:           `{"decision": "keep", "confidence": 0.8, "reasoning": "Anomaly still valid"}`,
			expectedDecision:   "keep",
			expectedConfidence: 0.8,
			confidenceTol:      0.001,
		},
		{
			name:               "text fallback clear",
			response:           "Based on the user feedback, this alert should be cleared",
			expectedDecision:   "clear",
			expectedConfidence: 0.5,
			confidenceTol:      0.001,
		},
		{
			name:               "text fallback keep",
			response:           "The anomaly appears genuine and should be kept active",
			expectedDecision:   "keep",
			expectedConfidence: 0.5,
			confidenceTol:      0.001,
		},
		{
			name:               "false positive text",
			response:           "The user marked this as a false positive, recommend clearing",
			expectedDecision:   "clear",
			expectedConfidence: 0.5,
			confidenceTol:      0.001,
		},
		{
			name:               "unparseable input defaults to keep",
			response:           "I'm not sure what to do",
			expectedDecision:   "keep",
			expectedConfidence: 0.3,
			confidenceTol:      0.001,
		},
		{
			name:               "empty string defaults to keep",
			response:           "",
			expectedDecision:   "keep",
			expectedConfidence: 0.3,
			confidenceTol:      0.001,
		},
		{
			name:               "JSON with extra fields",
			response:           `{"decision": "clear", "confidence": 0.95, "reasoning": "False positive confirmed", "extra": "field"}`,
			expectedDecision:   "clear",
			expectedConfidence: 0.95,
			confidenceTol:      0.001,
		},
		{
			name:               "text with quoted clear keyword",
			response:           `The analysis suggests "clear" is appropriate`,
			expectedDecision:   "clear",
			expectedConfidence: 0.5,
			confidenceTol:      0.001,
		},
		{
			name:               "text with quoted keep keyword",
			response:           `I recommend "keep" for this alert`,
			expectedDecision:   "keep",
			expectedConfidence: 0.5,
			confidenceTol:      0.001,
		},
		{
			name:               "text safe to clear",
			response:           "This anomaly is safe to clear based on feedback",
			expectedDecision:   "clear",
			expectedConfidence: 0.5,
			confidenceTol:      0.001,
		},
		{
			name:               "text remain active",
			response:           "The alert should remain active for monitoring",
			expectedDecision:   "keep",
			expectedConfidence: 0.5,
			confidenceTol:      0.001,
		},
		{
			name:               "JSON with invalid decision falls back to text",
			response:           `{"decision": "maybe", "confidence": 0.7, "reasoning": "unclear"}`,
			expectedDecision:   "keep",
			expectedConfidence: 0.3,
			confidenceTol:      0.001,
		},
		{
			name:               "JSON with uppercase decision",
			response:           `{"decision": "CLEAR", "confidence": 0.85, "reasoning": "confirmed"}`,
			expectedDecision:   "clear",
			expectedConfidence: 0.85,
			confidenceTol:      0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, confidence := parseReevaluationResponse(tt.response)

			if decision != tt.expectedDecision {
				t.Errorf("decision = %q, expected %q", decision, tt.expectedDecision)
			}

			diff := confidence - tt.expectedConfidence
			if diff < 0 {
				diff = -diff
			}
			if diff > tt.confidenceTol {
				t.Errorf("confidence = %v, expected %v (tolerance %v)",
					confidence, tt.expectedConfidence, tt.confidenceTol)
			}
		})
	}
}
