/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePassword_Flag(t *testing.T) {
	// Override env lookup to return nothing
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) { return "", false }
	defer func() { envLookupFunc = origLookup }()

	result, err := ResolvePassword("secret123", true, "TEST_PASSWORD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceFlag {
		t.Errorf("expected PasswordSourceFlag, got %d", result.Source)
	}
	if result.Value != "secret123" {
		t.Errorf("expected 'secret123', got %q", result.Value)
	}
}

func TestResolvePassword_FlagEmpty(t *testing.T) {
	// An empty flag value should not count as "set"
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) { return "", false }
	defer func() { envLookupFunc = origLookup }()

	result, err := ResolvePassword("", true, "TEST_PASSWORD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceNone {
		t.Errorf("expected PasswordSourceNone, got %d", result.Source)
	}
}

func TestResolvePassword_Env(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) {
		if key == "AIDBA_DB_PASSWORD" {
			return "env-secret", true
		}
		return "", false
	}
	defer func() { envLookupFunc = origLookup }()

	result, err := ResolvePassword("", false, "AIDBA_DB_PASSWORD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceEnv {
		t.Errorf("expected PasswordSourceEnv, got %d", result.Source)
	}
	if result.Value != "env-secret" {
		t.Errorf("expected 'env-secret', got %q", result.Value)
	}
}

func TestResolvePassword_EnvEmpty(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) {
		if key == "AIDBA_DB_PASSWORD" {
			return "", true
		}
		return "", false
	}
	defer func() { envLookupFunc = origLookup }()

	result, err := ResolvePassword("", false, "AIDBA_DB_PASSWORD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceNone {
		t.Errorf("expected PasswordSourceNone for empty env, got %d", result.Source)
	}
}

func TestResolvePassword_File(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) { return "", false }
	defer func() { envLookupFunc = origLookup }()

	dir := t.TempDir()
	pwFile := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pwFile, []byte("file-secret\n"), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result, err := ResolvePassword("", false, "AIDBA_DB_PASSWORD", pwFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceFile {
		t.Errorf("expected PasswordSourceFile, got %d", result.Source)
	}
	if result.Value != "file-secret" {
		t.Errorf("expected 'file-secret', got %q", result.Value)
	}
}

func TestResolvePassword_FileWindowsLineEnding(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) { return "", false }
	defer func() { envLookupFunc = origLookup }()

	dir := t.TempDir()
	pwFile := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pwFile, []byte("file-secret\r\n"), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result, err := ResolvePassword("", false, "AIDBA_DB_PASSWORD", pwFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value != "file-secret" {
		t.Errorf("expected 'file-secret', got %q", result.Value)
	}
}

func TestResolvePassword_FileEmpty(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) { return "", false }
	defer func() { envLookupFunc = origLookup }()

	dir := t.TempDir()
	pwFile := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pwFile, []byte(""), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := ResolvePassword("", false, "AIDBA_DB_PASSWORD", pwFile)
	if err == nil {
		t.Fatal("expected error for empty password file")
	}
}

func TestResolvePassword_FileMissing(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) { return "", false }
	defer func() { envLookupFunc = origLookup }()

	_, err := ResolvePassword("", false, "AIDBA_DB_PASSWORD", "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for missing password file")
	}
}

func TestResolvePassword_None(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) { return "", false }
	defer func() { envLookupFunc = origLookup }()

	result, err := ResolvePassword("", false, "AIDBA_DB_PASSWORD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceNone {
		t.Errorf("expected PasswordSourceNone, got %d", result.Source)
	}
	if result.Value != "" {
		t.Errorf("expected empty value, got %q", result.Value)
	}
}

func TestResolvePassword_Precedence_FlagOverEnv(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) {
		if key == "AIDBA_DB_PASSWORD" {
			return "env-value", true
		}
		return "", false
	}
	defer func() { envLookupFunc = origLookup }()

	result, err := ResolvePassword("flag-value", true, "AIDBA_DB_PASSWORD", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceFlag {
		t.Errorf("expected PasswordSourceFlag, got %d", result.Source)
	}
	if result.Value != "flag-value" {
		t.Errorf("expected 'flag-value', got %q", result.Value)
	}
}

func TestResolvePassword_Precedence_EnvOverFile(t *testing.T) {
	origLookup := envLookupFunc
	envLookupFunc = func(key string) (string, bool) {
		if key == "AIDBA_DB_PASSWORD" {
			return "env-value", true
		}
		return "", false
	}
	defer func() { envLookupFunc = origLookup }()

	dir := t.TempDir()
	pwFile := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pwFile, []byte("file-value\n"), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result, err := ResolvePassword("", false, "AIDBA_DB_PASSWORD", pwFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceEnv {
		t.Errorf("expected PasswordSourceEnv, got %d", result.Source)
	}
	if result.Value != "env-value" {
		t.Errorf("expected 'env-value', got %q", result.Value)
	}
}
