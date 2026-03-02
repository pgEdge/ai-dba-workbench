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
	"encoding/json"
	"strings"
)

// llmDecisionResponse is the expected JSON structure from LLM classification
// and re-evaluation responses.
type llmDecisionResponse struct {
	Decision   string  `json:"decision"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// llmDecisionConfig defines the keywords and fallback behavior for parsing
// an LLM decision response.
type llmDecisionConfig struct {
	// ValidDecisions maps lowercase decision strings to their canonical form.
	ValidDecisions map[string]string

	// TextKeywords maps canonical decisions to keyword phrases that indicate
	// that decision in natural language.
	TextKeywords map[string][]string

	// DefaultDecision is returned when the response cannot be parsed.
	DefaultDecision string

	// DefaultConfidence is the confidence returned for the default decision.
	DefaultConfidence float64

	// FallbackConfidence is the confidence returned for text-matched decisions.
	FallbackConfidence float64
}

// parseLLMDecision parses an LLM response string into a decision and
// confidence score using the provided configuration. It first attempts
// JSON parsing, then falls back to keyword matching in the response text.
func parseLLMDecision(response string, cfg llmDecisionConfig) (string, float64) {
	// Try JSON parsing first
	var result llmDecisionResponse
	if err := json.Unmarshal([]byte(response), &result); err == nil {
		canonical, ok := cfg.ValidDecisions[strings.ToLower(result.Decision)]
		if ok {
			return canonical, result.Confidence
		}
	}

	// Fall back to text matching
	lowerResponse := strings.ToLower(response)

	for decision, keywords := range cfg.TextKeywords {
		for _, keyword := range keywords {
			if strings.Contains(lowerResponse, keyword) {
				return decision, cfg.FallbackConfidence
			}
		}
	}

	return cfg.DefaultDecision, cfg.DefaultConfidence
}

// reevaluationDecisionConfig returns the parsing configuration for
// re-evaluation responses (clear/keep decisions).
var reevaluationDecisionConfig = llmDecisionConfig{
	ValidDecisions: map[string]string{
		"clear": "clear",
		"keep":  "keep",
	},
	TextKeywords: map[string][]string{
		"clear": {
			"\"clear\"",
			"'clear'",
			"should be cleared",
			"safe to clear",
			"recommend clearing",
		},
		"keep": {
			"\"keep\"",
			"'keep'",
			"should be kept",
			"keep active",
			"remain active",
		},
	},
	DefaultDecision:    "keep",
	DefaultConfidence:  0.3,
	FallbackConfidence: 0.5,
}

// anomalyDecisionConfig returns the parsing configuration for anomaly
// classification responses (alert/suppress decisions).
var anomalyDecisionConfig = llmDecisionConfig{
	ValidDecisions: map[string]string{
		"alert":          "alert",
		"anomaly":        "alert",
		"suppress":       "suppress",
		"suppressed":     "suppress",
		"false_positive": "suppress",
	},
	TextKeywords: map[string][]string{
		"suppress": {
			"should be suppressed",
			"false positive",
			"not a real issue",
			"normal behavior",
		},
		"alert": {
			"real issue",
			"should alert",
			"requires attention",
			"genuine anomaly",
		},
	},
	DefaultDecision:    "alert",
	DefaultConfidence:  0.3,
	FallbackConfidence: 0.5,
}
