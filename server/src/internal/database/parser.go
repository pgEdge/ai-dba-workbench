/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"regexp"
	"strings"
)

// Package-level compiled regexes for query parsing
var (
	reConnString = regexp.MustCompile(`postgres(?:ql)?://[^\s'"]+`)
	reAtPattern  = regexp.MustCompile(`(?i)\s+(?:at|from|on|for|in)\s+(postgres(?:ql)?://[^\s'"]+)`)
	reDBPattern  = regexp.MustCompile(`(?i)^(?:database|db)\s+(postgres(?:ql)?://[^\s'"]+)\s+`)
)

// QueryContext contains information parsed from a natural language query
type QueryContext struct {
	CleanedQuery     string // The query with connection string references removed
	ConnectionString string // The extracted connection string (empty if none)
	SetAsDefault     bool   // Whether to set this as the new default connection
}

// ParseQueryForConnection extracts connection string and intent from a natural language query
func ParseQueryForConnection(query string) *QueryContext {
	ctx := &QueryContext{
		CleanedQuery: query,
	}

	// Use pre-compiled patterns for detecting connection strings

	// Check for "set default" or "use database" commands
	lowerQuery := strings.ToLower(query)

	// Pattern: "set default database to postgres://..."
	if strings.Contains(lowerQuery, "set default") ||
		strings.Contains(lowerQuery, "use database") ||
		strings.Contains(lowerQuery, "switch to") {
		ctx.SetAsDefault = true

		// Extract the connection string
		if match := reConnString.FindString(query); match != "" {
			ctx.ConnectionString = match
			// Remove the command from the query
			ctx.CleanedQuery = ""
		}
		return ctx
	}

	// Pattern: "... at postgres://..." or "... from postgres://..." or "... on postgres://..."
	if matches := reAtPattern.FindStringSubmatch(query); len(matches) > 1 {
		ctx.ConnectionString = matches[1]
		// Remove the connection string reference from the query
		ctx.CleanedQuery = reAtPattern.ReplaceAllString(query, "")
		ctx.CleanedQuery = strings.TrimSpace(ctx.CleanedQuery)
		return ctx
	}

	// Pattern: "database postgres://... " at the beginning
	if matches := reDBPattern.FindStringSubmatch(query); len(matches) > 1 {
		ctx.ConnectionString = matches[1]
		// Remove the database prefix from the query
		ctx.CleanedQuery = reDBPattern.ReplaceAllString(query, "")
		ctx.CleanedQuery = strings.TrimSpace(ctx.CleanedQuery)
		return ctx
	}

	return ctx
}

// IsSetDefaultCommand checks if the query is a command to set the default database
func IsSetDefaultCommand(query string) bool {
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	return strings.HasPrefix(lowerQuery, "set default") ||
		strings.HasPrefix(lowerQuery, "use database") ||
		strings.HasPrefix(lowerQuery, "switch to database") ||
		strings.HasPrefix(lowerQuery, "change database to")
}
