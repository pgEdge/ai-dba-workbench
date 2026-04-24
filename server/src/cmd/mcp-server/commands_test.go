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
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestOpenAuthStoreCLI(t *testing.T) {
	t.Run("prints path to stderr", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Capture stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		store, err := openAuthStoreCLI(tmpDir)
		if err != nil {
			w.Close()
			os.Stderr = oldStderr
			t.Fatalf("Failed to open auth store: %v", err)
		}
		store.Close()

		w.Close()
		var buf bytes.Buffer
		buf.ReadFrom(r)
		os.Stderr = oldStderr

		output := buf.String()
		expectedPath := tmpDir + "/auth.db"
		if !strings.Contains(output, expectedPath) {
			t.Errorf("Expected stderr to contain %q, got %q", expectedPath, output)
		}
		if !strings.HasPrefix(output, "Auth store: ") {
			t.Errorf("Expected stderr to start with 'Auth store: ', got %q", output)
		}
	})

	t.Run("returns error for invalid directory", func(t *testing.T) {
		// Use a path that cannot be created (file as directory)
		tmpDir := t.TempDir()
		blockingFile := tmpDir + "/blocking"
		if err := os.WriteFile(blockingFile, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create blocking file: %v", err)
		}

		_, err := openAuthStoreCLI(blockingFile + "/subdir")
		if err == nil {
			t.Error("Expected error for invalid directory path")
		}
	})
}
