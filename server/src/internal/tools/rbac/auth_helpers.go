/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package rbac

import (
	"fmt"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// RequirePermission checks that the caller has the specified admin
// permission. It extracts the context from the tool args and delegates
// to the RBACChecker. Returns nil when access is granted or an error
// when access is denied.
func RequirePermission(args map[string]interface{}, checker *auth.RBACChecker, permission string) error {
	ctx := getContextFromArgs(args)
	if !checker.HasAdminPermission(ctx, permission) {
		return fmt.Errorf("access denied: %s permission required", permission)
	}
	return nil
}

// RequireSuperuser checks that the caller has superuser privileges.
// This is used for operations that must remain restricted to superusers,
// such as granting or revoking superuser status.
func RequireSuperuser(args map[string]interface{}) error {
	ctx := getContextFromArgs(args)
	if !auth.IsSuperuserFromContext(ctx) {
		return fmt.Errorf("access denied: superuser privileges required")
	}
	return nil
}
