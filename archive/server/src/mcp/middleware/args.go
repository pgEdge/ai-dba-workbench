/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package middleware

import (
    "fmt"
    "time"
)

// ArgParser provides type-safe argument extraction from MCP tool arguments
type ArgParser struct {
    args map[string]interface{}
}

// NewArgParser creates a new argument parser
func NewArgParser(args map[string]interface{}) *ArgParser {
    return &ArgParser{args: args}
}

// GetString extracts a string argument
func (p *ArgParser) GetString(key string) string {
    if v, ok := p.args[key].(string); ok {
        return v
    }
    return ""
}

// GetStringPtr extracts a string pointer argument (nil if empty or missing)
func (p *ArgParser) GetStringPtr(key string) *string {
    if val, ok := p.args[key].(string); ok && val != "" {
        return &val
    }
    return nil
}

// GetBool extracts a boolean argument
func (p *ArgParser) GetBool(key string) bool {
    if v, ok := p.args[key].(bool); ok {
        return v
    }
    return false
}

// GetInt extracts an integer argument (from JSON float64)
func (p *ArgParser) GetInt(key string) int {
    if val, ok := p.args[key].(float64); ok {
        return int(val)
    }
    return 0
}

// GetDate extracts a date argument in YYYY-MM-DD format
func (p *ArgParser) GetDate(key string) (*time.Time, error) {
    if str, ok := p.args[key].(string); ok && str != "" {
        t, err := time.Parse("2006-01-02", str)
        if err != nil {
            return nil, fmt.Errorf("invalid date format for %s: %w", key, err)
        }
        return &t, nil
    }
    return nil, nil
}
