/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Token Scopes
// =============================================================================

// The refusals given when a caller who is not a superuser reaches beyond
// the tokens it owns (issue #522). Each is a fixed string, so that the
// denial audit coalesces repeats, and neither names the owner or says
// whether one exists.
const (
	refuseTokenForOtherOwner = "Permission denied: only a superuser may " +
		"create a token owned by another user"
	refuseTokenNotOwned = "Permission denied: only a superuser may widen " +
		"or clear the scope of a token owned by another user; a change to " +
		"such a token must narrow it"
)

// refuseTokenScopeChanged answers a scope write refused because the
// token's scope changed between the ceiling check and the write (see
// auth.ErrTokenScopeChanged). The caller may retry, and the retry is
// judged against the scope as it now stands.
const refuseTokenScopeChanged = "Token scope changed while the request " +
	"was being checked; retry the request"

// tokenWriteChecked, when set, is called between the checks a token
// write makes before its transaction and the write itself. It exists so
// that tests can change what those checks read at that point, as a
// concurrent request could; it is nil in the server.
var tokenWriteChecked func()

// afterTokenWriteCheck runs tokenWriteChecked, if set.
func afterTokenWriteCheck() {
	if tokenWriteChecked != nil {
		tokenWriteChecked()
	}
}

// handleTokens handles GET/POST /api/v1/rbac/tokens
func (h *RBACHandler) handleTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listTokens(w, r)
	case http.MethodPost:
		h.createToken(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listTokens returns all tokens with their scope information. Every
// holder of manage_token_scopes sees every token, since any of them may
// narrow or revoke any token (issue #522); without it a caller may not
// create or change a token at all, so there is nothing to list.
func (h *RBACHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, auth.PermManageTokenScopes) {
		return
	}

	tokens, err := h.authStore.ListAllTokens()
	if err != nil {
		log.Printf("[ERROR] Failed to list tokens: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list tokens")
		return
	}

	type tokenConnectionScope struct {
		ConnectionID int    `json:"connection_id"`
		AccessLevel  string `json:"access_level"`
	}

	type tokenScope struct {
		Scoped           bool                   `json:"scoped"`
		Connections      []tokenConnectionScope `json:"connections,omitempty"`
		MCPPrivileges    []int64                `json:"mcp_privileges,omitempty"`
		AdminPermissions []string               `json:"admin_permissions,omitempty"`
	}

	type tokenResponse struct {
		ID               int64       `json:"id"`
		Name             string      `json:"name"`
		TokenPrefix      string      `json:"token_prefix"`
		UserID           int64       `json:"user_id"`
		Username         string      `json:"username,omitempty"`
		IsServiceAccount bool        `json:"is_service_account"`
		IsSuperuser      bool        `json:"is_superuser"`
		ExpiresAt        *time.Time  `json:"expires_at"`
		Scope            *tokenScope `json:"scope,omitempty"`
	}

	result := make([]tokenResponse, 0, len(tokens))
	for _, t := range tokens {
		prefix := t.TokenHash
		if len(prefix) > 12 {
			prefix = prefix[:12]
		}
		tr := tokenResponse{
			ID:          t.ID,
			Name:        t.Annotation,
			TokenPrefix: prefix,
			UserID:      t.OwnerID,
			ExpiresAt:   t.ExpiresAt,
		}
		user, err := h.authStore.GetUserByID(t.OwnerID)
		if err == nil && user != nil {
			tr.Username = user.Username
			tr.IsServiceAccount = user.IsServiceAccount
			tr.IsSuperuser = user.IsSuperuser
		}
		scope, err := h.authStore.GetTokenScope(t.ID)
		if err == nil && scope != nil {
			conns := make([]tokenConnectionScope, len(scope.Connections))
			for i, c := range scope.Connections {
				conns[i] = tokenConnectionScope{
					ConnectionID: c.ConnectionID,
					AccessLevel:  c.AccessLevel,
				}
			}
			tr.Scope = &tokenScope{
				Scoped:           true,
				Connections:      conns,
				MCPPrivileges:    scope.MCPPrivileges,
				AdminPermissions: scope.AdminPermissions,
			}
		}
		result = append(result, tr)
	}

	RespondJSON(w, http.StatusOK, map[string]any{"tokens": result})
}

