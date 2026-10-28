/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package llmsettings

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
	"gopkg.in/yaml.v3"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "empty"},
		{name: "effort xhigh", yaml: "reasoning_effort: xhigh"},
		{name: "effort none", yaml: "reasoning_effort: none"},
		{name: "caching auto", yaml: "prompt_caching: auto"},
		{name: "caching off", yaml: "prompt_caching: off"},
		{
			name: "gateway options",
			yaml: "extra_body:\n  providerOptions:\n    gateway:\n      caching: auto\n  prompt_cache_key: ellie",
		},
		{
			name:    "unknown effort",
			yaml:    "reasoning_effort: extreme",
			wantErr: `llm.reasoning_effort must be one of none, minimal, low, medium, high, xhigh, max, or empty, not "extreme"`,
		},
		{
			name:    "effort is case-sensitive",
			yaml:    "reasoning_effort: High",
			wantErr: "llm.reasoning_effort must be one of",
		},
		{
			name:    "unknown caching",
			yaml:    "prompt_caching: always",
			wantErr: `llm.prompt_caching must be auto, off or empty, not "always"`,
		},
		{
			name:    "stream refused",
			yaml:    "extra_body:\n  stream: true",
			wantErr: "llm.extra_body must not set stream",
		},
		{
			name:    "empty key refused",
			yaml:    "extra_body:\n  \"\": 1",
			wantErr: "llm.extra_body must not contain an empty key",
		},
		{
			name:    "non-string nested key refused",
			yaml:    "extra_body:\n  options:\n    1: secret-value",
			wantErr: "llm.extra_body.options cannot be encoded as JSON",
		},
		{
			name:    "non-string key in a list refused",
			yaml:    "extra_body:\n  options:\n    - name: a\n    - 2: secret-value",
			wantErr: "llm.extra_body.options cannot be encoded as JSON",
		},
		{
			name:    "non-string deeply nested key refused",
			yaml:    "extra_body:\n  options:\n    inner:\n      deeper:\n        true: secret-value",
			wantErr: "llm.extra_body.options cannot be encoded as JSON",
		},
		{
			name: "nested lists and null accepted",
			yaml: "extra_body:\n  options:\n    - [1, 2]\n    - ~\n    - {k: v}",
		},
		{
			name:    "NaN refused",
			yaml:    "extra_body:\n  weight: .nan",
			wantErr: "llm.extra_body.weight cannot be encoded as JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Settings
			if err := yaml.Unmarshal([]byte(tt.yaml), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := s.Validate("llm")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("Validate() error quotes an extra_body value: %v", err)
			}
		})
	}
}

// TestHasNonStringKey covers value shapes YAML never produces but a
// caller building Settings in code could: pointers, arrays and typed maps.
func TestHasNonStringKey(t *testing.T) {
	inner := map[int]string{1: "x"}
	var nilMap *map[string]any
	tests := []struct {
		name string
		v    any
		want bool
	}{
		{name: "nil", v: nil},
		{name: "scalar", v: 3},
		{name: "string-keyed map", v: map[string]int{"a": 1}},
		{name: "int-keyed map", v: map[int]int{1: 1}, want: true},
		{name: "pointer to bad map", v: &inner, want: true},
		{name: "nil pointer", v: nilMap},
		{name: "array holding bad map", v: [1]any{inner}, want: true},
		{name: "array of scalars", v: [2]int{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasNonStringKey(reflect.ValueOf(tt.v)); got != tt.want {
				t.Fatalf("hasNonStringKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestValidateDoesNotQuoteValues guards against an extra_body value,
// which may be a credential, reaching an error message by way of the
// JSON encoder's own error text.
func TestValidateDoesNotQuoteValues(t *testing.T) {
	s := Settings{ExtraBody: map[string]any{
		"auth": map[string]any{"token": "sk-secret", "bad": math.Inf(1)},
	}}
	err := s.Validate("llm.openai")
	if err == nil {
		t.Fatal("Validate() = nil, want an error for an infinite number")
	}
	if strings.Contains(err.Error(), "sk-secret") || strings.Contains(err.Error(), "Inf") {
		t.Fatalf("Validate() error quotes the value: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "llm.openai.extra_body.auth ") {
		t.Fatalf("Validate() error does not name the key under its section: %v", err)
	}
}

func TestCachingEnabled(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "auto": true} {
		if got := (Settings{PromptCaching: value}).CachingEnabled(); got != want {
			t.Errorf("CachingEnabled() with %q = %v, want %v", value, got, want)
		}
	}
}

func TestApply(t *testing.T) {
	extra := map[string]any{"providerOptions": map[string]any{"gateway": map[string]any{"caching": "auto"}}}
	s := Settings{ReasoningEffort: "high", PromptCaching: "auto", ExtraBody: extra}

	var opts llm.Options
	s.Apply(&opts)
	if opts.ReasoningEffort != llm.ReasoningEffortHigh {
		t.Errorf("ReasoningEffort = %q, want high", opts.ReasoningEffort)
	}
	if opts.PromptCaching != llm.PromptCachingAuto {
		t.Errorf("PromptCaching = %q, want auto", opts.PromptCaching)
	}
	if len(opts.ExtraBody) != 1 || opts.ExtraBody["providerOptions"] == nil {
		t.Fatalf("ExtraBody = %v, want the configured field", opts.ExtraBody)
	}

	// The client gets its own map, so adding a key to it leaves the
	// configuration untouched.
	opts.ExtraBody["added"] = true
	if _, ok := extra["added"]; ok {
		t.Error("Apply shared the configuration's extra_body map with the client")
	}
}

func TestApplyEmptyClearsOptions(t *testing.T) {
	opts := llm.Options{
		ReasoningEffort: llm.ReasoningEffortLow,
		PromptCaching:   llm.PromptCachingAuto,
		ExtraBody:       map[string]any{"x": 1},
	}
	Settings{}.Apply(&opts)
	if opts.ReasoningEffort != "" || opts.PromptCaching != "" || opts.ExtraBody != nil {
		t.Errorf("Apply of empty settings left %+v", opts)
	}
}

// TestInlineYAML checks the keys decode when Settings is inlined in a
// configuration section, which is how the server and alerter carry it.
func TestInlineYAML(t *testing.T) {
	var section struct {
		Model    string   `yaml:"model"`
		Settings Settings `yaml:",inline"`
	}
	in := "model: m\nreasoning_effort: low\nprompt_caching: auto\nextra_body:\n  k: v\n"
	if err := yaml.Unmarshal([]byte(in), &section); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if section.Model != "m" || section.Settings.ReasoningEffort != "low" ||
		section.Settings.PromptCaching != "auto" || section.Settings.ExtraBody["k"] != "v" {
		t.Fatalf("inline decode = %+v", section)
	}
}
