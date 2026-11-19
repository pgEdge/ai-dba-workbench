/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSideEffectingFunction_RequiresConfirmation proves that the query
// endpoint asks for confirmation before running a SELECT that calls a
// side-effecting function, and lists that statement alone as the write.
// The handler answers before it reaches the nil datastore.
func TestSideEffectingFunction_RequiresConfirmation(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC(t)

	query := "SELECT 1; SELECT pg_terminate_backend(pid) FROM pg_stat_activity"
	body, err := json.Marshal(queryRequest{Query: query})
	if err != nil {
		t.Fatalf("failed to encode the request: %v", err)
	}
	req := withSuperuser(httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp multiQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.RequiresConfirmation {
		t.Fatal("expected requires_confirmation for pg_terminate_backend")
	}
	if len(resp.WriteStatements) != 1 ||
		!strings.Contains(resp.WriteStatements[0], "pg_terminate_backend") {
		t.Errorf("write statements = %q, want the pg_terminate_backend statement",
			resp.WriteStatements)
	}
}
