/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package chat

import (
	"sort"
)

// ModelSelectionResult contains the result of model selection
type ModelSelectionResult struct {
	Model           string
	FromSavedPref   bool // true if selected from saved preference (exact or family match)
	HadSavedPref    bool // true if there was a saved preference for this provider
	UsedFamilyMatch bool // true if a newer version in the same family was selected
}

// SelectModelFromPreferences determines the best model to use based on:
// 1. Saved preference - exact match
// 2. Saved preference - family match (e.g., claude-opus-4-5-20251101 -> claude-opus-4-5-20251217)
// 3. Default for provider (if available)
// 4. First available model from provider's list
func SelectModelFromPreferences(provider string, savedModel string, availableModels []string) ModelSelectionResult {
	if savedModel != "" {
		// Try exact match first
		if IsModelAvailable(savedModel, availableModels) {
			return ModelSelectionResult{Model: savedModel, FromSavedPref: true, HadSavedPref: true}
		}

		// Try family match (e.g., claude-opus-4-5-* when saved is claude-opus-4-5-20251101)
		// This handles Anthropic releasing newer versions of the same model
		if familyMatch := FindModelFamilyMatch(savedModel, availableModels); familyMatch != "" {
			return ModelSelectionResult{
				Model:           familyMatch,
				FromSavedPref:   true,
				HadSavedPref:    true,
				UsedFamilyMatch: true,
			}
		}

		// Saved preference exists but couldn't be matched
		// Fall through to defaults, but remember we had a saved pref
	}

	hadSaved := savedModel != ""

	// Use default for provider
	defaultModel := GetDefaultModelForProvider(provider)
	if IsModelAvailable(defaultModel, availableModels) {
		return ModelSelectionResult{Model: defaultModel, FromSavedPref: false, HadSavedPref: hadSaved}
	}

	// Fall back to first available model
	if len(availableModels) > 0 {
		return ModelSelectionResult{Model: availableModels[0], FromSavedPref: false, HadSavedPref: hadSaved}
	}

	// Last resort: use default even if not validated
	return ModelSelectionResult{Model: defaultModel, FromSavedPref: false, HadSavedPref: hadSaved}
}

// FindModelFamilyMatch finds a model in availableModels that matches the family of savedModel.
// Family matching: "claude-opus-4-5-20251101" matches "claude-opus-4-5-*"
// Returns the latest (by date suffix) matching model, or empty string if no match.
func FindModelFamilyMatch(savedModel string, availableModels []string) string {
	if len(availableModels) == 0 {
		return ""
	}

	// Extract family prefix (everything before the last date segment)
	// e.g., "claude-opus-4-5-20251101" -> "claude-opus-4-5-"
	family := ExtractModelFamily(savedModel)
	if family == "" {
		return ""
	}

	// Find all models with the SAME family (exact family match, not prefix)
	// e.g., claude-opus-4-5- should not match claude-opus-4- or claude-opus-4-5-1-
	var matches []string
	for _, m := range availableModels {
		modelFamily := ExtractModelFamily(m)
		if modelFamily == family {
			matches = append(matches, m)
		}
	}

	if len(matches) == 0 {
		return ""
	}

	// Return the latest version (highest date suffix)
	// Models are typically returned sorted, but sort to be safe
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// ExtractModelFamily extracts the model family prefix from a model ID.
// Returns the prefix including trailing hyphen, or empty string if not parseable.
// Examples:
//   - "claude-opus-4-5-20251101" -> "claude-opus-4-5-"
//   - "claude-sonnet-4-20250514" -> "claude-sonnet-4-"
//   - "gpt-4o-mini" -> "" (no date suffix pattern)
func ExtractModelFamily(model string) string {
	// Look for a date suffix pattern: -YYYYMMDD at the end
	// The date is 8 digits after a hyphen
	if len(model) < 9 {
		return ""
	}

	// Check if last 8 chars are digits (date)
	suffix := model[len(model)-8:]
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return "" // Not a date suffix
		}
	}

	// Check there's a hyphen before the date
	if len(model) < 10 || model[len(model)-9] != '-' {
		return ""
	}

	// Return everything up to and including the hyphen before the date
	return model[:len(model)-8]
}

// IsModelAvailable checks if model is in the available list
// Returns true if availableModels is nil (couldn't fetch) for graceful degradation
func IsModelAvailable(model string, availableModels []string) bool {
	if availableModels == nil {
		return true // Can't validate, assume available
	}
	for _, m := range availableModels {
		if m == model {
			return true
		}
	}
	return false
}

// GetDefaultModelForProvider returns the default model for a provider
func GetDefaultModelForProvider(provider string) string {
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-5-20250929"
	case "openai":
		return "gpt-4o"
	case "ollama":
		return "qwen3-coder:latest"
	default:
		return ""
	}
}
