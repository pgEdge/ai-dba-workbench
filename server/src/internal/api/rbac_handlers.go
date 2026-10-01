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
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// denialCoalesceWindow is how long one recorded denial stands for the
// identical denials that follow it. An unauthenticated or under-
// privileged client can retry a refused request as fast as it likes,
// and every attempt used to append a row, so a single loop could grow
// the audit log without bound and bury the events that matter. Within
// the window the repeats are counted rather than written, and the next
// denial after it reports how many it stands for.
const denialCoalesceWindow = 60 * time.Second

// maxDenialKeys caps the coalescing map, so that a client varying the
// actor name or the path cannot turn the memory saved on audit rows
// into unbounded memory here instead. When the cap is reached the
// oldest entry is dropped, which at worst records one extra row for
// the denial whose entry was evicted.
const maxDenialKeys = 10000

// denialKey identifies a repeated denial. Two refusals coalesce only
// when the same principal, identified by actor id as well as by name
// so that two tokens of one user stay apart, is refused the same action
// for the same reason from the same client address.
type denialKey struct {
	actorType string
	actorID   int64
	actorName string
	actorIP   string
	action    string
	reason    string
	target    string
}

// maxDenialTargetLen caps the request path a denial key and its audit
// row carry, so that a long path cannot inflate either.
const maxDenialTargetLen = 256

// denialTarget is the request path a refusal was made on, capped at
// maxDenialTargetLen bytes. It names the object the request would have
// changed (for example /api/v1/rbac/groups/5/members/user/3), so that
// refusals against different objects are neither merged into one row
// nor recorded without saying what they were aimed at.
func denialTarget(r *http.Request) string {
	target := r.URL.Path
	if len(target) > maxDenialTargetLen {
		target = target[:maxDenialTargetLen]
	}
	return target
}

// denialKeyOf builds the coalescing key for one refusal. An actor with
// no id, such as an unauthenticated caller, keys on zero.
func denialKeyOf(actor auth.Actor, action, reason, target string) denialKey {
	var actorID int64
	if actor.ID != nil {
		actorID = *actor.ID
	}

	return denialKey{
		actorType: string(actor.Type),
		actorID:   actorID,
		actorName: actor.Name,
		actorIP:   actor.IP,
		action:    action,
		reason:    reason,
		target:    target,
	}
}

// summaryActor rebuilds the acting principal from the key, which
// carries every field a summary row needs to attribute the repeats it
// reports.
func (k denialKey) summaryActor() auth.Actor {
	actor := auth.Actor{
		Type: auth.ActorType(k.actorType),
		Name: k.actorName,
		IP:   k.actorIP,
	}
	if k.actorID != 0 {
		id := k.actorID
		actor.ID = &id
	}

	return actor
}

// denialState tracks one key's current window: when the window opened,
// which is when the denial that was recorded happened, and how many
// identical denials have been suppressed since.
type denialState struct {
	firstSeen  time.Time
	suppressed int
}

// denialSummary carries the repeats an evicted entry never got to
// report, so that they are written as one summary row rather than
// discarded with the entry.
type denialSummary struct {
	key        denialKey
	suppressed int
}

// RBACHandler handles REST API requests for RBAC management
type RBACHandler struct {
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker

	// connLister enumerates the monitored connections, so that the
	// token-scope grant checks can work out which unrestricted
	// connections a user reaches without a group grant. With none set
	// those checks fail closed for a connection-bounded token.
	connLister auth.ConnectionVisibilityLister

	// federatedGroups holds the Workbench group names the OIDC group map
	// assigns federated users to. Federation matches a group by name, so
	// renaming one of these, or renaming another group to one of them,
	// moves federated users between groups; a bounded token may do
	// neither. The map is read at startup and changes only on restart.
	federatedGroups map[string]bool

	// denialMu guards denials, which is read and written from every
	// request goroutine that is refused.
	denialMu sync.Mutex
	denials  map[denialKey]*denialState
}

