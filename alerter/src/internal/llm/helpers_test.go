/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package llm

import (
	"io"
	"testing"
)

// writeOrFail writes body to w and fails the test on error. It exists so
// errcheck's check-blank rule doesn't flag test servers that discard the
// Write return value.
func writeOrFail(t *testing.T, w io.Writer, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write: %v", err)
	}
}