// createToken creates a new token for the specified user.
func (h *RBACHandler) createToken(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, auth.PermManageTokenScopes) {
		return
	}

	var req struct {
		OwnerUsername string  `json:"owner_username"`
		Annotation    string  `json:"annotation"`
		ExpiresIn     *string `json:"expires_in"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.OwnerUsername == "" {
		RespondError(w, http.StatusBadRequest, "Owner username is required")
		return
	}
	if !h.requireOwnTokenOwner(w, r, req.OwnerUsername) {
		return
	}

	// The token's display name is stored in the annotation field. An empty
	// annotation is permitted (an unnamed token), so validation runs only
	// when the caller supplied a value. When present, the annotation is
	// subject to the shared display-name contract (length, allowed
	// characters). Token names are intentionally not unique, so no
	// duplicate check is performed here.
	annotation := strings.TrimSpace(req.Annotation)
	if annotation != "" {
		if err := ValidateDisplayName(annotation); err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	var expiry *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn != "" && *req.ExpiresIn != "never" {
		duration, err := parseTokenExpiry(*req.ExpiresIn)
		if err != nil {
			RespondError(w, http.StatusBadRequest,
				fmt.Sprintf("Invalid expires_in: %s", err.Error()))
			return
		}
		exp := time.Now().Add(duration)
		expiry = &exp
	}

	// A superuser's token carries the superuser role, so only a superuser
	// may mint one, as only a superuser may edit a superuser's account
	// (see updateUser). Without this, a session holding
	// manage_token_scopes, which ownerWithinTokenScope does not bound,
	// could mint itself an unscoped superuser token. The store makes that
	// check against the owner it reads inside the insert's own
	// transaction, so that an owner promoted after any read made here is
	// still refused (issue #607); see respondTokenWriteError.

	// A new token acts with its owner's access, so a token may mint one
	// only for an owner whose access lies within its own (issues #471
	// and #522).
	if !h.requireGrantInTokenScope(w, r,
		h.ownerWithinTokenScope(r.Context(), req.OwnerUsername),
		refuseTokenOwner) {
		return
	}

	afterTokenWriteCheck()

	// The store also refuses, with ErrTokenNotOwned, an owner other than
	// the caller's own user, judged on the owner row the insert reads, so
	// that a user recreated under the name after requireOwnTokenOwner
	// looked it up is still refused (issue #522).
	rawToken, storedToken, err := h.actorStore(r).CreateTokenFor(
		req.OwnerUsername, annotation, expiry,
		h.rbacChecker.IsSuperuser(r.Context()), h.tokenOwnerGuard(r))
	if errors.Is(err, auth.ErrTokenNotOwned) {
		h.requireGrantInTokenScope(w, r, false, refuseTokenForOtherOwner)
		return
	}
	if err != nil {
		h.respondTokenWriteError(w, r, err, fmt.Sprintf(
			"Failed to create token for %s", req.OwnerUsername),
			"Failed to create token")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]any{
		"token":      rawToken,
		"id":         storedToken.ID,
		"owner":      req.OwnerUsername,
		"annotation": storedToken.Annotation,
		"expires_at": storedToken.ExpiresAt,
		"message":    "Token created. Save this token securely - it will not be shown again.",
	})
}

// parseTokenExpiry parses a human-readable expiry string into a duration.
// Supported formats: "24h", "30d", "4w", "6m", "1y".
// Maximum allowed duration is 10 years (3650 days equivalent).
func parseTokenExpiry(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid format")
	}
	numStr := s[:len(s)-1]
	unit := s[len(s)-1:]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", numStr)
	}
	if num <= 0 {
		return 0, fmt.Errorf("value must be positive")
	}

	// Convert to days equivalent for bounds checking (max 3650 days = ~10 years)
	var days int
	switch unit {
	case "h":
		// Allow up to 3650*24 = 87600 hours
		if num > 87600 {
			return 0, fmt.Errorf("value exceeds maximum of 87600 hours (~10 years)")
		}
		return time.Duration(num) * time.Hour, nil
	case "d":
		days = num
	case "w":
		days = num * 7
	case "m":
		days = num * 30
	case "y":
		days = num * 365
	default:
		return 0, fmt.Errorf("invalid unit: %s (use h, d, w, m, y)", unit)
	}

	if days > 3650 {
		return 0, fmt.Errorf("value exceeds maximum of 3650 days (~10 years)")
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// handleTokenSubpath handles /api/v1/rbac/tokens/{id} and
// /api/v1/rbac/tokens/{id}/scope
func (h *RBACHandler) handleTokenSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/rbac/tokens/")
	if path == "" {
		h.handleTokens(w, r)
		return
	}
	parts := strings.Split(path, "/")

	tokenID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid token ID")
		return
	}

	if !h.requirePermission(w, r, auth.PermManageTokenScopes) {
		return
	}

	// /api/v1/rbac/tokens/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodDelete:
			h.deleteToken(w, r, tokenID)
		default:
			w.Header().Set("Allow", "DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/v1/rbac/tokens/{id}/scope
	if len(parts) == 2 && parts[1] == "scope" {
		switch r.Method {
		case http.MethodGet:
			h.getTokenScope(w, r, tokenID)
		case http.MethodPut:
			h.setTokenScope(w, r, tokenID)
		case http.MethodDelete:
			h.clearTokenScope(w, r, tokenID)
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// requireOwnTokenOwner refuses, with 403, a caller that is not a
// superuser creating a token for any owner but the user it acts as: a
// session's user, or an API token's owner (issue #522). A new token acts
// with its owner's access, so minting one for someone else is as good as
// acting as them. An unknown owner is refused in the same way, so that
// the answer does not say whether the name exists; a failed lookup is
// refused with 500. Either refusal is recorded as a denial.
func (h *RBACHandler) requireOwnTokenOwner(w http.ResponseWriter,
	r *http.Request, ownerUsername string) bool {

	ctx := r.Context()
	if h.rbacChecker.IsSuperuser(ctx) {
		return true
	}
	owner, err := h.authStore.GetUser(ownerUsername)
	if err != nil {
		log.Printf("[ERROR] Failed to look up token owner: %v", err)
		const reason = "Failed to get user"
		h.recordDenial(r, reason)
		RespondError(w, http.StatusInternalServerError, reason)
		return false
	}
	return h.requireGrantInTokenScope(w, r,
		owner != nil && owner.ID == auth.GetUserIDFromContext(ctx),
		refuseTokenForOtherOwner)
}

// tokenOwnerGuard is the owner rule the store applies to a scope write
// made by this request (issue #522): a superuser may widen or clear any
// token, and anyone else only those owned by the user it acts as, a
// session's user or an API token's owner; a write to any other token
// must narrow it. A token caller is also bounded by its ceiling, which
// requireTokenScopeChange checks first, siblings with the same owner
// included.
func (h *RBACHandler) tokenOwnerGuard(r *http.Request) auth.TokenOwnerGuard {
	ctx := r.Context()
	if h.rbacChecker.IsSuperuser(ctx) {
		return auth.AnyTokenOwner()
	}
	return auth.TokensOwnedBy(auth.GetUserIDFromContext(ctx))
}

// superuserOwnerAllowed reports whether the caller may change or delete
// a token owned by a superuser, which the store checks against the owner
// it reads inside the change's own transaction (issue #607). Only a
// superuser may (see requireSuperuser), as updateUser requires for a
// superuser's account: such a token carries the superuser role, so
// widening its scope hands that role out, and narrowing or deleting it
// can lock an administrator out. A token acting on itself is allowed,
// since it may already narrow or delete itself and
// JudgeTokenScopeChange stops it widening itself.
func (h *RBACHandler) superuserOwnerAllowed(r *http.Request,
	tokenID int64) bool {

	if acting := auth.GetTokenIDFromContext(r.Context()); acting > 0 &&
		acting == tokenID {
		return true
	}
	return h.rbacChecker.IsSuperuser(r.Context())
}

// respondTokenWriteError answers a failed token write. The store's
// refusal to let a caller who is not a superuser mint, change or delete
// a superuser's token is answered exactly as requireSuperuser answers,
// and its refusal to let one widen or clear a token it does not own
// with refuseTokenNotOwned. A write refused because the token's scope
// changed after the ceiling check judged it is answered 409 with
// refuseTokenScopeChanged. For each of these the coalesced denial row is
// the only audit row the refusal leaves, since the store records no
// failure for them. Anything else is logged with logContext and answered
// with a 500 carrying failure.
func (h *RBACHandler) respondTokenWriteError(w http.ResponseWriter,
	r *http.Request, err error, logContext, failure string) {

	if errors.Is(err, auth.ErrSuperuserTargetForbidden) {
		h.denyNotSuperuser(w, r)
		return
	}
	if errors.Is(err, auth.ErrTokenNotOwned) {
		h.requireGrantInTokenScope(w, r, false, refuseTokenNotOwned)
		return
	}
	if errors.Is(err, auth.ErrTokenScopeChanged) {
		h.recordDenial(r, refuseTokenScopeChanged)
		RespondError(w, http.StatusConflict, refuseTokenScopeChanged)
		return
	}
	log.Printf("[ERROR] %s: %v", logContext, err)
	RespondError(w, http.StatusInternalServerError, failure)
}

// deleteToken deletes a token by its ID.
func (h *RBACHandler) deleteToken(w http.ResponseWriter, r *http.Request, tokenID int64) {
	if err := h.actorStore(r).DeleteToken(strconv.FormatInt(tokenID, 10),
		h.superuserOwnerAllowed(r, tokenID)); err != nil {
		h.respondTokenWriteError(w, r, err,
			fmt.Sprintf("Failed to delete token %d", tokenID),
			"Failed to delete token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) getTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	scope, err := h.authStore.GetTokenScope(tokenID)
	if errors.Is(err, auth.ErrTokenNotFound) {
		RespondError(w, http.StatusNotFound, "Token not found")
		return
	}
	if err != nil {
		log.Printf("[ERROR] Failed to get token scope for token %d: %v", tokenID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to get token scope")
		return
	}

	if scope == nil {
		RespondJSON(w, http.StatusOK, map[string]any{
			"token_id": tokenID,
			"scoped":   false,
		})
		return
	}

	RespondJSON(w, http.StatusOK, map[string]any{
		"token_id":          tokenID,
		"scoped":            true,
		"connections":       scope.Connections,
		"mcp_privileges":    scope.MCPPrivileges,
		"admin_permissions": scope.AdminPermissions,
	})
}

// requireKnownMCPPrivileges refuses, with 400, an MCP scope naming an
// identifier that is not registered, before any ceiling or owner check
// can refuse it with 403 as an item the token does not hold (issue
// #522). The store checks again as it writes, so an identifier deleted
// in between still fails the write with ErrUnknownMCPPrivilege. A
// failed lookup is answered with 500.
func (h *RBACHandler) requireKnownMCPPrivileges(w http.ResponseWriter,
	identifiers []string) bool {

	for _, identifier := range identifiers {
		if identifier == "*" {
			continue
		}
		priv, err := h.authStore.GetMCPPrivilege(identifier)
		if err != nil {
			log.Printf("[ERROR] Failed to look up MCP privilege: %v", err)
			RespondError(w, http.StatusInternalServerError,
				"Failed to set token scope")
			return false
		}
		if priv == nil {
			RespondError(w, http.StatusBadRequest, fmt.Sprintf("%v: %q",
				auth.ErrUnknownMCPPrivilege, identifier))
			return false
		}
	}
	return true
}

// emptyScopeKind names the first scope kind a scope PUT supplied as an
// empty array, or returns "" when there is none. Each kind is stored as
// rows in its own table and a kind with no rows is unrestricted, so
// writing an empty array would lift that restriction whilst reading as
// "allow nothing". The store still clears a kind that way for the CLI,
// which does so deliberately and says so; the HTTP API refuses it, and
// DELETE on the scope is its explicit way to lift a restriction.
func emptyScopeKind(connections, mcpPrivileges, adminPermissions bool) string {
	switch {
	case connections:
		return "connections"
	case mcpPrivileges:
		return "mcp_privileges"
	case adminPermissions:
		return "admin_permissions"
	}
	return ""
}

func (h *RBACHandler) setTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	var req struct {
		Connections      []auth.ScopedConnection `json:"connections"`
		MCPPrivileges    []string                `json:"mcp_privileges"`
		AdminPermissions []string                `json:"admin_permissions"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// A request refused for its content must leave every scope kind as
	// it was, so empty arrays and the connection access levels are
	// checked before any write, and the store writes every kind in one
	// transaction, so that an MCP scope naming an unregistered
	// identifier, or a token whose owner is promoted to superuser before
	// the write, leaves them all unchanged.
	if kind := emptyScopeKind(req.Connections != nil && len(req.Connections) == 0,
		req.MCPPrivileges != nil && len(req.MCPPrivileges) == 0,
		req.AdminPermissions != nil && len(req.AdminPermissions) == 0); kind != "" {
		RespondError(w, http.StatusBadRequest, fmt.Sprintf(
			"%s must not be an empty array: a scope kind with no entries is "+
				"unrestricted, so an empty array would lift the restriction "+
				"rather than deny everything; omit the key to leave it "+
				"unchanged, or use DELETE to clear the token's scope", kind))
		return
	}
	if err := auth.ValidateScopedConnections(req.Connections); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidateAdminPermissions(req.AdminPermissions); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.requireKnownMCPPrivileges(w, req.MCPPrivileges) {
		return
	}
	// What the change grants beyond the token's current scope must lie
	// within the acting token's own access, and what it narrows needs
	// read on each connection concerned, kind by kind for the kinds the
	// request supplies (issue #471). This holds for a token changing its
	// own scope as well: it may narrow itself, but its access is already
	// its ceiling, so it can never widen itself.
	change := auth.TokenScopeChange{
		Connections:      req.Connections,
		MCPPrivileges:    req.MCPPrivileges,
		AdminPermissions: req.AdminPermissions,
	}
	judged, ok := h.requireTokenScopeChange(w, r, tokenID, change, false)
	if !ok {
		return
	}
	afterTokenWriteCheck()

	// The store refuses a change that widens a token the caller does
	// not own, judged in the write's own transaction (issue #522), and a
	// change whose token's scope is no longer the one judged above.
	if err := h.actorStore(r).SetTokenScope(tokenID, change,
		h.superuserOwnerAllowed(r, tokenID),
		h.tokenOwnerGuard(r), judged); err != nil {
		if errors.Is(err, auth.ErrUnknownMCPPrivilege) {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.respondTokenWriteError(w, r, err,
			fmt.Sprintf("Failed to set scope for token %d", tokenID),
			"Failed to set token scope")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) clearTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	// Clearing the scope leaves the token with its owner's whole access,
	// which must then lie within the acting token's own in every kind
	// the scope restricts today.
	judged, ok := h.requireTokenScopeChange(w, r, tokenID,
		auth.TokenScopeChange{}, true)
	if !ok {
		return
	}
	afterTokenWriteCheck()

	// Clearing widens the token, so the store refuses it for a token
	// the caller does not own (issue #522), and the clear if the token's
	// scope is no longer the one judged above.
	if err := h.actorStore(r).ClearTokenScope(tokenID,
		h.superuserOwnerAllowed(r, tokenID),
		h.tokenOwnerGuard(r), judged); err != nil {
		h.respondTokenWriteError(w, r, err,
			fmt.Sprintf("Failed to clear token scope for token %d", tokenID),
			"Failed to clear token scope")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
