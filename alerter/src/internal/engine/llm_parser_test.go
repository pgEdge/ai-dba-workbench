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
	"math"
	"testing"
)

// TestParseLLMDecisionValidJSON tests valid JSON responses
func TestParseLLMDecisionValidJSON(t *testing.T) {
	tests := []struct {
		name               string
		response           string
		config             llmDecisionConfig
		expectedDecision   string
		expectedConfidence float64
	}{
		{
			name:               "reevaluation clear",
			response:           `{"decision": "clear", "confidence": 0.9, "reasoning": "User confirmed"}`,
			config:             reevaluationDecisionConfig,
			expectedDecision:   "clear",
			expectedConfidence: 0.9,
		},
		{
			name:               "reevaluation keep",
			response:           `{"decision": "keep", "confidence": 0.8, "reasoning": "Still valid"}`,
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.8,
		},
		{
			name:               "anomaly alert",
			response:           `{"decision": "alert", "confidence": 0.95, "reasoning": "Real issue"}`,
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.95,
		},
		{
			name:               "anomaly suppress",
			response:           `{"decision": "suppress", "confidence": 0.85, "reasoning": "False positive"}`,
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.85,
		},
		{
			name:               "anomaly false_positive synonym",
			response:           `{"decision": "false_positive", "confidence": 0.75, "reasoning": "Not real"}`,
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.75,
		},
		{
			name:               "anomaly anomaly synonym",
			response:           `{"decision": "anomaly", "confidence": 0.88, "reasoning": "Genuine anomaly"}`,
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.88,
		},
		{
			name:               "case insensitive decision",
			response:           `{"decision": "CLEAR", "confidence": 0.85, "reasoning": "OK"}`,
			config:             reevaluationDecisionConfig,
			expectedDecision:   "clear",
			expectedConfidence: 0.85,
		},
		{
			name:               "extra fields ignored",
			response:           `{"decision": "keep", "confidence": 0.7, "reasoning": "test", "extra": "field"}`,
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, confidence := parseLLMDecision(tt.response, tt.config)

			if decision != tt.expectedDecision {
				t.Errorf("decision = %q, expected %q", decision, tt.expectedDecision)
			}

			if math.Abs(confidence-tt.expectedConfidence) > 0.001 {
				t.Errorf("confidence = %v, expected %v", confidence, tt.expectedConfidence)
			}
		})
	}
}

// TestParseLLMDecisionTextFallback tests text-based fallback parsing
func TestParseLLMDecisionTextFallback(t *testing.T) {
	tests := []struct {
		name               string
		response           string
		config             llmDecisionConfig
		expectedDecision   string
		expectedConfidence float64
	}{
		// Reevaluation text fallbacks
		{
			name:               "text should be cleared",
			response:           "Based on feedback, this alert should be cleared",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "clear",
			expectedConfidence: 0.5,
		},
		{
			name:               "text safe to clear",
			response:           "This is safe to clear based on the pattern",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "clear",
			expectedConfidence: 0.5,
		},
		{
			name:               "text recommend clearing",
			response:           "I recommend clearing this alert",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "clear",
			expectedConfidence: 0.5,
		},
		{
			name:               "text should be kept",
			response:           "This alert should be kept for monitoring",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.5,
		},
		{
			name:               "text remain active",
			response:           "The alert should remain active",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.5,
		},
		{
			name:               "quoted clear",
			response:           `The decision is "clear" based on history`,
			config:             reevaluationDecisionConfig,
			expectedDecision:   "clear",
			expectedConfidence: 0.5,
		},
		{
			name:               "single quoted keep",
			response:           `I suggest 'keep' for this alert`,
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.5,
		},

		// Anomaly text fallbacks
		{
			name:               "should be suppressed",
			response:           "This anomaly should be suppressed as it's normal",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.5,
		},
		{
			name:               "false positive text",
			response:           "This appears to be a false positive",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.5,
		},
		{
			name:               "this is normal behavior",
			response:           "This looks like normal behavior for the system",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.5,
		},
		{
			name:               "normal behavior",
			response:           "This looks like normal behavior for the workload",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.5,
		},
		{
			name:               "real issue text",
			response:           "This is a real issue that needs attention",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.5,
		},
		{
			name:               "should alert text",
			response:           "The system should alert on this pattern",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.5,
		},
		{
			name:               "requires attention",
			response:           "This requires attention from the DBA",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.5,
		},
		{
			name:               "genuine anomaly",
			response:           "This appears to be a genuine anomaly",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, confidence := parseLLMDecision(tt.response, tt.config)

			if decision != tt.expectedDecision {
				t.Errorf("decision = %q, expected %q", decision, tt.expectedDecision)
			}

			if math.Abs(confidence-tt.expectedConfidence) > 0.001 {
				t.Errorf("confidence = %v, expected %v", confidence, tt.expectedConfidence)
			}
		})
	}
}

