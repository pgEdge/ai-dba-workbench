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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// DatabaseInfo represents a database connection in API responses
type DatabaseInfo struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	SSLMode  string `json:"sslmode"`
}

// ListDatabasesResponse is the response from GET /api/databases
type ListDatabasesResponse struct {
	Databases []DatabaseInfo `json:"databases"`
	Current   string         `json:"current"`
}

// SelectDatabaseRequest is the request body for POST /api/databases/select
type SelectDatabaseRequest struct {
	Name string `json:"name"`
}

// SelectDatabaseResponse is the response from POST /api/databases/select
type SelectDatabaseResponse struct {
	Success bool   `json:"success"`
	Current string `json:"current,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// getServerKey returns a unique identifier for the current server connection
// Used for storing per-server preferences like selected database
func (c *Client) getServerKey() string {
	if c.config.MCP.Mode == "http" {
		// For HTTP mode, hash the server URL
		hash := sha256.Sum256([]byte(c.config.MCP.URL))
		return hex.EncodeToString(hash[:8]) // First 8 bytes = 16 hex chars
	}
	// For STDIO mode, use "local" or hash of binary path
	if c.config.MCP.ServerPath != "" {
		hash := sha256.Sum256([]byte(c.config.MCP.ServerPath))
		return "local-" + hex.EncodeToString(hash[:4])
	}
	return "local"
}

// restoreDatabasePreference restores the saved database preference for this server
func (c *Client) restoreDatabasePreference(ctx context.Context) {
	serverKey := c.getServerKey()
	savedDB := c.preferences.GetDatabaseForServer(serverKey)
	if savedDB == "" {
		return // No saved preference
	}

	// Try to select the saved database
	if err := c.mcp.SelectDatabase(ctx, savedDB); err != nil {
		// Log but don't fail - database might no longer exist
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Failed to restore saved database %q: %v\n", savedDB, err)
		}
		// Clear the invalid preference
		c.preferences.SetDatabaseForServer(serverKey, "")
		_ = SavePreferences(c.preferences) //nolint:errcheck // Best effort cleanup
	}
}

// handleListDatabases handles /list databases command - lists available databases
func (c *Client) handleListDatabases(ctx context.Context) bool {
	// Use the MCPClient interface method (works for both HTTP and STDIO modes)
	databases, current, err := c.mcp.ListDatabases(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to list databases: %v", err))
		return true
	}

	if len(databases) == 0 {
		c.ui.PrintSystemMessage("No databases available")
		return true
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("Available databases (%d):", len(databases)))
	for _, db := range databases {
		currentMarker := ""
		if db.Name == current {
			currentMarker = " (current)"
		}
		fmt.Printf("  %s%s - %s@%s:%d/%s\n",
			db.Name, currentMarker, db.User, db.Host, db.Port, db.Database)
	}

	return true
}

// handleShowDatabase handles /show database command - shows current database
func (c *Client) handleShowDatabase(ctx context.Context) bool {
	// Use the MCPClient interface method (works for both HTTP and STDIO modes)
	_, current, err := c.mcp.ListDatabases(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to get current database: %v", err))
		return true
	}

	if current == "" {
		c.ui.PrintSystemMessage("No database currently selected")
	} else {
		c.ui.PrintSystemMessage(fmt.Sprintf("Current database: %s", current))
	}

	return true
}

// handleSetDatabase handles /set database <name> command - selects a database
func (c *Client) handleSetDatabase(ctx context.Context, dbName string) bool {
	// Use the MCPClient interface method (works for both HTTP and STDIO modes)
	if err := c.mcp.SelectDatabase(ctx, dbName); err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to select database: %v", err))
		return true
	}

	// Save the preference for this server
	serverKey := c.getServerKey()
	c.preferences.SetDatabaseForServer(serverKey, dbName)
	if err := SavePreferences(c.preferences); err != nil {
		c.ui.PrintError(fmt.Sprintf("Warning: Failed to save preference: %v", err))
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("Database switched to: %s", dbName))

	// Refresh tools since they may be database-specific
	if err := c.refreshCapabilities(ctx); err != nil {
		c.ui.PrintError(fmt.Sprintf("Warning: Failed to refresh capabilities: %v", err))
	}

	return true
}

// refreshCapabilities refreshes tools, resources, and prompts from the server
func (c *Client) refreshCapabilities(ctx context.Context) error {
	tools, err := c.mcp.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}
	c.tools = tools

	resources, err := c.mcp.ListResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to list resources: %w", err)
	}
	c.resources = resources

	prompts, err := c.mcp.ListPrompts(ctx)
	if err != nil {
		return fmt.Errorf("failed to list prompts: %w", err)
	}
	c.prompts = prompts

	return nil
}
