/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package config

import (
	"os"
	"reflect"
	"testing"
)

func TestParseHeadersEnvVar(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "single header",
			input: "X-Custom=value",
			want:  map[string]string{"X-Custom": "value"},
		},
		{
			name:  "multiple headers",
			input: "X-One=val1,X-Two=val2",
			want:  map[string]string{"X-One": "val1", "X-Two": "val2"},
		},
		{
			name:  "value with equals sign",
			input: "X-Token=abc=def=ghi",
			want:  map[string]string{"X-Token": "abc=def=ghi"},
		},
		{
			name:  "empty string",
			input: "",
			want:  map[string]string{},
		},
		{
			name:    "malformed entry",
			input:   "X-NoValue",
			wantErr: false, // should skip malformed entries
			want:    map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHeadersEnvVar(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseHeadersEnvVar() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseHeadersEnvVar() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadHeadersFromFiles(t *testing.T) {
	// Create temp files
	tmpDir := t.TempDir()
	file1 := tmpDir + "/secret1"
	file2 := tmpDir + "/secret2"
	os.WriteFile(file1, []byte("secret-value-1\n"), 0600)
	os.WriteFile(file2, []byte("secret-value-2"), 0600)

	tests := []struct {
		name    string
		files   map[string]string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "load from files",
			files: map[string]string{"X-Secret-1": file1, "X-Secret-2": file2},
			want:  map[string]string{"X-Secret-1": "secret-value-1", "X-Secret-2": "secret-value-2"},
		},
		{
			name:  "empty map",
			files: map[string]string{},
			want:  map[string]string{},
		},
		{
			name:    "missing file",
			files:   map[string]string{"X-Missing": "/nonexistent/file"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadHeadersFromFiles(tt.files)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadHeadersFromFiles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("loadHeadersFromFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeHeaders(t *testing.T) {
	base := map[string]string{"A": "1", "B": "2"}
	override := map[string]string{"B": "override", "C": "3"}

	result := mergeHeaders(base, override)

	expected := map[string]string{"A": "1", "B": "override", "C": "3"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("mergeHeaders() = %v, want %v", result, expected)
	}
}

func TestMergeHeaders_NilInputs(t *testing.T) {
	result := mergeHeaders(nil, nil)
	if result == nil || len(result) != 0 {
		t.Errorf("mergeHeaders(nil, nil) should return empty map, got %v", result)
	}

	result = mergeHeaders(map[string]string{"A": "1"}, nil)
	if result["A"] != "1" {
		t.Errorf("mergeHeaders with nil second arg should preserve first")
	}
}

func TestLLMConfig_LoadCustomHeaders(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := tmpDir + "/secret"
	os.WriteFile(secretFile, []byte("file-secret"), 0600)

	cfg := &LLMConfig{
		CustomHeaders:      map[string]string{"X-Yaml": "yaml-value"},
		CustomHeadersFiles: map[string]string{"X-File": secretFile},
	}

	// Set env var
	os.Setenv("LLM_CUSTOM_HEADERS", "X-Env=env-value,X-Yaml=env-override")
	defer os.Unsetenv("LLM_CUSTOM_HEADERS")

	headers, err := cfg.LoadCustomHeaders()
	if err != nil {
		t.Fatalf("LoadCustomHeaders() error = %v", err)
	}

	// Check precedence: env > file > yaml
	if headers["X-Yaml"] != "env-override" {
		t.Errorf("env should override yaml, got %s", headers["X-Yaml"])
	}
	if headers["X-File"] != "file-secret" {
		t.Errorf("expected file header, got %s", headers["X-File"])
	}
	if headers["X-Env"] != "env-value" {
		t.Errorf("expected env header, got %s", headers["X-Env"])
	}
}

func TestLLMConfig_GetProviderHeaders(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := tmpDir + "/secret"
	os.WriteFile(secretFile, []byte("file-secret"), 0600)

	cfg := &LLMConfig{
		CustomHeaders:          map[string]string{"X-Global": "global"},
		CustomHeadersFiles:     map[string]string{"X-File": secretFile},
		OpenAICustomHeaders:    map[string]string{"X-Provider": "openai"},
		AnthropicCustomHeaders: map[string]string{"X-Provider": "anthropic"},
	}

	// Test OpenAI headers
	os.Setenv("LLM_OPENAI_CUSTOM_HEADERS", "X-Env=openai-env")
	defer os.Unsetenv("LLM_OPENAI_CUSTOM_HEADERS")

	headers, err := cfg.GetProviderHeaders("openai")
	if err != nil {
		t.Fatalf("GetProviderHeaders() error = %v", err)
	}

	if headers["X-Global"] != "global" {
		t.Errorf("expected global header, got %s", headers["X-Global"])
	}
	if headers["X-Provider"] != "openai" {
		t.Errorf("expected provider header, got %s", headers["X-Provider"])
	}
	if headers["X-Env"] != "openai-env" {
		t.Errorf("expected env header, got %s", headers["X-Env"])
	}
	if headers["X-File"] != "file-secret" {
		t.Errorf("expected file header, got %s", headers["X-File"])
	}
}
