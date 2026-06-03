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
			// Genuine "normal" language now uses a specific phrasing so it
			// reliably suppresses.
			name:               "this is normal behavior",
			response:           "This is normal behavior for the system",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.5,
		},
		{
			// The "within normal behavior" phrasing also suppresses.
			name:               "within normal behavior",
			response:           "This stays within normal behavior for the workload",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.5,
		},
		{
			// A bare "normal behavior" mention no longer matches the specific
			// suppress keyword, so it falls through to the fail-safe default
			// of "alert" rather than wrongly suppressing.
			name:               "bare normal behavior falls through to alert",
			response:           "This looks like normal behavior for the workload",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.3,
		},
		{
			// "deviation from normal behavior" means NOT normal; it must not
			// match the suppress keyword and so defaults to alert.
			name:               "deviation from normal behavior is not suppressed",
			response:           "This is a deviation from normal behavior",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.3,
		},
		{
			name:               "real issue text",
			response:           "This is a real issue that needs attention",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.5,
		},
		{
			// "not a real issue" must reach the suppress group rather than
			// matching the alert keyword "is a real issue". The substring
			// "is not a real issue" does not contain "is a real issue", so
			// the response correctly falls through and suppresses.
			name:               "not a real issue falls through to suppress",
			response:           "After review, this is not a real issue worth alerting on",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
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

// TestParseLLMDecisionFencedJSON tests that JSON wrapped in markdown code
// fences, or embedded in surrounding prose, is parsed correctly.
func TestParseLLMDecisionFencedJSON(t *testing.T) {
	tests := []struct {
		name               string
		response           string
		config             llmDecisionConfig
		expectedDecision   string
		expectedConfidence float64
	}{
		{
			name:               "json fence alert",
			response:           "```json\n{\"decision\":\"alert\",\"confidence\":0.9}\n```",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.9,
		},
		{
			name:               "json fence suppress",
			response:           "```json\n{\"decision\":\"suppress\",\"confidence\":0.8}\n```",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.8,
		},
		{
			name:               "bare fence alert",
			response:           "```\n{\"decision\":\"alert\",\"confidence\":0.7}\n```",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.7,
		},
		{
			name:               "bare fence reevaluation clear",
			response:           "```\n{\"decision\":\"clear\",\"confidence\":0.6}\n```",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "clear",
			expectedConfidence: 0.6,
		},
		{
			name:               "json embedded in prose",
			response:           `Here is my assessment: {"decision":"alert","confidence":0.95} as requested.`,
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.95,
		},
		{
			name:               "fenced json with leading prose",
			response:           "After analysis I conclude:\n```json\n{\"decision\":\"suppress\",\"confidence\":0.55}\n```\nThanks.",
			config:             anomalyDecisionConfig,
			expectedDecision:   "suppress",
			expectedConfidence: 0.55,
		},
		{
			name:               "plain unfenced json still works",
			response:           `{"decision":"alert","confidence":0.42}`,
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.42,
		},
		{
			name:               "fence with surrounding whitespace",
			response:           "   ```json\n{\"decision\":\"keep\",\"confidence\":0.33}\n```   ",
			config:             reevaluationDecisionConfig,
			expectedDecision:   "keep",
			expectedConfidence: 0.33,
		},
		{
			// A fence marker with no newline yields no JSON; embedded object
			// extraction recovers the decision instead.
			name:               "single-line fence no newline",
			response:           "```{\"decision\":\"alert\",\"confidence\":0.51}```",
			config:             anomalyDecisionConfig,
			expectedDecision:   "alert",
			expectedConfidence: 0.51,
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

// TestParseLLMDecisionDeterministic guards against regression of the map
// iteration ordering bug. A response containing keywords for BOTH decisions
// must always resolve to the same fail-safe decision.
func TestParseLLMDecisionDeterministic(t *testing.T) {
	t.Run("anomaly conflicting keywords always alert", func(t *testing.T) {
		// "requires attention" (alert) and "false positive" (suppress) both
		// appear; the fail-safe bias must always choose alert.
		response := "This deviation from normal behavior requires attention, " +
			"though some might call it a false positive."
		for i := 0; i < 1000; i++ {
			decision, _ := parseLLMDecision(response, anomalyDecisionConfig)
			if decision != "alert" {
				t.Fatalf("iteration %d: decision = %q, expected stable \"alert\"", i, decision)
			}
		}
	})

	t.Run("bug scenario deviation requires attention", func(t *testing.T) {
		// The exact scenario from the bug report: must deterministically alert.
		response := "This is a deviation from normal behavior and requires attention."
		for i := 0; i < 1000; i++ {
			decision, confidence := parseLLMDecision(response, anomalyDecisionConfig)
			if decision != "alert" {
				t.Fatalf("iteration %d: decision = %q, expected stable \"alert\"", i, decision)
			}
			if math.Abs(confidence-0.5) > 0.001 {
				t.Fatalf("iteration %d: confidence = %v, expected 0.5", i, confidence)
			}
		}
	})

	t.Run("reevaluation conflicting keywords always keep", func(t *testing.T) {
		// "remain active" (keep) and "safe to clear" (clear) both appear; the
		// fail-safe bias must always choose keep.
		response := "The alert should remain active even though it may be safe to clear."
		for i := 0; i < 1000; i++ {
			decision, _ := parseLLMDecision(response, reevaluationDecisionConfig)
			if decision != "keep" {
				t.Fatalf("iteration %d: decision = %q, expected stable \"keep\"", i, decision)
			}
		}
	})
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