// NewRBACHandler creates a new RBAC handler
func NewRBACHandler(authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *RBACHandler {
	return &RBACHandler{
		authStore:   authStore,
		rbacChecker: rbacChecker,
		denials:     make(map[denialKey]*denialState),
	}
}

// SetConnectionLister sets the lister the token-scope grant checks use
// to enumerate the monitored connections.
func (h *RBACHandler) SetConnectionLister(lister auth.ConnectionVisibilityLister) {
	h.connLister = lister
}

// SetFederatedGroupMap records the Workbench groups the OIDC group map
// (provider group to Workbench group) names, so that a bounded token
// cannot rename a group into or out of it.
func (h *RBACHandler) SetFederatedGroupMap(groupMap map[string]string) {
	h.federatedGroups = make(map[string]bool, len(groupMap))
	for _, workbenchGroup := range groupMap {
		if workbenchGroup != "" {
			h.federatedGroups[workbenchGroup] = true
		}
	}
}

// RecordDenial writes a denied audit event for a refusal made by another
// handler, through the same coalescing as the RBAC handler's own
// refusals.
func (h *RBACHandler) RecordDenial(r *http.Request, reason string) {
	h.recordDenial(r, reason)
}

// RegisterRoutes registers RBAC management routes on the mux
func (h *RBACHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/v1/rbac/users", authWrapper(h.handleUsers))
	mux.HandleFunc("/api/v1/rbac/users/", authWrapper(h.handleUserSubpath))
	mux.HandleFunc("/api/v1/rbac/groups", authWrapper(h.handleGroups))
	mux.HandleFunc("/api/v1/rbac/groups/", authWrapper(h.handleGroupSubpath))
	mux.HandleFunc("/api/v1/rbac/privileges/mcp", authWrapper(h.handleMCPPrivileges))
	mux.HandleFunc("/api/v1/rbac/tokens", authWrapper(h.handleTokens))
	mux.HandleFunc("/api/v1/rbac/tokens/", authWrapper(h.handleTokenSubpath))
	mux.HandleFunc("/api/v1/rbac/audit", authWrapper(h.handleAudit))
}

// =============================================================================
// Permission Helpers
// =============================================================================

// actorStore returns a view of the auth store that attributes every
// audited change to the principal that made the request, so that the
// audit log names the acting user or token rather than the server.
//
// Like recordDenial it tolerates a nil store, which only a partially
// constructed handler has. It returns nil in that case, and the
// mutation the caller then attempts through the nil *auth.ActorStore
// panics, which is what a handler built without an auth store did
// before the audit work as well: the nil check exists to keep the
// audit plumbing out of the failure, not to turn a misconfiguration
// into a handled error.
func (h *RBACHandler) actorStore(r *http.Request) *auth.ActorStore {
	if h.authStore == nil {
		return nil
	}
	return h.authStore.AsActor(auth.ActorFromContext(r.Context()))
}

// recordDenial writes a denied audit event for the request. A recording
// failure is logged and otherwise ignored: the caller is about to
// respond 403 either way, and an audit error must not change what the
// client sees. A nil store is tolerated for the same reason as in
// actorStore.
func (h *RBACHandler) recordDenial(r *http.Request, reason string) {
	if h.authStore == nil {
		return
	}

	actor := auth.ActorFromContext(r.Context())
	action := deniedAction(r)

	target := denialTarget(r)

	record, repeats, expired := h.admitDenial(
		denialKeyOf(actor, action, reason, target), time.Now())

	// Entries evicted by the call above may have carried suppressed
	// repeats; they are written here, outside the lock the eviction
	// ran under.
	h.recordDenialSummaries(r, expired)

	if !record {
		return
	}

	details := map[string]any{"target": target}
	if repeats > 0 {
		details["repeat_count"] = repeats
	}

	if err := h.authStore.RecordDeniedWithDetails(actor, action, reason,
		details); err != nil {
		log.Printf("[ERROR] Failed to record RBAC denial for %s %s: %v", r.Method, logging.SanitizeForLog(r.URL.Path), err) //nolint:gosec // G706: r.URL.Path passed through logging.SanitizeForLog
	}
}

// admitDenial decides whether a denial is written or merely counted. It
// returns true when the caller should record a row, together with the
// number of identical denials that row stands for, which is zero unless
// repeats were suppressed during the window that has just closed.
//
// The first denial for a key opens a window and is recorded at once, so
// that a refusal is never invisible; identical denials inside the
// window are counted instead of written; and the first denial after the
// window closes is recorded, reporting the suppressed ones, and opens a
// fresh window.
func (h *RBACHandler) admitDenial(key denialKey, now time.Time) (bool, int,
	[]denialSummary) {

	h.denialMu.Lock()
	defer h.denialMu.Unlock()

	if h.denials == nil {
		h.denials = make(map[denialKey]*denialState)
	}

	record := true
	repeats := 0

	switch state, ok := h.denials[key]; {
	case !ok:
		h.denials[key] = &denialState{firstSeen: now}
	case now.Sub(state.firstSeen) < denialCoalesceWindow:
		state.suppressed++
		record = false
	default:
		if state.suppressed > 0 {
			repeats = state.suppressed + 1
		}
		state.firstSeen = now
		state.suppressed = 0
	}

	return record, repeats, h.evictDenials(key, now)
}

// recordDenialSummaries writes one summary row per evicted entry that
// had suppressed repeats, so that a burst which stops before the window
// closes still leaves its count in the log. Failures are logged and
// otherwise ignored, as elsewhere on the denial path.
func (h *RBACHandler) recordDenialSummaries(r *http.Request,
	expired []denialSummary) {

	for _, summary := range expired {
		details := map[string]any{
			"repeat_count":  summary.suppressed,
			"window_closed": true,
			"target":        summary.key.target,
		}
		if err := h.authStore.RecordDeniedWithDetails(summary.key.summaryActor(),
			summary.key.action, summary.key.reason, details); err != nil {
			log.Printf("[ERROR] Failed to record RBAC denial summary for %s %s: %v", r.Method, logging.SanitizeForLog(r.URL.Path), err) //nolint:gosec // G706: r.URL.Path passed through logging.SanitizeForLog
		}
	}
}

// evictDenials drops entries whose window has closed, and, if the map
// is still at its cap, the oldest entry. The key just handled is kept
// in both passes, because its window has only now been opened. Every
// dropped entry that still held suppressed repeats is returned as a
// summary for the caller to record once the lock is released. The
// caller must hold h.denialMu.
func (h *RBACHandler) evictDenials(keep denialKey, now time.Time) []denialSummary {
	var expired []denialSummary

	drop := func(key denialKey, state *denialState) {
		if state.suppressed > 0 {
			expired = append(expired, denialSummary{
				key:        key,
				suppressed: state.suppressed,
			})
		}
		delete(h.denials, key)
	}

	for key, state := range h.denials {
		if key != keep && now.Sub(state.firstSeen) >= denialCoalesceWindow {
			drop(key, state)
		}
	}

	for len(h.denials) > maxDenialKeys {
		oldestKey, oldestState, found := h.oldestDenial(keep)
		if !found {
			break
		}
		drop(oldestKey, oldestState)
	}

	return expired
}

// oldestDenial returns the tracked entry whose window opened earliest,
// skipping the key just handled. The caller must hold h.denialMu.
func (h *RBACHandler) oldestDenial(keep denialKey) (denialKey, *denialState, bool) {
	var oldestKey denialKey
	var oldestState *denialState
	var oldest time.Time
	found := false

	for key, state := range h.denials {
		if key == keep {
			continue
		}
		if !found || state.firstSeen.Before(oldest) {
			oldestKey, oldestState, oldest, found = key, state, state.firstSeen, true
		}
	}

	return oldestKey, oldestState, found
}

// requirePermission checks that the caller has the specified admin permission.
// Returns false and sends an error response if access is denied.
func (h *RBACHandler) requirePermission(w http.ResponseWriter, r *http.Request, permission string) bool {
	if !h.rbacChecker.HasAdminPermission(r.Context(), permission) {
		reason := fmt.Sprintf("Permission denied: requires %s permission",
			permission)
		h.recordDenial(r, reason)
		RespondError(w, http.StatusForbidden, reason)
		return false
	}
	return true
}

// requireSuperuser checks that the caller is a superuser.
// Returns false and sends an error response if access is denied.
func (h *RBACHandler) requireSuperuser(w http.ResponseWriter, r *http.Request) bool {
	if !h.rbacChecker.IsSuperuser(r.Context()) {
		const reason = "Permission denied: requires superuser privileges"
		h.recordDenial(r, reason)
		RespondError(w, http.StatusForbidden, reason)
		return false
	}
	return true
}

// =============================================================================
// Denial Action Mapping
// =============================================================================

// rbacPathPrefix is the common prefix of every RBAC REST route.
const rbacPathPrefix = "/api/v1/rbac/"

// deniedResourceActions maps the first RBAC path segment to the helper
// that maps that resource's routes onto an action name. Each helper
// returns an empty string when the request matches no known route
// shape, which leaves the caller's fallback in place.
var deniedResourceActions = map[string]func(method string, parts []string) string{
	"users":  deniedUserAction,
	"groups": deniedGroupAction,
	"tokens": deniedTokenAction,
	"audit":  deniedAuditAction,
}

// rbacPathSegments splits an RBAC route into the segments below the
// common prefix, reporting false when the path is not an RBAC route or
// carries no segment at all.
func rbacPathSegments(path string) ([]string, bool) {
	if !strings.HasPrefix(path, rbacPathPrefix) {
		return nil, false
	}

	parts := strings.Split(strings.Trim(
		strings.TrimPrefix(path, rbacPathPrefix), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return nil, false
	}

	return parts, true
}

// deniedAction maps a refused request to the audit action name it would
// have recorded had it been allowed, so that a denial and the change it
// was refused share a vocabulary. A request that matches no known route
// shape records "rbac.<method>" in lower case, which keeps the event
// rather than dropping it. Connection routes, whose token-scope
// refusals are recorded here too, map through deniedConnectionAction.
func deniedAction(r *http.Request) string {
	fallback := "rbac." + strings.ToLower(r.Method)

	if action := deniedConnectionAction(r.Method, r.URL.Path); action != "" {
		return action
	}

	parts, ok := rbacPathSegments(r.URL.Path)
	if !ok {
		return fallback
	}

	mapper, ok := deniedResourceActions[parts[0]]
	if !ok {
		return fallback
	}

	if action := mapper(r.Method, parts); action != "" {
		return action
	}

	return fallback
}

// connectionPathPrefix is the prefix of the per-connection REST routes.
const connectionPathPrefix = "/api/v1/connections/"

// deniedConnectionAction maps the connection routes whose token-scope
// refusals are audited: PUT and DELETE /connections/{id}, and PUT
// /connections/{id}/cluster. It returns an empty string for any other
// request.
func deniedConnectionAction(method, path string) string {
	if !strings.HasPrefix(path, connectionPathPrefix) {
		return ""
	}
	parts := strings.Split(strings.Trim(
		strings.TrimPrefix(path, connectionPathPrefix), "/"), "/")

	switch {
	case len(parts) == 1 && method == http.MethodPut:
		return "connection.update"
	case len(parts) == 1 && method == http.MethodDelete:
		return "connection.delete"
	case len(parts) == 2 && parts[1] == "cluster" && method == http.MethodPut:
		return "connection.cluster.update"
	}
	return ""
}

// deniedAuditAction maps the /audit routes.
func deniedAuditAction(method string, parts []string) string {
	if len(parts) == 1 && method == http.MethodGet {
		return "audit.read"
	}
	return ""
}

// deniedUserAction maps the /users routes; it returns an empty string
// when the request is not a known mutation.
func deniedUserAction(method string, parts []string) string {
	switch {
	case len(parts) == 1 && method == http.MethodPost:
		return "user.create"
	case len(parts) == 2 && method == http.MethodPut:
		return "user.update"
	case len(parts) == 2 && method == http.MethodDelete:
		return "user.delete"
	}
	return ""
}

// deniedGroupAction maps the /groups routes and their members,
// privileges and permissions sub-resources.
func deniedGroupAction(method string, parts []string) string {
	if len(parts) < 3 {
		return deniedGroupRootAction(method, parts)
	}

	switch parts[2] {
	case "members":
		return deniedGroupMemberAction(method, parts)
	case "permissions":
		return deniedGroupPermissionAction(method, parts)
	case "privileges":
		return deniedGroupPrivilegeAction(method, parts)
	}

	return ""
}

// deniedGroupRootAction maps the /groups and /groups/{id} routes.
func deniedGroupRootAction(method string, parts []string) string {
	switch {
	case len(parts) == 1 && method == http.MethodPost:
		return "group.create"
	case len(parts) == 2 && method == http.MethodPut:
		return "group.update"
	case len(parts) == 2 && method == http.MethodDelete:
		return "group.delete"
	}
	return ""
}

// deniedGroupMemberAction maps the /groups/{id}/members routes.
func deniedGroupMemberAction(method string, parts []string) string {
	switch {
	case len(parts) == 3 && method == http.MethodPost:
		return "group.member.add"
	case len(parts) == 5 && method == http.MethodDelete:
		return "group.member.remove"
	}
	return ""
}

// deniedGroupPermissionAction maps the /groups/{id}/permissions routes.
func deniedGroupPermissionAction(method string, parts []string) string {
	switch {
	case len(parts) == 3 && method == http.MethodPost:
		return "permission.admin.grant"
	case len(parts) == 4 && method == http.MethodDelete:
		return "permission.admin.revoke"
	}
	return ""
}

// deniedGroupPrivilegeAction maps the /groups/{id}/privileges routes.
func deniedGroupPrivilegeAction(method string, parts []string) string {
	if len(parts) < 4 {
		return ""
	}

	switch parts[3] {
	case "mcp":
		if len(parts) != 4 {
			return ""
		}
		switch method {
		case http.MethodPost:
			return "privilege.mcp.grant"
		case http.MethodDelete:
			return "privilege.mcp.revoke"
		}
	case "connections":
		switch {
		case len(parts) == 4 && method == http.MethodPost:
			return "privilege.connection.grant"
		case len(parts) == 5 && method == http.MethodDelete:
			return "privilege.connection.revoke"
		}
	}

	return ""
}

// deniedTokenAction maps the /tokens routes and the scope sub-resource.
func deniedTokenAction(method string, parts []string) string {
	switch {
	case len(parts) == 1 && method == http.MethodPost:
		return "token.create"
	case len(parts) == 2 && method == http.MethodDelete:
		return "token.delete"
	case len(parts) == 3 && parts[2] == "scope" && method == http.MethodPut:
		return "token.scope.set"
	case len(parts) == 3 && parts[2] == "scope" && method == http.MethodDelete:
		return "token.scope.clear"
	}
	return ""
}
