/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package conversations

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendJSON(t *testing.T) {
	rr := httptest.NewRecorder()

	data := map[string]string{"message": "test"}
	sendJSON(rr, http.StatusOK, data)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
	}

	var response map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["message"] != "test" {
		t.Errorf("Expected message 'test', got %q", response["message"])
	}
}

func TestSendError(t *testing.T) {
	rr := httptest.NewRecorder()

	sendError(rr, http.StatusBadRequest, "test error")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["error"] != "test error" {
		t.Errorf("Expected error 'test error', got %q", response["error"])
	}
}

func TestSendJSON_LinkHeader(t *testing.T) {
	rr := httptest.NewRecorder()

	sendJSON(rr, http.StatusOK, map[string]string{})

	link := rr.Header().Get("Link")
	if link == "" {
		t.Error("Expected Link header to be set")
	}
}

func TestSendJSON_ResponseStructure(t *testing.T) {
	rr := httptest.NewRecorder()

	sendJSON(rr, http.StatusOK, map[string]string{"key": "value"})

	var response map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if got, ok := response["key"]; !ok {
		t.Error("Expected 'key' in response body")
	} else if got != "value" {
		t.Errorf("Expected 'value', got %q", got)
	}
}

func TestSendError_ResponseStructure(t *testing.T) {
	rr := httptest.NewRecorder()

	sendError(rr, http.StatusForbidden, "forbidden")

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rr.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, ok := response["error"]; !ok {
		t.Error("Expected 'error' key in response body")
	}
	if response["error"] != "forbidden" {
		t.Errorf("Expected 'forbidden', got %q", response["error"])
	}
	if len(response) != 1 {
		t.Errorf("Expected exactly 1 key in error response, got %d", len(response))
	}
}

func TestHandleList_MethodNotAllowed(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", nil)
	rr := httptest.NewRecorder()
	h.HandleList(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestHandleGet_MethodNotAllowed(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/123", nil)
	rr := httptest.NewRecorder()
	h.HandleGet(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestHandleCreate_MethodNotAllowed(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	rr := httptest.NewRecorder()
	h.HandleCreate(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestHandleUpdate_MethodNotAllowed(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/123", nil)
	rr := httptest.NewRecorder()
	h.HandleUpdate(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestHandleRename_MethodNotAllowed(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/123", nil)
	rr := httptest.NewRecorder()
	h.HandleRename(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestHandleDelete_MethodNotAllowed(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/123", nil)
	rr := httptest.NewRecorder()
	h.HandleDelete(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestHandleDeleteAll_MethodNotAllowed(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	rr := httptest.NewRecorder()
	h.HandleDeleteAll(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}
