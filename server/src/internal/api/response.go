/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package api provides HTTP handlers for the REST API.
// This file contains response helpers implementing RFC 8631 for API discovery.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/apiconst"
)

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// RespondJSON sends a JSON response with RFC 8631 Link header for API discovery.
func RespondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"service-desc\"", apiconst.OpenAPISpecPath))
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// The status line and headers are already flushed, so the client will
		// receive this status with a truncated or empty body. Log loudly: an
		// encode failure here (for example an unmarshalable NaN/Inf float that
		// slipped through upstream sanitization) is a real bug that otherwise
		// manifests only as a mysterious empty 200 response.
		log.Printf("[ERROR] RespondJSON: failed to encode response body (status %d): %v", status, err)
	}
}

// RespondError sends a standardized error response with RFC 8631 Link header.
// Use this for all API error responses to ensure consistent format.
func RespondError(w http.ResponseWriter, status int, message string) {
	RespondJSON(w, status, ErrorResponse{Error: message})
}
