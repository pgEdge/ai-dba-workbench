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
	"net/http"
	"os"
)

// OpenAPISpecPath is the path to the OpenAPI specification for RFC 8631 API discovery.
const OpenAPISpecPath = "/api/v1/openapi.json"

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// RespondJSON sends a JSON response with RFC 8631 Link header for API discovery.
func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"service-desc\"", OpenAPISpecPath))
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to encode JSON response: %v\n", err)
	}
}

// RespondError sends a standardized error response with RFC 8631 Link header.
// Use this for all API error responses to ensure consistent format.
func RespondError(w http.ResponseWriter, status int, message string) {
	RespondJSON(w, status, ErrorResponse{Error: message})
}
