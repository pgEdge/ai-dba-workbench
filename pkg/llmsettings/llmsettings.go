/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package llmsettings holds the LLM request settings that the server's
// llm section and the alerter's per-provider llm sections share:
// reasoning effort, prompt caching and extra request body fields. It
// validates them when the configuration is loaded and copies them onto
// the pgedge-go-llm-lib client options; the library does the translation
// into each provider's request.
package llmsettings

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
)

// Settings is the set of LLM request settings an operator can configure.
// It is embedded inline in each configuration section that carries it,
// so the keys sit beside the section's other settings.
type Settings struct {
	// ReasoningEffort sets how much the model reasons before it answers:
	// none, minimal, low, medium, high, xhigh or max. Empty leaves it
	// to the model, and the request is sent without the setting.
	ReasoningEffort string `yaml:"reasoning_effort"`

	// PromptCaching is "auto" to cache the prompt prefix on providers
	// that need it asked for (Anthropic), or "off" or empty for no
	// automatic caching.
	PromptCaching string `yaml:"prompt_caching"`

	// ExtraBody holds fields added to the JSON body of each chat request
	// sent through the OpenAI provider, for fields the library does not
	// model, such as an OpenAI-compatible gateway's own options. Other
	// providers ignore it. Its values may carry anything, so they are
	// never logged or quoted in an error.
	ExtraBody map[string]any `yaml:"extra_body"`
}

// reasoningEfforts lists the accepted reasoning_effort values, in the
// order an error message names them.
var reasoningEfforts = []llm.ReasoningEffort{
	llm.ReasoningEffortNone, llm.ReasoningEffortMinimal,
	llm.ReasoningEffortLow, llm.ReasoningEffortMedium,
	llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh,
	llm.ReasoningEffortMax,
}

// Validate reports the first setting the library would refuse, naming
// it under section (for example "llm" or "llm.openai"). Nothing is sent
// at load time, so this catches a mistyped value when the configuration
// is loaded or reloaded rather than on the first request.
//
// The extra_body rules mirror the ones the library's OpenAI provider
// applies before sending: a stream field is refused, because the library
// decides streaming itself, and every value must encode as JSON. A nested
// mapping with non-string keys is refused explicitly rather than left to
// the encoder, since newer Go releases encode such maps where older ones
// refused them. An empty key is refused as well, since it can only be a
// mistake. Errors name the key but never the value, which may be a
// credential.
func (s Settings) Validate(section string) error {
	if !llm.ReasoningEffort(s.ReasoningEffort).Valid() {
		names := make([]string, len(reasoningEfforts))
		for i, e := range reasoningEfforts {
			names[i] = string(e)
		}
		return fmt.Errorf("%s.reasoning_effort must be one of %s, or empty, not %q",
			section, strings.Join(names, ", "), s.ReasoningEffort)
	}
	if !llm.PromptCaching(s.PromptCaching).Valid() {
		return fmt.Errorf("%s.prompt_caching must be %s, %s or empty, not %q",
			section, llm.PromptCachingAuto, llm.PromptCachingOff, s.PromptCaching)
	}

	keys := make([]string, 0, len(s.ExtraBody))
	for k := range s.ExtraBody {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch {
		case strings.TrimSpace(k) == "":
			return fmt.Errorf("%s.extra_body must not contain an empty key", section)
		case k == "stream":
			return fmt.Errorf("%s.extra_body must not set stream, which the server chooses for each request",
				section)
		}
		if hasNonStringKey(reflect.ValueOf(s.ExtraBody[k])) {
			return fmt.Errorf("%s.extra_body.%s cannot be encoded as JSON: "+
				"check for a nested mapping with non-string keys, or a NaN "+
				"or infinite number", section, k)
		}
		if _, err := json.Marshal(s.ExtraBody[k]); err != nil {
			// Deliberately drop err: the encoder's message can quote
			// part of the value.
			return fmt.Errorf("%s.extra_body.%s cannot be encoded as JSON: "+
				"check for a nested mapping with non-string keys, or a NaN "+
				"or infinite number", section, k)
		}
	}
	return nil
}

// hasNonStringKey reports whether v holds, at any depth, a map whose key
// type is not string. yaml.v3 decodes a mapping with any non-string key
// (such as 1: x) to map[interface{}]interface{}, which Go 1.26's
// encoding/json refuses but Go 1.27's encodes, so Validate checks for it
// itself to behave the same on every Go version.
func hasNonStringKey(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		return !v.IsNil() && hasNonStringKey(v.Elem())
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return true
		}
		iter := v.MapRange()
		for iter.Next() {
			if hasNonStringKey(iter.Value()) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if hasNonStringKey(v.Index(i)) {
				return true
			}
		}
	}
	return false
}

// CachingEnabled reports whether prompt caching is set to auto, which is
// when a caller should also mark its stable prefixes (the tool list and
// the system prompt) with the library's Anthropic caching helpers.
func (s Settings) CachingEnabled() bool {
	return llm.PromptCaching(s.PromptCaching) == llm.PromptCachingAuto
}

// Apply copies the settings onto opts as client defaults. ExtraBody is
// copied into a new map, so a client never shares the configuration's
// map; the values themselves are shared, and neither side modifies them.
func (s Settings) Apply(opts *llm.Options) {
	opts.ReasoningEffort = llm.ReasoningEffort(s.ReasoningEffort)
	opts.PromptCaching = llm.PromptCaching(s.PromptCaching)
	if len(s.ExtraBody) == 0 {
		opts.ExtraBody = nil
		return
	}
	opts.ExtraBody = make(map[string]any, len(s.ExtraBody))
	for k, v := range s.ExtraBody {
		opts.ExtraBody[k] = v
	}
}