// TestParseLLMDecisionDefaults tests default behavior for unparseable input
func TestParseLLMDecisionDefaults(t *testing.T) {
	tests := []struct {
		name               string
		response           string
		config             llmDecisionConfig
		expectedDecision   string
		expectedConfidence float64
	}{
		{
			name:               "empty string reevaluation",
			response:           "",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.3,
		},
		{
			name:               "empty string anomaly",
			response:           "",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.3,
		},
		{
			name:               "random text reevaluation",
			response:           "I'm not sure what to do here",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.3,
		},
		{
			name:               "random text anomaly",
			response:           "The weather is nice today",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.3,
		},
		{
			name:               "invalid JSON",
			response:           `{"decision": }`,
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.3,
		},
		{
			name:               "JSON with invalid decision",
			response:           `{"decision": "maybe", "confidence": 0.5}`,
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.3,
		},
		{
			name:               "JSON with unknown anomaly decision",
			response:           `{"decision": "unknown", "confidence": 0.5}`,
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, confidence := parseLLMDecision(tt.response, tt.config)

			if decision != tt.expectedDecision {
				t.Errorf("decision = %q, expected %q", decision, tt.expectedDecision)
			}

			if math.Abs(confidence-tt.expectedConfidence) > 0.001 {
				t.Errorf("confidence = %v, expected %v", confidence, tt.expectedConfidence)
			}
		})
	}
}

// TestParseLLMDecisionMixedCase tests case handling in responses
func TestParseLLMDecisionMixedCase(t *testing.T) {
	tests := []struct {
		name             string
		response         string
		config           llmDecisionConfig
		expectedDecision string
	}{
		{"uppercase clear", `{"decision": "CLEAR", "confidence": 0.8}`, reevaluationDecisionConfig, "clear"},
		{"mixed case Keep", `{"decision": "Keep", "confidence": 0.8}`, reevaluationDecisionConfig, "keep"},
		{"uppercase ALERT", `{"decision": "ALERT", "confidence": 0.8}`, anomalyDecisionConfig, "alert"},
		{"mixed case Suppress", `{"decision": "Suppress", "confidence": 0.8}`, anomalyDecisionConfig, "suppress"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := parseLLMDecision(tt.response, tt.config)
			if decision != tt.expectedDecision {
				t.Errorf("decision = %q, expected %q", decision, tt.expectedDecision)
			}
		})
	}
}

// TestLLMDecisionConfigs verifies the config structures are correct
func TestLLMDecisionConfigs(t *testing.T) {
	t.Run("reevaluation config has required decisions", func(t *testing.T) {
		if _, ok := reevaluationDecisionConfig.ValidDecisions["clear"]; !ok {
			t.Error("reevaluationDecisionConfig missing 'clear' decision")
		}
		if _, ok := reevaluationDecisionConfig.ValidDecisions["keep"]; !ok {
			t.Error("reevaluationDecisionConfig missing 'keep' decision")
		}
	})

	t.Run("anomaly config has required decisions", func(t *testing.T) {
		if _, ok := anomalyDecisionConfig.ValidDecisions["alert"]; !ok {
			t.Error("anomalyDecisionConfig missing 'alert' decision")
		}
		if _, ok := anomalyDecisionConfig.ValidDecisions["suppress"]; !ok {
			t.Error("anomalyDecisionConfig missing 'suppress' decision")
		}
	})

	t.Run("reevaluation default is keep", func(t *testing.T) {
		if reevaluationDecisionConfig.DefaultDecision != "keep" {
			t.Errorf("reevaluationDecisionConfig.DefaultDecision = %q, expected 'keep'",
				reevaluationDecisionConfig.DefaultDecision)
		}
	})

	t.Run("anomaly default is alert", func(t *testing.T) {
		if anomalyDecisionConfig.DefaultDecision != "alert" {
			t.Errorf("anomalyDecisionConfig.DefaultDecision = %q, expected 'alert'",
				anomalyDecisionConfig.DefaultDecision)
		}
	})
}

// TestParseLLMDecisionSynonyms tests that synonyms map to canonical decisions
func TestParseLLMDecisionSynonyms(t *testing.T) {
	synonymTests := []struct {
		input    string
		expected string
	}{
		{"alert", "alert"},
		{"anomaly", "alert"},
		{"suppress", "suppress"},
		{"suppressed", "suppress"},
		{"false_positive", "suppress"},
	}

	for _, tt := range synonymTests {
		t.Run(tt.input, func(t *testing.T) {
			response := `{"decision": "` + tt.input + `", "confidence": 0.8}`
			decision, _ := parseLLMDecision(response, anomalyDecisionConfig)
			if decision != tt.expected {
				t.Errorf("decision = %q, expected %q for input %q",
					decision, tt.expected, tt.input)
			}
		})
	}
}
