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
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLog redirects the standard logger output to a buffer so tests
// can assert on the "[ERROR] Failed to <verb>..." log line emitted by
// respondDBError without polluting test output.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	buf := &bytes.Buffer{}
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})
	return buf
}

func decodeErrorResponse(t *testing.T, body io.Reader) ErrorResponse {
	t.Helper()
	var er ErrorResponse
	if err := json.NewDecoder(body).Decode(&er); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return er
}

func TestRespondDBError_NilReturnsFalse(t *testing.T) {
	rec := httptest.NewRecorder()
	if respondDBError(rec, nil, "fetch widget",
		notFound(errors.New("sentinel"), "missing")) {
		t.Fatalf("expected false when err is nil")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 when no response was written, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body when err is nil, got %q", rec.Body.String())
	}
}

func TestRespondDBError_NotFoundSentinel(t *testing.T) {
	sentinel := errors.New("not found sentinel")
	wrapped := fmt.Errorf("layer 1: %w", sentinel)

	rec := httptest.NewRecorder()
	logBuf := captureLog(t)
	if !respondDBError(rec, wrapped, "fetch widget",
		notFound(sentinel, "Widget not found")) {
		t.Fatalf("expected true when response was written")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	body := decodeErrorResponse(t, rec.Body)
	if body.Error != "Widget not found" {
		t.Fatalf("expected not-found message verbatim, got %q", body.Error)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("not-found path must not emit a log line, got %q", logBuf.String())
	}
}

func TestRespondDBError_InternalServerError(t *testing.T) {
	other := errors.New("some database failure")
	rec := httptest.NewRecorder()
	logBuf := captureLog(t)
	sentinel := errors.New("sentinel")
	if !respondDBError(rec, other, "fetch widget",
		notFound(sentinel, "Widget not found")) {
		t.Fatalf("expected true when response was written")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	body := decodeErrorResponse(t, rec.Body)
	if body.Error != "Failed to fetch widget" {
		t.Fatalf("expected verb-driven 500 body, got %q", body.Error)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "[ERROR] Failed to fetch widget:") {
		t.Fatalf("expected log line with [ERROR] prefix and verb, got %q", logged)
	}
	if !strings.Contains(logged, "some database failure") {
		t.Fatalf("expected log line to include underlying error text, got %q", logged)
	}
}

func TestRespondDBError_NoMappingsTreatsAllAs500(t *testing.T) {
	// When the caller passes zero mappings, even an error that would
	// otherwise be a not-found sentinel must yield a 500. This is the
	// "no 404 path at all" mode used by create/list/save endpoints.
	would := errors.New("would-be sentinel")
	rec := httptest.NewRecorder()
	logBuf := captureLog(t)
	if !respondDBError(rec, would, "delete widget") {
		t.Fatalf("expected true when response was written")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when no mappings supplied, got %d", rec.Code)
	}
	body := decodeErrorResponse(t, rec.Body)
	if body.Error != "Failed to delete widget" {
		t.Fatalf("expected 500 body, got %q", body.Error)
	}
	if !strings.Contains(logBuf.String(), "Failed to delete widget") {
		t.Fatalf("expected log to include verb, got %q", logBuf.String())
	}
}

func TestRespondDBError_MultipleSentinels_FirstWins(t *testing.T) {
	// When multiple mappings are supplied, the first matching sentinel
	// wins. This protects callers that map two distinct not-found
	// sentinels (e.g. ErrClusterNotFound and ErrConnectionNotFound) to
	// distinct 404 messages.
	first := errors.New("first sentinel")
	second := errors.New("second sentinel")

	rec := httptest.NewRecorder()
	logBuf := captureLog(t)
	if !respondDBError(rec, second, "add server",
		notFound(first, "Cluster not found"),
		notFound(second, "Connection not found")) {
		t.Fatalf("expected true when response was written")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for second-sentinel match, got %d", rec.Code)
	}
	body := decodeErrorResponse(t, rec.Body)
	if body.Error != "Connection not found" {
		t.Fatalf("expected second mapping's message, got %q", body.Error)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("not-found path must not emit a log line, got %q", logBuf.String())
	}
}

func TestRespondDBError_MultipleSentinels_FallthroughTo500(t *testing.T) {
	// If none of the supplied mappings match, the helper logs and
	// returns a 500 using the verb-driven phrasing.
	first := errors.New("first sentinel")
	second := errors.New("second sentinel")
	other := errors.New("transient failure")

	rec := httptest.NewRecorder()
	logBuf := captureLog(t)
	if !respondDBError(rec, other, "add server",
		notFound(first, "Cluster not found"),
		notFound(second, "Connection not found")) {
		t.Fatalf("expected true when response was written")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when no sentinel matches, got %d", rec.Code)
	}
	body := decodeErrorResponse(t, rec.Body)
	if body.Error != "Failed to add server" {
		t.Fatalf("expected verb-driven 500 body, got %q", body.Error)
	}
	if !strings.Contains(logBuf.String(), "[ERROR] Failed to add server:") {
		t.Fatalf("expected log line with [ERROR] prefix and verb, got %q", logBuf.String())
	}
}

func TestRespondDBError_NilSentinelInMappingIgnored(t *testing.T) {
	// A mapping with a nil Sentinel must not match every error; it is
	// silently ignored. This guards against subtle bugs where callers
	// construct mappings dynamically and a sentinel ends up nil.
	rec := httptest.NewRecorder()
	logBuf := captureLog(t)
	if !respondDBError(rec, errors.New("boom"), "do thing",
		dbErrorMapping{Sentinel: nil, Message: "ignored"}) {
		t.Fatalf("expected true when response was written")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when nil-sentinel mapping is ignored, got %d", rec.Code)
	}
	if !strings.Contains(logBuf.String(), "Failed to do thing") {
		t.Fatalf("expected log to include verb, got %q", logBuf.String())
	}
}

func TestDecodeBody_Success(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	body := strings.NewReader(`{"name":"alice","age":42}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	rec := httptest.NewRecorder()
	got, ok := decodeBody[payload](rec, r)
	if !ok {
		t.Fatalf("expected decodeBody to succeed, response body=%q", rec.Body.String())
	}
	if got.Name != "alice" || got.Age != 42 {
		t.Fatalf("unexpected decoded value: %+v", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected no response written, got status %d", rec.Code)
	}
}

func TestDecodeBody_InvalidJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	body := strings.NewReader(`{not json`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	rec := httptest.NewRecorder()
	got, ok := decodeBody[payload](rec, r)
	if ok {
		t.Fatalf("expected decodeBody to fail on bad JSON")
	}
	if got.Name != "" {
		t.Fatalf("expected zero value on failure, got %+v", got)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 written by DecodeJSONBody, got %d", rec.Code)
	}
	resp := decodeErrorResponse(t, rec.Body)
	if resp.Error != "Invalid request body" {
		t.Fatalf("expected the standard invalid-body message, got %q", resp.Error)
	}
}
