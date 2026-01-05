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
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/pgEdge/ai-workbench/server/src/privileges"
)

// UserInfo holds authentication information for a request
type UserInfo struct {
    Username       string
    IsAuthenticated bool
    IsSuperuser    bool
    IsServiceToken bool
}

// AuthChecker provides authentication and authorization checking
type AuthChecker struct {
    dbPool   *pgxpool.Pool
    userInfo *UserInfo
    userID   int // Cached user ID
}

// NewAuthChecker creates a new authentication checker
func NewAuthChecker(dbPool *pgxpool.Pool, userInfo *UserInfo) *AuthChecker {
    return &AuthChecker{
        dbPool:   dbPool,
        userInfo: userInfo,
    }
}

// RequireAuthentication checks if the user is authenticated
func (ac *AuthChecker) RequireAuthentication() error {
    if ac.userInfo == nil || !ac.userInfo.IsAuthenticated {
        return ErrAuthenticationRequired
    }
    return nil
}

// RequireDatabase checks if a database connection is available
func (ac *AuthChecker) RequireDatabase() error {
    if ac.dbPool == nil {
        return ErrDatabaseRequired
    }
    return nil
}

// RequireSuperuser checks if the user is a superuser
func (ac *AuthChecker) RequireSuperuser() error {
    if err := ac.RequireAuthentication(); err != nil {
        return err
    }
    if !ac.userInfo.IsSuperuser {
        return ErrSuperuserRequired
    }
    return nil
}

// RequireSuperuserOrPrivilege checks if the user is a superuser or has the specified privilege
func (ac *AuthChecker) RequireSuperuserOrPrivilege(ctx context.Context, privilegeID string) error {
    if err := ac.RequireAuthentication(); err != nil {
        return err
    }

    // Superusers bypass all privilege checks
    if ac.userInfo.IsSuperuser {
        return nil
    }

    if err := ac.RequireDatabase(); err != nil {
        return err
    }

    // Service tokens must be superusers for privileged operations
    if ac.userInfo.IsServiceToken {
        return ErrSuperuserRequired
    }

    // Get user ID (cached)
    userID, err := ac.GetUserID(ctx)
    if err != nil {
        return err
    }

    // Check privilege via group membership
    canAccess, err := privileges.CanAccessMCPItem(ctx, ac.dbPool, userID, privilegeID)
    if err != nil {
        return fmt.Errorf("failed to check privileges: %w", err)
    }
    if !canAccess {
        return ErrInsufficientPrivileges
    }

    return nil
}

// RequireOwnerOrSuperuser checks if the user is the owner of a resource or a superuser
func (ac *AuthChecker) RequireOwnerOrSuperuser(username string) error {
    if err := ac.RequireAuthentication(); err != nil {
        return err
    }

    if ac.userInfo.IsSuperuser {
        return nil
    }

    if ac.userInfo.Username != username {
        return fmt.Errorf("permission denied: can only access your own resources")
    }

    return nil
}

// GetUserID returns the user ID (cached for performance)
func (ac *AuthChecker) GetUserID(ctx context.Context) (int, error) {
    // Return cached ID if available
    if ac.userID != 0 {
        return ac.userID, nil
    }

    if ac.dbPool == nil {
        return 0, ErrDatabaseRequired
    }

    // Query and cache user ID
    err := ac.dbPool.QueryRow(ctx,
        "SELECT id FROM user_accounts WHERE username = $1",
        ac.userInfo.Username).Scan(&ac.userID)
    if err != nil {
        return 0, fmt.Errorf("failed to get user ID: %w", err)
    }

    return ac.userID, nil
}

// UserInfo returns the current user information
func (ac *AuthChecker) UserInfo() *UserInfo {
    return ac.userInfo
}
