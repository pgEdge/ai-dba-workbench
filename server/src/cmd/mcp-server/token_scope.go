/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package main

import (
    "fmt"
    "strings"

    "github.com/pgedge/ai-workbench/server/internal/auth"
)

// scopeTokenConnectionsCommand sets the connection scope for a token
func scopeTokenConnectionsCommand(dataDir string, tokenID int64, connectionIDs string) error {
    if tokenID <= 0 {
        return fmt.Errorf("valid token ID is required")
    }

    // Open auth store
    store, err := auth.NewAuthStore(dataDir, 0, 0)
    if err != nil {
        return fmt.Errorf("failed to open auth store: %w", err)
    }
    defer store.Close()

    // Parse connection IDs
    ids, err := parseConnectionIDs(connectionIDs)
    if err != nil {
        return fmt.Errorf("failed to parse connection IDs: %w", err)
    }

    // Set token connection scope
    if err := store.SetTokenConnectionScope(tokenID, ids); err != nil {
        return fmt.Errorf("failed to set token connection scope: %w", err)
    }

    if len(ids) == 0 {
        fmt.Printf("Cleared connection scope for token %d (no restrictions)\n", tokenID)
    } else {
        fmt.Printf("Set connection scope for token %d: %v\n", tokenID, ids)
    }

    return nil
}

// scopeTokenToolsCommand sets the MCP privilege scope for a token using tool names
func scopeTokenToolsCommand(dataDir string, tokenID int64, tools string) error {
    if tokenID <= 0 {
        return fmt.Errorf("valid token ID is required")
    }

    // Open auth store
    store, err := auth.NewAuthStore(dataDir, 0, 0)
    if err != nil {
        return fmt.Errorf("failed to open auth store: %w", err)
    }
    defer store.Close()

    // Parse tool names
    var toolNames []string
    if tools != "" {
        for _, t := range strings.Split(tools, ",") {
            t = strings.TrimSpace(t)
            if t != "" {
                toolNames = append(toolNames, t)
            }
        }
    }

    // Set token MCP scope by names
    if err := store.SetTokenMCPScopeByNames(tokenID, toolNames); err != nil {
        return fmt.Errorf("failed to set token MCP scope: %w", err)
    }

    if len(toolNames) == 0 {
        fmt.Printf("Cleared MCP scope for token %d (no restrictions)\n", tokenID)
    } else {
        fmt.Printf("Set MCP scope for token %d: %v\n", tokenID, toolNames)
    }

    return nil
}

// clearTokenScopeCommand clears all scope restrictions from a token
func clearTokenScopeCommand(dataDir string, tokenID int64) error {
    if tokenID <= 0 {
        return fmt.Errorf("valid token ID is required")
    }

    // Open auth store
    store, err := auth.NewAuthStore(dataDir, 0, 0)
    if err != nil {
        return fmt.Errorf("failed to open auth store: %w", err)
    }
    defer store.Close()

    // Clear token scope
    if err := store.ClearTokenScope(tokenID); err != nil {
        return fmt.Errorf("failed to clear token scope: %w", err)
    }

    fmt.Printf("Cleared all scope restrictions for token %d\n", tokenID)
    return nil
}

// showTokenScopeCommand displays the current scope for a token
func showTokenScopeCommand(dataDir string, tokenID int64) error {
    if tokenID <= 0 {
        return fmt.Errorf("valid token ID is required")
    }

    // Open auth store
    store, err := auth.NewAuthStore(dataDir, 0, 0)
    if err != nil {
        return fmt.Errorf("failed to open auth store: %w", err)
    }
    defer store.Close()

    // Get token scope
    scope, err := store.GetTokenScope(tokenID)
    if err != nil {
        return fmt.Errorf("failed to get token scope: %w", err)
    }

    fmt.Printf("\nScope for token %d:\n", tokenID)
    fmt.Println(strings.Repeat("=", 60))

    if scope == nil {
        fmt.Println("No scope restrictions (full access to user's privileges)")
    } else {
        // Connection scope
        if len(scope.ConnectionIDs) > 0 {
            fmt.Println("\nConnection Scope (restricted to):")
            for _, connID := range scope.ConnectionIDs {
                fmt.Printf("  - Connection %d\n", connID)
            }
        } else {
            fmt.Println("\nConnection Scope: Unrestricted")
        }

        // MCP scope - need to resolve privilege IDs to names
        if len(scope.MCPPrivileges) > 0 {
            fmt.Println("\nMCP Privilege Scope (restricted to):")
            mcpScope, err := store.GetTokenMCPScope(tokenID)
            if err != nil {
                fmt.Printf("  Error resolving privilege names: %v\n", err)
            } else {
                for _, name := range mcpScope {
                    fmt.Printf("  - %s\n", name)
                }
            }
        } else {
            fmt.Println("\nMCP Privilege Scope: Unrestricted")
        }
    }

    fmt.Println(strings.Repeat("=", 60) + "\n")

    return nil
}
