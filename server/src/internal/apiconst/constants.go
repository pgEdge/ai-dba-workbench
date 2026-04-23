/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package apiconst holds shared API constants used by multiple packages.
package apiconst

// OpenAPISpecPath is the path to the OpenAPI specification for
// RFC 8631 API discovery.
const OpenAPISpecPath = "/api/v1/openapi.json"

// MaxRequestBodySize is the maximum allowed size for HTTP request bodies (1MB).
// This prevents denial-of-service attacks via memory exhaustion from large payloads.
const MaxRequestBodySize = 1 << 20 // 1MB
