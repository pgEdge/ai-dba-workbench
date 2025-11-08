/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package config provides shared configuration file parsing utilities
package config

import (
    "bufio"
    "fmt"
    "os"
    "strconv"
    "strings"
)

// KeyHandler is a function that processes a configuration key-value pair
type KeyHandler func(key, value string) error

// Parser handles configuration file parsing with pluggable key handlers
type Parser struct {
    handlers map[string]KeyHandler
}

// NewParser creates a new configuration parser
func NewParser() *Parser {
    return &Parser{
        handlers: make(map[string]KeyHandler),
    }
}

// RegisterHandler registers a handler for a specific configuration key
func (p *Parser) RegisterHandler(key string, handler KeyHandler) {
    p.handlers[key] = handler
}

// ParseFile parses a configuration file and calls registered handlers
func (p *Parser) ParseFile(filename string) error {
    file, err := os.Open(filename) // #nosec G304 - Config file path is provided by administrator
    if err != nil {
        return fmt.Errorf("failed to open config file: %w", err)
    }
    defer func() {
        if cerr := file.Close(); cerr != nil && err == nil {
            err = fmt.Errorf("failed to close config file: %w", cerr)
        }
    }()

    scanner := bufio.NewScanner(file)
    lineNum := 0

    for scanner.Scan() {
        lineNum++
        line := strings.TrimSpace(scanner.Text())

        // Skip empty lines and comments
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }

        // Parse key = value
        parts := strings.SplitN(line, "=", 2)
        if len(parts) != 2 {
            return fmt.Errorf("invalid config line %d: %s", lineNum, line)
        }

        key := strings.TrimSpace(parts[0])
        value := strings.TrimSpace(parts[1])

        // Remove quotes if present
        if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
            value = value[1 : len(value)-1]
        }

        // Call registered handler for this key
        if handler, ok := p.handlers[key]; ok {
            if err := handler(key, value); err != nil {
                return fmt.Errorf("error processing key '%s' on line %d: %w", key, lineNum, err)
            }
        }
        // Silently ignore unknown keys for forward compatibility
    }

    if err := scanner.Err(); err != nil {
        return fmt.Errorf("error reading config file: %w", err)
    }

    return nil
}

// ParseInt converts a string value to an integer
func ParseInt(value string) (int, error) {
    return strconv.Atoi(value)
}

// ParseBool converts a string value to a boolean
func ParseBool(value string) (bool, error) {
    return strconv.ParseBool(value)
}

// ReadPasswordFile reads a password from a file (first line only)
func ReadPasswordFile(filename string) (string, error) {
    file, err := os.Open(filename) // #nosec G304 - Password file path is from config
    if err != nil {
        return "", fmt.Errorf("failed to open password file: %w", err)
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    if scanner.Scan() {
        return scanner.Text(), nil
    }

    if err := scanner.Err(); err != nil {
        return "", fmt.Errorf("failed to read password file: %w", err)
    }

    return "", fmt.Errorf("password file is empty")
}
