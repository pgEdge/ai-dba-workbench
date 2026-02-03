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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRespondJSON(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		data           interface{}
		expectedStatus int
		checkBody      func(t *testing.T, body string)
	}{
		{
			name:           "success response with map",
			status:         http.StatusOK,
			data:           map[string]string{"message": "success"},
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				var resp map[string]string
				if err := json.Unmarshal([]byte(body), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp["message"] != "success" {
					t.Errorf("Expected message 'success', got %q", resp["message"])
				}
			},
		},
		{
			name:   "created response with struct",
			status: http.StatusCreated,
			data: struct {
				ID int `json:"id"`
			}{ID: 42},
			expectedStatus: http.StatusCreated,
			checkBody: func(t *testing.T, body string) {
				var resp struct {
					ID int `json:"id"`
				}
				if err := json.Unmarshal([]byte(body), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp.ID != 42 {
					t.Errorf("Expected ID 42, got %d", resp.ID)
				}
			},
		},
		{
			name:           "response with array",
			status:         http.StatusOK,
			data:           []string{"item1", "item2"},
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				var resp []string
				if err := json.Unmarshal([]byte(body), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if len(resp) != 2 {
					t.Errorf("Expected 2 items, got %d", len(resp))
				}
			},
		},
		{
			name:           "response with nil data",
			status:         http.StatusOK,
			data:           nil,
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				if strings.TrimSpace(body) != "null" {
					t.Errorf("Expected 'null', got %q", body)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			RespondJSON(rec, tt.status, tt.data)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			contentType := rec.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
			}

			linkHeader := rec.Header().Get("Link")
			expectedLink := "<" + OpenAPISpecPath + ">; rel=\"service-desc\""
			if linkHeader != expectedLink {
				t.Errorf("Expected Link header %q, got %q", expectedLink, linkHeader)
			}

			if tt.checkBody != nil {
				tt.checkBody(t, rec.Body.String())
			}
		})
	}
}

func TestRespondError(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		message        string
		expectedStatus int
	}{
		{
			name:           "bad request error",
			status:         http.StatusBadRequest,
			message:        "Invalid input",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "not found error",
			status:         http.StatusNotFound,
			message:        "Resource not found",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "internal server error",
			status:         http.StatusInternalServerError,
			message:        "Something went wrong",
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "forbidden error",
			status:         http.StatusForbidden,
			message:        "Access denied",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "service unavailable",
			status:         http.StatusServiceUnavailable,
			message:        "Service is down",
			expectedStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			RespondError(rec, tt.status, tt.message)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			contentType := rec.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
			}

			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			if resp.Error != tt.message {
				t.Errorf("Expected error message %q, got %q", tt.message, resp.Error)
			}
		})
	}
}

func TestErrorResponse_JSON(t *testing.T) {
	resp := ErrorResponse{Error: "Test error message"}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal ErrorResponse: %v", err)
	}

	var decoded ErrorResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ErrorResponse: %v", err)
	}

	if decoded.Error != resp.Error {
		t.Errorf("Expected error %q, got %q", resp.Error, decoded.Error)
	}

	// Verify JSON structure
	var rawJSON map[string]interface{}
	if err := json.Unmarshal(data, &rawJSON); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	if _, ok := rawJSON["error"]; !ok {
		t.Error("Expected 'error' key in JSON")
	}
}

func TestOpenAPISpecPath(t *testing.T) {
	if OpenAPISpecPath != "/api/v1/openapi.json" {
		t.Errorf("Expected OpenAPISpecPath to be '/api/v1/openapi.json', got %q", OpenAPISpecPath)
	}
}
