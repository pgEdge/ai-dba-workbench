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

// TestFormatOptionalFloat tests the formatOptionalFloat helper
func TestFormatOptionalFloat(t *testing.T) {
	tests := []struct {
		name     string
		input    *float64
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "null",
		},
		{
			name:     "zero value",
			input:    float64Ptr(0.0),
			expected: "0.0000",
		},
		{
			name:     "positive value",
			input:    float64Ptr(3.14159),
			expected: "3.1416",
		},
		{
			name:     "negative value",
			input:    float64Ptr(-2.5),
			expected: "-2.5000",
		},
		{
			name:     "large value",
			input:    float64Ptr(12345.6789),
			expected: "12345.6789",
		},
		{
			name:     "small value",
			input:    float64Ptr(0.00001),
			expected: "0.0000",
		},
		{
			name:     "integer like value",
			input:    float64Ptr(100.0),
			expected: "100.0000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatOptionalFloat(tt.input)
			if result != tt.expected {
				t.Errorf("formatOptionalFloat(%v) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestFormatOptionalString tests the formatOptionalString helper
func TestFormatOptionalString(t *testing.T) {
	tests := []struct {
		name     string
		input    *string
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "null",
		},
		{
			name:     "empty string",
			input:    strPtr(""),
			expected: `""`,
		},
		{
			name:     "simple string",
			input:    strPtr("hello"),
			expected: `"hello"`,
		},
		{
			name:     "string with quotes",
			input:    strPtr(`He said "hello"`),
			expected: `"He said \"hello\""`,
		},
		{
			name:     "string with newlines",
			input:    strPtr("line1\nline2"),
			expected: `"line1\nline2"`,
		},
		{
			name:     "string with backslashes",
			input:    strPtr(`path\to\file`),
			expected: `"path\\to\\file"`,
		},
		{
			name:     "string with unicode",
			input:    strPtr("hello \u2764"),
			expected: `"hello ❤"`,
		},
		{
			name:     "JSON-like string",
			input:    strPtr(`{"key": "value"}`),
			expected: `"{\"key\": \"value\"}"`,
		},
		{
			name:     "string with tabs",
			input:    strPtr("col1\tcol2"),
			expected: `"col1\tcol2"`,
		},
		{
			name:     "long string",
			input:    strPtr("This is a longer string with multiple words and some punctuation, including commas and periods."),
			expected: `"This is a longer string with multiple words and some punctuation, including commas and periods."`,
		},
		{
			name:     "string with numbers",
			input:    strPtr("value is 42.5"),
			expected: `"value is 42.5"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatOptionalString(tt.input)
			if result != tt.expected {
				t.Errorf("formatOptionalString(%v) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestBuildContextText tests the buildContextText method
func TestBuildContextText(t *testing.T) {
	engine := &Engine{}

	t.Run("basic candidate", func(t *testing.T) {
		now := time.Date(2026, 2, 20, 14, 30, 0, 0, time.UTC)
		candidate := &database.AnomalyCandidate{
			ID:           1,
			ConnectionID: 5,
			MetricName:   "cpu_usage_percent",
			MetricValue:  92.5,
			ZScore:       3.2,
			DetectedAt:   now,
			Context:      `{"baseline_mean": 40.0}`,
		}

		result := engine.buildContextText(candidate)

		checks := []string{
			"Metric: cpu_usage_percent",
			"Value: 92.5000",
			"Z-Score: 3.20",
			"Connection ID: 5",
			"Context: {\"baseline_mean\": 40.0}",
		}

		for _, check := range checks {
			if !strings.Contains(result, check) {
				t.Errorf("buildContextText result missing %q", check)
			}
		}
	})

	t.Run("candidate with database name", func(t *testing.T) {
		now := time.Date(2026, 2, 20, 14, 30, 0, 0, time.UTC)
		dbName := "production_db"
		candidate := &database.AnomalyCandidate{
			ID:           2,
			ConnectionID: 10,
			DatabaseName: &dbName,
			MetricName:   "active_connections",
			MetricValue:  150.0,
			ZScore:       4.5,
			DetectedAt:   now,
			Context:      "{}",
		}

		result := engine.buildContextText(candidate)

		if !strings.Contains(result, "Database: production_db") {
			t.Error("buildContextText should include database name when present")
		}
	})

	t.Run("candidate without database name", func(t *testing.T) {
		now := time.Date(2026, 2, 20, 14, 30, 0, 0, time.UTC)
		candidate := &database.AnomalyCandidate{
			ID:           3,
			ConnectionID: 15,
			DatabaseName: nil,
			MetricName:   "disk_usage_percent",
			MetricValue:  85.0,
			ZScore:       2.8,
			DetectedAt:   now,
			Context:      "{}",
		}

		result := engine.buildContextText(candidate)

		if strings.Contains(result, "Database:") {
			t.Error("buildContextText should not include Database line when nil")
		}
	})

	t.Run("timestamp format", func(t *testing.T) {
		now := time.Date(2026, 3, 15, 10, 45, 30, 0, time.UTC)
		candidate := &database.AnomalyCandidate{
			ID:           4,
			ConnectionID: 1,
			MetricName:   "test_metric",
			MetricValue:  1.0,
			ZScore:       1.0,
			DetectedAt:   now,
			Context:      "{}",
		}

		result := engine.buildContextText(candidate)

		// Should contain RFC3339 formatted time
		if !strings.Contains(result, "2026-03-15T10:45:30Z") {
			t.Error("buildContextText should include RFC3339 formatted timestamp")
		}
	})
}

// TestDetermineFinalDecision tests the determineFinalDecision method
func TestDetermineFinalDecision(t *testing.T) {
	engine := &Engine{}

	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name             string
		candidate        *database.AnomalyCandidate
		expectedDecision string
	}{
		{
			name: "tier2 suppressed tier3 nil",
			candidate: &database.AnomalyCandidate{
				Tier2Pass: boolPtr(false),
				Tier3Pass: nil,
			},
			expectedDecision: "suppress",
		},
		{
			name: "tier2 suppressed tier3 passed should still suppress",
			candidate: &database.AnomalyCandidate{
				Tier2Pass: boolPtr(false),
				Tier3Pass: boolPtr(true),
			},
			expectedDecision: "suppress",
		},
		{
			name: "tier3 passed",
			candidate: &database.AnomalyCandidate{
				Tier2Pass: boolPtr(true),
				Tier3Pass: boolPtr(true),
			},
			expectedDecision: "alert",
		},
		{
			name: "tier3 failed",
			candidate: &database.AnomalyCandidate{
				Tier2Pass: boolPtr(true),
				Tier3Pass: boolPtr(false),
			},
			expectedDecision: "suppress",
		},
		{
			name: "only tier2 passed no tier3",
			candidate: &database.AnomalyCandidate{
				Tier2Pass: boolPtr(true),
				Tier3Pass: nil,
			},
			expectedDecision: "alert",
		},
		{
			name: "tier2 nil tier3 passed",
			candidate: &database.AnomalyCandidate{
				Tier2Pass: nil,
				Tier3Pass: boolPtr(true),
			},
			expectedDecision: "alert",
		},
		{
			name: "tier2 nil tier3 failed",
			candidate: &database.AnomalyCandidate{
				Tier2Pass: nil,
				Tier3Pass: boolPtr(false),
			},
			expectedDecision: "suppress",
		},
		{
			name: "both nil defaults to alert",
			candidate: &database.AnomalyCandidate{
				Tier2Pass: nil,
				Tier3Pass: nil,
			},
			expectedDecision: "alert",
		},
		{
			name: "tier1 true tier2 passed tier3 nil",
			candidate: &database.AnomalyCandidate{
				Tier1Pass: true,
				Tier2Pass: boolPtr(true),
				Tier3Pass: nil,
			},
			expectedDecision: "alert",
		},
		{
			name: "tier1 true tier2 nil tier3 nil",
			candidate: &database.AnomalyCandidate{
				Tier1Pass: true,
				Tier2Pass: nil,
				Tier3Pass: nil,
			},
			expectedDecision: "alert",
		},
		{
			name: "tier1 false tier2 nil tier3 nil defaults to alert",
			candidate: &database.AnomalyCandidate{
				Tier1Pass: false,
				Tier2Pass: nil,
				Tier3Pass: nil,
			},
			expectedDecision: "alert",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine.determineFinalDecision(tt.candidate)

			if tt.candidate.FinalDecision == nil {
				t.Fatal("FinalDecision should not be nil after determineFinalDecision")
			}

			if *tt.candidate.FinalDecision != tt.expectedDecision {
				t.Errorf("FinalDecision = %q, expected %q",
					*tt.candidate.FinalDecision, tt.expectedDecision)
			}
		})
	}
}

// TestDetermineFinalDecisionOverwritesPrevious tests that FinalDecision is overwritten
func TestDetermineFinalDecisionOverwritesPrevious(t *testing.T) {
	engine := &Engine{}
	boolPtr := func(b bool) *bool { return &b }
	strPtr := func(s string) *string { return &s }

	// Start with a previous decision
	candidate := &database.AnomalyCandidate{
		Tier2Pass:     boolPtr(true),
		Tier3Pass:     boolPtr(true),
		FinalDecision: strPtr("old_decision"),
	}

	engine.determineFinalDecision(candidate)

	if *candidate.FinalDecision != "alert" {
		t.Errorf("FinalDecision = %q, expected 'alert'", *candidate.FinalDecision)
	}
}

// TestParseLLMResponseWrapper tests the engine's parseLLMResponse wrapper
func TestParseLLMResponseWrapper(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name             string
		response         string
		expectedDecision string
	}{
		{
			name:             "alert decision",
			response:         `{"decision": "alert", "confidence": 0.9}`,
			expectedDecision: "alert",
		},
		{
			name:             "suppress decision",
			response:         `{"decision": "suppress", "confidence": 0.8}`,
			expectedDecision: "suppress",
		},
		{
			name:             "text fallback false positive",
			response:         "This looks like a false positive",
			expectedDecision: "suppress",
		},
		{
			name:             "text fallback real issue",
			response:         "This is a real issue requiring attention",
			expectedDecision: "alert",
		},
		{
			name:             "unparseable defaults to alert",
			response:         "I don't know",
			expectedDecision: "alert",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := engine.parseLLMResponse(tt.response)
			if decision != tt.expectedDecision {
				t.Errorf("parseLLMResponse(%q) = %q, expected %q",
					tt.response, decision, tt.expectedDecision)
			}
		})
	}
}

// TestWriteClusterContext tests the writeClusterContext helper
func TestWriteClusterContext(t *testing.T) {
	t.Run("no peers", func(t *testing.T) {
		var sb strings.Builder
		writeClusterContext(&sb, 1, nil, nil)

		if sb.String() != "" {
			t.Error("writeClusterContext should produce empty output when no peers")
		}
	})

	t.Run("with peers no alerts", func(t *testing.T) {
		var sb strings.Builder
		peers := []*database.ClusterPeerInfo{
			{ConnectionID: 2, ConnectionName: "primary-node", NodeRole: "binary_primary"},
			{ConnectionID: 3, ConnectionName: "standby-node", NodeRole: "binary_standby"},
		}
		writeClusterContext(&sb, 1, peers, nil)

		result := sb.String()

		checks := []string{
			"Cluster Context",
			"2 other node(s)",
			"primary-node",
			"standby-node",
			"binary_primary",
			"binary_standby",
			"No active alerts on cluster peers",
		}

		for _, check := range checks {
			if !strings.Contains(result, check) {
				t.Errorf("writeClusterContext result missing %q", check)
			}
		}
	})

	t.Run("with peers and alerts", func(t *testing.T) {
		var sb strings.Builder
		peers := []*database.ClusterPeerInfo{
			{ConnectionID: 2, ConnectionName: "primary-node", NodeRole: "binary_primary"},
		}
		alerts := []*database.Alert{
			{
				ID:           100,
				Title:        "High CPU usage",
				Severity:     "warning",
				ConnectionID: 2,
				TriggeredAt:  time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC),
			},
		}
		writeClusterContext(&sb, 1, peers, alerts)

		result := sb.String()

		checks := []string{
			"Cluster Context",
			"1 other node(s)",
			"primary-node",
			"High CPU usage",
			"warning",
			"primary-node",
		}

		for _, check := range checks {
			if !strings.Contains(result, check) {
				t.Errorf("writeClusterContext result missing %q", check)
			}
		}

		// Should NOT contain "No active alerts"
		if strings.Contains(result, "No active alerts on cluster peers") {
			t.Error("writeClusterContext should not say no alerts when there are alerts")
		}
	})

	t.Run("alert with unknown connection", func(t *testing.T) {
		var sb strings.Builder
		peers := []*database.ClusterPeerInfo{
			{ConnectionID: 2, ConnectionName: "known-node", NodeRole: "primary"},
		}
		alerts := []*database.Alert{
			{
				ID:           100,
				Title:        "Unknown node alert",
				Severity:     "high",
				ConnectionID: 999, // Not in peers list
				TriggeredAt:  time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC),
			},
		}
		writeClusterContext(&sb, 1, peers, alerts)

		result := sb.String()

		// Should fall back to "connection 999"
		if !strings.Contains(result, "connection 999") {
			t.Error("writeClusterContext should show 'connection N' for unknown peer")
		}
	})
}

// TestBuildClassificationPromptMinimal tests buildClassificationPrompt with minimal input
func TestBuildClassificationPromptMinimal(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
	candidate := &database.AnomalyCandidate{
		ID:           1,
		ConnectionID: 5,
		MetricName:   "test_metric",
		MetricValue:  100.0,
		ZScore:       3.0,
		DetectedAt:   now,
		Context:      `{"baseline_mean": 50.0, "baseline_stddev": 15.0, "period_type": "all"}`,
	}

	prompt := engine.buildClassificationPrompt(candidate, nil, nil, nil, nil)

	// Should contain basic sections
	checks := []string{
		"Current Anomaly",
		"Metric: test_metric",
		"Value: 100.0000",
		"Z-Score: 3.00",
		"Connection ID: 5",
		"Similar Past Anomalies",
		"No similar past anomalies found",
		"Instructions",
		"\"decision\"",
		"\"confidence\"",
		"\"reasoning\"",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("buildClassificationPrompt result missing %q", check)
		}
	}

	// Should NOT contain cluster context
	if strings.Contains(prompt, "Cluster Context") {
		t.Error("buildClassificationPrompt should not have Cluster Context without peers")
	}
}

// TestBuildClassificationPromptWithAllInputs tests buildClassificationPrompt with all inputs
func TestBuildClassificationPromptWithAllInputs(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
	dbName := "prod_db"
	candidate := &database.AnomalyCandidate{
		ID:           1,
		ConnectionID: 5,
		DatabaseName: &dbName,
		MetricName:   "cpu_usage_percent",
		MetricValue:  95.0,
		ZScore:       4.5,
		DetectedAt:   now,
		Context:      `{"baseline_mean": 40.0, "baseline_stddev": 10.0, "period_type": "hourly"}`,
	}

	similarAnomalies := []*database.SimilarAnomaly{
		{
			CandidateID:   10,
			Similarity:    0.92,
			FinalDecision: strPtr("suppress"),
			MetricName:    "cpu_usage_percent",
		},
		{
			CandidateID:   11,
			Similarity:    0.85,
			FinalDecision: strPtr("alert"),
			MetricName:    "cpu_usage_percent",
		},
	}

	ackHistory := []*database.AcknowledgedAnomalyAlert{
		{
			ID:             20,
			AckMessage:     strPtr("Known maintenance window"),
			FalsePositive:  true,
			AcknowledgedBy: strPtr("admin"),
			AcknowledgedAt: timePtr(now.Add(-24 * time.Hour)),
			MetricValue:    float64Ptr(88.0),
			ZScore:         float64Ptr(3.0),
			Severity:       "warning",
		},
	}

	clusterPeers := []*database.ClusterPeerInfo{
		{ConnectionID: 6, ConnectionName: "replica-1", NodeRole: "standby"},
	}

	clusterAlerts := []*database.Alert{
		{
			ID:           50,
			Title:        "High memory usage",
			Severity:     "warning",
			ConnectionID: 6,
			TriggeredAt:  now.Add(-5 * time.Minute),
		},
	}

	prompt := engine.buildClassificationPrompt(
		candidate, similarAnomalies, ackHistory, clusterPeers, clusterAlerts,
	)

	checks := []string{
		// Basic info
		"cpu_usage_percent",
		"95.0000",
		"4.50",
		"Database: prod_db",

		// Similar anomalies
		"Similar Past Anomalies",
		"92.00%",
		"85.00%",

		// Ack history
		"Past User Feedback",
		"Known maintenance window",
		"FALSE POSITIVE",
		"admin",

		// Cluster context
		"Cluster Context",
		"replica-1",
		"High memory usage",

		// Baseline info from context
		"Baseline mean: 40.0000",
		"Baseline stddev: 10.0000",
		"Baseline period: hourly",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("buildClassificationPrompt result missing %q", check)
		}
	}
}

// TestBuildClassificationPromptWithDatabaseName tests including database name in candidate
func TestBuildClassificationPromptWithDatabaseName(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
	dbName := "mydb"
	candidate := &database.AnomalyCandidate{
		ID:           1,
		ConnectionID: 5,
		DatabaseName: &dbName,
		MetricName:   "test_metric",
		MetricValue:  100.0,
		ZScore:       3.0,
		DetectedAt:   now,
		Context:      `{}`, // Empty context to test parsing
	}

	prompt := engine.buildClassificationPrompt(candidate, nil, nil, nil, nil)

	if !strings.Contains(prompt, "Database: mydb") {
		t.Error("prompt should include database name")
	}
}

// TestBuildClassificationPromptWithNilFinalDecision tests similar anomalies with nil decision
func TestBuildClassificationPromptWithNilFinalDecision(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
	candidate := &database.AnomalyCandidate{
		ID:           1,
		ConnectionID: 5,
		MetricName:   "test",
		MetricValue:  100.0,
		ZScore:       3.0,
		DetectedAt:   now,
		Context:      "{}",
	}

	similarAnomalies := []*database.SimilarAnomaly{
		{
			CandidateID:   10,
			Similarity:    0.85,
			FinalDecision: nil, // nil decision
			MetricName:    "test",
		},
	}

	prompt := engine.buildClassificationPrompt(candidate, similarAnomalies, nil, nil, nil)

	// Should show "unknown" for nil decision
	if !strings.Contains(prompt, "unknown") {
		t.Error("prompt should show 'unknown' for nil FinalDecision")
	}
}

// TestBuildClassificationPromptSimilarAnomalyTruncation tests that similar anomalies are truncated
func TestBuildClassificationPromptSimilarAnomalyTruncation(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
	candidate := &database.AnomalyCandidate{
		ID:           1,
		ConnectionID: 5,
		MetricName:   "test",
		MetricValue:  100.0,
		ZScore:       3.0,
		DetectedAt:   now,
		Context:      "{}",
	}

	// Create more than 5 similar anomalies
	similarAnomalies := make([]*database.SimilarAnomaly, 10)
	for i := 0; i < 10; i++ {
		similarAnomalies[i] = &database.SimilarAnomaly{
			CandidateID:   int64(i),
			Similarity:    0.9 - float64(i)*0.05,
			FinalDecision: strPtr("alert"),
			MetricName:    "test",
		}
	}

	prompt := engine.buildClassificationPrompt(candidate, similarAnomalies, nil, nil, nil)

	// Should indicate truncation
	if !strings.Contains(prompt, "and 5 more similar anomalies") {
		t.Error("buildClassificationPrompt should indicate truncation for >5 similar anomalies")
	}
}
