/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package auth

import "time"

// =============================================================================
// RBAC Types
// =============================================================================

// UserGroup represents a hierarchical group for RBAC
type UserGroup struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// GroupMembership represents a membership relationship
// Either member_user_id OR member_group_id is set, not both
type GroupMembership struct {
	ID            int64     `json:"id"`
	ParentGroupID int64     `json:"parent_group_id"`
	MemberUserID  *int64    `json:"member_user_id,omitempty"`
	MemberGroupID *int64    `json:"member_group_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// MCPPrivilege represents a registered MCP privilege identifier
type MCPPrivilege struct {
	ID          int64     `json:"id"`
	Identifier  string    `json:"identifier"`
	ItemType    string    `json:"item_type"` // "tool", "resource", or "prompt"
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// MCPPrivilegeType constants
const (
	MCPPrivilegeTypeTool     = "tool"
	MCPPrivilegeTypeResource = "resource"
	MCPPrivilegeTypePrompt   = "prompt"
)

// Admin permission constants
const (
	PermManageConnections = "manage_connections"
	PermManageGroups      = "manage_groups"
	PermManagePermissions = "manage_permissions"
	PermManageUsers       = "manage_users"
	PermManageTokenScopes = "manage_token_scopes"
	PermManageBlackouts   = "manage_blackouts"
)

// AdminPermissionGrant represents a granted admin permission for a group
type AdminPermissionGrant struct {
	ID         int64  `json:"id"`
	GroupID    int64  `json:"group_id"`
	Permission string `json:"permission"`
	CreatedAt  string `json:"created_at"`
}

// GroupMCPPrivilege represents a group's access to an MCP privilege
type GroupMCPPrivilege struct {
	ID                    int64     `json:"id"`
	GroupID               int64     `json:"group_id"`
	PrivilegeIdentifierID int64     `json:"privilege_identifier_id"`
	CreatedAt             time.Time `json:"created_at"`
}

// ConnectionPrivilege represents a group's access to a database connection
type ConnectionPrivilege struct {
	ID           int64     `json:"id"`
	GroupID      int64     `json:"group_id"`
	ConnectionID int       `json:"connection_id"`
	AccessLevel  string    `json:"access_level"` // "read" or "read_write"
	CreatedAt    time.Time `json:"created_at"`
}

// ConnectionIDAll represents access to all connections
const ConnectionIDAll = 0

// ConnectionAccessLevel constants
const (
	AccessLevelRead      = "read"
	AccessLevelReadWrite = "read_write"
)

// TokenConnectionScope represents a token's restriction to specific connections
type TokenConnectionScope struct {
	ID           int64     `json:"id"`
	TokenID      int64     `json:"token_id"`
	ConnectionID int       `json:"connection_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// TokenMCPScope represents a token's restriction to specific MCP privileges
type TokenMCPScope struct {
	ID                    int64     `json:"id"`
	TokenID               int64     `json:"token_id"`
	PrivilegeIdentifierID int64     `json:"privilege_identifier_id"`
	CreatedAt             time.Time `json:"created_at"`
}

// TokenScope represents all scope restrictions for a token
type TokenScope struct {
	TokenID       int64   `json:"token_id"`
	ConnectionIDs []int   `json:"connection_ids,omitempty"`
	MCPPrivileges []int64 `json:"mcp_privileges,omitempty"`
}

// EffectivePrivileges represents the computed privileges for a user/token
type EffectivePrivileges struct {
	IsSuperuser          bool            `json:"is_superuser"`
	MCPPrivileges        map[string]bool `json:"mcp_privileges"`        // identifier -> allowed
	ConnectionPrivileges map[int]string  `json:"connection_privileges"` // connection_id -> access_level
	AdminPermissions     map[string]bool `json:"admin_permissions"`     // permission -> granted
	TokenScope           *TokenScope     `json:"token_scope,omitempty"` // nil if no scoping
}

// GroupWithMembers represents a group with its membership details
type GroupWithMembers struct {
	Group        UserGroup `json:"group"`
	UserMembers  []string  `json:"user_members,omitempty"`  // usernames
	GroupMembers []string  `json:"group_members,omitempty"` // group names
}

// GroupWithPrivileges represents a group with its assigned privileges
type GroupWithPrivileges struct {
	Group                UserGroup             `json:"group"`
	MCPPrivileges        []MCPPrivilege        `json:"mcp_privileges,omitempty"`
	ConnectionPrivileges []ConnectionPrivilege `json:"connection_privileges,omitempty"`
}
