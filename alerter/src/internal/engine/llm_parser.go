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

// decisionKeywords associates a canonical decision with the natural-language
// keyword phrases that indicate it. Keyword groups are evaluated in slice
// order so that matching is deterministic and has a well-defined precedence.
type decisionKeywords struct {
	// Decision is the canonical decision returned when a keyword matches.
	Decision string

	// Keywords are the lowercase phrases that indicate the decision.
	Keywords []string
}

// llmDecisionConfig defines the keywords and fallback behavior for parsing
// an LLM decision response.
type llmDecisionConfig struct {
	// ValidDecisions maps lowercase decision strings to their canonical form.
	ValidDecisions map[string]string

	// TextKeywords lists keyword groups in precedence order. The first group
	// with a matching keyword wins, so fail-safe decisions must come first.
	TextKeywords []decisionKeywords

	// DefaultDecision is returned when the response cannot be parsed.
	DefaultDecision string

	// DefaultConfidence is the confidence returned for the default decision.
	DefaultConfidence float64

	// FallbackConfidence is the confidence returned for text-matched decisions.
	FallbackConfidence float64
}

// extractJSONObject returns the substring spanning the first '{' to the last
// '}' in s, or the empty string if no such span exists. This recovers a JSON
// object embedded in surrounding prose.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < 0 || end < start {
		return ""
	}
	return s[start : end+1]
}

// stripCodeFence removes a leading markdown code fence (```json or ```) and a
// trailing fence from s, returning the trimmed inner content. If s is not
// fenced, the whitespace-trimmed input is returned unchanged.
func stripCodeFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	// Drop the opening fence line (e.g. "```json" or "```").
	trimmed = strings.TrimPrefix(trimmed, "```")
	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		trimmed = trimmed[idx+1:]
	} else {
		trimmed = ""
	}

	// Drop the trailing closing fence if present.
	trimmed = strings.TrimSpace(trimmed)
	if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
		trimmed = trimmed[:idx]
	}

	return strings.TrimSpace(trimmed)
}

// parseDecisionJSON attempts to decode response as a decision object, first
// directly, then after stripping markdown fences, then by extracting an
// embedded JSON object. It returns the canonical decision, its confidence,
// and whether a valid decision was found.
func parseDecisionJSON(response string, cfg llmDecisionConfig) (string, float64, bool) {
	candidates := []string{
		response,
		stripCodeFence(response),
	}
	if obj := extractJSONObject(response); obj != "" {
		candidates = append(candidates, obj)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, dup := seen[candidate]; dup {
			continue
		}
		seen[candidate] = struct{}{}

		var result llmDecisionResponse
		if err := json.Unmarshal([]byte(candidate), &result); err != nil {
			continue
		}
		if canonical, ok := cfg.ValidDecisions[strings.ToLower(result.Decision)]; ok {
			return canonical, result.Confidence, true
		}
	}

	return "", 0, false
}

// parseLLMDecision parses an LLM response string into a decision and
// confidence score using the provided configuration. It first attempts
// JSON parsing (tolerating markdown code fences and surrounding prose),
// then falls back to deterministic keyword matching in the response text.
func parseLLMDecision(response string, cfg llmDecisionConfig) (string, float64) {
	// Try JSON parsing first, including fenced and embedded variants.
	if decision, confidence, ok := parseDecisionJSON(response, cfg); ok {
		return decision, confidence
	}

	// Fall back to text matching. Keyword groups are evaluated in order so
	// the result is deterministic and biased toward the fail-safe decision.
	lowerResponse := strings.ToLower(response)

	for _, group := range cfg.TextKeywords {
		for _, keyword := range group.Keywords {
			if strings.Contains(lowerResponse, keyword) {
				return group.Decision, cfg.FallbackConfidence
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
	// Keyword groups are evaluated in order. "keep" is checked before
	// "clear" so that ambiguous responses err toward keeping the alert
	// active, matching the fail-safe DefaultDecision below.
	TextKeywords: []decisionKeywords{
		{
			Decision: "keep",
			Keywords: []string{
				"\"keep\"",
				"'keep'",
				"should be kept",
				"keep active",
				"remain active",
			},
		},
		{
			Decision: "clear",
			Keywords: []string{
				"\"clear\"",
				"'clear'",
				"should be cleared",
				"safe to clear",
				"recommend clearing",
			},
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
	// Keyword groups are evaluated in order. "alert" is checked before
	// "suppress" so that ambiguous responses err toward alerting, matching
	// the fail-safe DefaultDecision below. The suppress phrases are kept
	// specific (for example "is normal behavior") so that prose such as
	// "deviation from normal behavior" does not wrongly suppress an alert.
	TextKeywords: []decisionKeywords{
		{
			Decision: "alert",
			Keywords: []string{
				"is a real issue",
				"should alert",
				"requires attention",
				"genuine anomaly",
			},
		},
		{
			Decision: "suppress",
			Keywords: []string{
				"should be suppressed",
				"false positive",
				"not a real issue",
				"is normal behavior",
				"within normal behavior",
			},
		},
	},
	DefaultDecision:    "alert",
	DefaultConfidence:  0.3,
	FallbackConfidence: 0.5,
}
