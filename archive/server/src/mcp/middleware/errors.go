/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package middleware provides MCP request/response processing helpers
package middleware

import "errors"

// Common errors for MCP operations
var (
    ErrAuthenticationRequired = errors.New("authentication required")
    ErrDatabaseRequired       = errors.New("database connection required")
    ErrSuperuserRequired      = errors.New("permission denied: superuser privileges required")
    ErrInsufficientPrivileges = errors.New("permission denied: insufficient privileges")
)
