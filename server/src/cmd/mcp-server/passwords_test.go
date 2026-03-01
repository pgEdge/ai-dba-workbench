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
	result, err := ResolvePassword("secret123", true, "")
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
	result, err := ResolvePassword("", true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != PasswordSourceNone {
		t.Errorf("expected PasswordSourceNone, got %d", result.Source)
	}
}

func TestResolvePassword_File(t *testing.T) {
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pwFile, []byte("file-secret\n"), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result, err := ResolvePassword("", false, pwFile)
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
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pwFile, []byte("file-secret\r\n"), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result, err := ResolvePassword("", false, pwFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value != "file-secret" {
		t.Errorf("expected 'file-secret', got %q", result.Value)
	}
}

func TestResolvePassword_FileEmpty(t *testing.T) {
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pwFile, []byte(""), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := ResolvePassword("", false, pwFile)
	if err == nil {
		t.Fatal("expected error for empty password file")
	}
}

func TestResolvePassword_FileMissing(t *testing.T) {
	_, err := ResolvePassword("", false, "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for missing password file")
	}
}

func TestResolvePassword_None(t *testing.T) {
	result, err := ResolvePassword("", false, "")
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

func TestResolvePassword_Precedence_FlagOverFile(t *testing.T) {
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pwFile, []byte("file-value\n"), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result, err := ResolvePassword("flag-value", true, pwFile)
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
