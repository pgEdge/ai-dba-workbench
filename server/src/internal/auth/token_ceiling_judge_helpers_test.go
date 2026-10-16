/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package auth

import "context"

// tokenScopeChangeWithinCeiling is JudgeTokenScopeChange without the
// snapshot, for tests that check only the verdict. It lives in a test
// file so that no handler can judge a change without the snapshot its
// write must be pinned to.
func (rc *RBACChecker) tokenScopeChangeWithinCeiling(ctx context.Context,
	tokenID int64, change TokenScopeChange) (bool, error) {

	allowed, _, err := rc.JudgeTokenScopeChange(ctx, tokenID, change)
	return allowed, err
}

// tokenScopeClearWithinCeiling is JudgeTokenScopeClear without the
// snapshot, for tests that check only the verdict.
func (rc *RBACChecker) tokenScopeClearWithinCeiling(ctx context.Context,
	tokenID int64, lister ConnectionVisibilityLister) (bool, error) {

	allowed, _, err := rc.JudgeTokenScopeClear(ctx, tokenID, lister)
	return allowed, err
}
