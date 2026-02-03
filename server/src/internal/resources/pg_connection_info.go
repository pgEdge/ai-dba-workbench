/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package resources

import (
	"encoding/json"

	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// ConnectionInfo represents the current database connection context
type ConnectionInfo struct {
	Connected      bool    `json:"connected"`                 // Whether a connection is currently selected
	ConnectionID   *int    `json:"connection_id,omitempty"`   // ID of the selected connection in the datastore
	ConnectionName *string `json:"connection_name,omitempty"` // User-friendly name of the connection
	Host           *string `json:"host,omitempty"`            // PostgreSQL server host
	Port           *int    `json:"port,omitempty"`            // PostgreSQL server port
	DatabaseName   *string `json:"database_name,omitempty"`   // Currently connected database name
	Username       *string `json:"username,omitempty"`        // Database username
	IsMonitored    *bool   `json:"is_monitored,omitempty"`    // Whether this connection is being monitored by the collector
	Message        string  `json:"message"`                   // Human-readable status message
}

// ConnectionInfoResourceDefinition returns the MCP resource definition for connection info
func ConnectionInfoResourceDefinition() mcp.Resource {
	return mcp.Resource{
		URI:  URIConnectionInfo,
		Name: "Current Connection Information",
		Description: `Information about the currently selected database connection.

<usecase>
Use this resource to:
- Check which database connection is currently active
- Get the connection_id for use with datastore tools (query_metrics, etc.)
- Verify you're connected to the expected server before running queries
- Get connection details to communicate to the user
- Determine if a connection needs to be selected
</usecase>

<important>
Reading this resource does NOT connect to the monitored database - it only retrieves
metadata about the currently selected connection from the session state. Use this
to get the connection_id when you need to query the datastore for metrics about
"the current database" without actually connecting to it.
</important>

<provided_info>
Returns JSON with:
- connected: Boolean indicating if a connection is selected
- connection_id: Numeric ID - use this with query_metrics connection_id parameter
- connection_name: User-friendly name for the connection
- host: PostgreSQL server hostname
- port: PostgreSQL server port number
- database_name: Name of the connected database
- username: Database user for the connection
- is_monitored: Whether the collector is gathering metrics from this server
- message: Human-readable status message
</provided_info>

<when_not_connected>
If no connection is selected, the response will indicate this with:
- connected: false
- message: Instructions for selecting a connection

CRITICAL: When connected: false, you MUST ask the user which connection to use.
DO NOT arbitrarily pick connections. Ask: "You don't have a database selected.
Which connection would you like me to analyze?" and wait for their response.

Guide the user to select a connection using their client interface
(CLI: /connect command, Web: connection selector).
</when_not_connected>

<examples>
- User asks "analyze my database" → read this resource to get connection_id, then use query_metrics
- Before running any database query, check this resource
- When a tool returns "no database connection selected", read this resource
- To help users understand which database they're querying
</examples>`,
		MimeType: "application/json",
	}
}

// BuildConnectionInfoResponse creates a ResourceContent response from ConnectionInfo
func BuildConnectionInfoResponse(info *ConnectionInfo) (mcp.ResourceContent, error) {
	jsonData, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return mcp.ResourceContent{}, err
	}

	return mcp.ResourceContent{
		URI: URIConnectionInfo,
		Contents: []mcp.ContentItem{
			{
				Type: "text",
				Text: string(jsonData),
			},
		},
	}, nil
}

// NewNoConnectionInfo creates a ConnectionInfo indicating no connection is selected
func NewNoConnectionInfo() *ConnectionInfo {
	return &ConnectionInfo{
		Connected: false,
		Message:   "No database connection selected. Please select a connection using your client interface (CLI: /connect, Web: connection selector).",
	}
}

// NewConnectionInfo creates a ConnectionInfo with connection details
func NewConnectionInfo(connectionID int, name, host string, port int, databaseName, username string, isMonitored bool) *ConnectionInfo {
	return &ConnectionInfo{
		Connected:      true,
		ConnectionID:   &connectionID,
		ConnectionName: &name,
		Host:           &host,
		Port:           &port,
		DatabaseName:   &databaseName,
		Username:       &username,
		IsMonitored:    &isMonitored,
		Message:        "Connected to " + name + " (" + host + ":" + itoa(port) + "/" + databaseName + ")",
	}
}

// itoa converts int to string (simple helper to avoid importing strconv)
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	result := ""
	negative := i < 0
	if negative {
		i = -i
	}
	for i > 0 {
		result = string(rune('0'+i%10)) + result
		i /= 10
	}
	if negative {
		result = "-" + result
	}
	return result
}
