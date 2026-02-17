/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package chat

import (
	"errors"
	"strings"
)

// ErrQuit is a sentinel error returned by HandleSlashCommand when the user
// requests to quit. The caller should check for this error and exit the
// chat loop cleanly, allowing deferred cleanup functions to run.
var ErrQuit = errors.New("quit requested")

// SlashCommand represents a parsed slash command
type SlashCommand struct {
	Command string
	Args    []string
}

// ParseSlashCommand parses a slash command from user input.
// Returns nil if the input does not start with "/".
func ParseSlashCommand(input string) *SlashCommand {
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	// Remove the leading slash
	input = strings.TrimPrefix(input, "/")

	// Split into command and arguments, respecting quotes
	parts := ParseQuotedArgs(input)
	if len(parts) == 0 {
		return nil
	}

	return &SlashCommand{
		Command: parts[0],
		Args:    parts[1:],
	}
}

// ParseQuotedArgs splits a string into arguments, respecting quoted strings.
// Single and double quotes are supported; backslash escapes are handled
// inside quoted regions.
func ParseQuotedArgs(input string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	// Convert to runes for proper Unicode handling
	runes := []rune(input)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		switch {
		case (r == '"' || r == '\'') && !inQuote:
			// Start of quoted string
			inQuote = true
			quoteChar = r
		case r == quoteChar && inQuote:
			// End of quoted string
			inQuote = false
			quoteChar = 0
		case r == ' ' && !inQuote:
			// Space outside quotes - end of argument
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		case r == '\\' && inQuote && i+1 < len(runes):
			// Escape sequence in quoted string
			next := runes[i+1]
			if next == quoteChar || next == '\\' {
				// Skip the backslash, include the escaped character
				current.WriteRune(next)
				i++ // Skip the next character since we've already processed it
			} else {
				// Not a valid escape sequence, include the backslash
				current.WriteRune(r)
			}
		default:
			// Regular character
			current.WriteRune(r)
		}
	}

	// Add the last argument if any
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}
