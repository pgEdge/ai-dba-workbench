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
	"strconv"
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
// actor name or the action cannot turn the memory saved on audit rows
// into unbounded memory here instead. When the cap is reached the
// oldest entry is dropped, and any repeats it held are written as a
// summary row.
const maxDenialKeys = 10000

// maxDenialTargets caps how many distinct targets one window lists in
// its audit row. Further targets are only counted, in
// targets_truncated, so that a flood across many objects still costs
// one bounded row.
const maxDenialTargets = 20

// maxDenialActionLen caps the action name a denial key carries. The
// action is drawn from a fixed vocabulary, but the cap keeps the key
// bounded whatever deniedAction returns.
const maxDenialActionLen = 64

// unmatchedDenialTarget is the target recorded for a refusal on a route
// the action mapper does not recognize, so that nothing taken verbatim
// from the request path reaches the key or the row.
const unmatchedDenialTarget = "unmatched"

// denialKey identifies a repeated denial. Two refusals coalesce only
// when the same principal, identified by actor id as well as by name
// so that two tokens of one user stay apart, is refused the same action
// for the same reason from the same client address. The target is
// deliberately not part of the key: a client may vary the object id
// without limit, so keying on it would let one loop write a row per id.
// The targets a window covers are listed in its row instead.
type denialKey struct {
	actorType string
	actorID   int64
	actorName string
	actorIP   string
	action    string
	reason    string
}

// denialKeyOf builds the coalescing key for one refusal. An actor with
// no id, such as an unauthenticated caller, keys on zero.
func denialKeyOf(actor auth.Actor, action, reason string) denialKey {
	var actorID int64
	if actor.ID != nil {
		actorID = *actor.ID
	}
	if len(action) > maxDenialActionLen {
		action = action[:maxDenialActionLen]
	}

	return denialKey{
		actorType: string(actor.Type),
		actorID:   actorID,
		actorName: actor.Name,
		actorIP:   actor.IP,
		action:    action,
		reason:    reason,
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

// denialWindow counts the denials a row stands for, and lists the
// distinct targets they were aimed at, up to maxDenialTargets. truncated
// counts the denials whose target was new once the list was full.
type denialWindow struct {
	count     int
	targets   []string
	truncated int
}

// note counts one more denial against target.
func (w *denialWindow) note(target string) {
	w.count++
	for _, t := range w.targets {
		if t == target {
			return
		}
	}
	if len(w.targets) < maxDenialTargets {
		w.targets = append(w.targets, target)
		return
	}
	w.truncated++
}

// addTo writes the window's count and targets into an audit row's
// details.
func (w *denialWindow) addTo(details map[string]any) {
	details["repeat_count"] = w.count
	details["targets"] = w.targets
	details["targets_truncated"] = w.truncated
}

// denialState tracks one key's current window: when the window opened,
// which is when the denial that was recorded happened, and the
// identical denials suppressed since. Every state is also on a list
// ordered by firstSeen, oldest first, so that eviction looks only at
// the entries it drops rather than scanning the map.
type denialState struct {
	key        denialKey
	firstSeen  time.Time
	suppressed denialWindow
	prev, next *denialState
}

// denialSummary carries the repeats an evicted entry never got to
// report, so that they are written as one summary row rather than
// discarded with the entry.
type denialSummary struct {
	key    denialKey
	window denialWindow
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

	// denialMu guards denials and the list running from denialOldest to
	// denialNewest, which are read and written from every request
	// goroutine that is refused.
	denialMu     sync.Mutex
	denials      map[denialKey]*denialState
	denialOldest *denialState
	denialNewest *denialState
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
	action, target := deniedRoute(r)
	key := denialKeyOf(actor, action, reason)

	record, closed, expired := h.admitDenial(key, target, time.Now())

	// Entries evicted by the call above may have carried suppressed
	// repeats; they are written here, outside the lock the eviction
	// ran under.
	h.recordDenialSummaries(r, expired)

	if !record {
		return
	}

	details := map[string]any{"target": target}
	if closed.count > 0 {
		closed.addTo(details)
	}

	if err := h.authStore.RecordDeniedWithDetails(actor, key.action, reason,
		details); err != nil {
		log.Printf("[ERROR] Failed to record RBAC denial for %s %s: %v", logging.SanitizeForLog(r.Method), logging.SanitizeForLog(r.URL.Path), err) //nolint:gosec // G706: r.Method and r.URL.Path passed through logging.SanitizeForLog
	}
}

// admitDenial decides whether a denial is written or merely counted. It
// returns true when the caller should record a row, together with the
// window that row closes, whose count is the number of denials the row
// stands for and is zero unless repeats were suppressed during the
// window that has just closed.
//
// The first denial for a key opens a window and is recorded at once, so
// that a refusal is never invisible; identical denials inside the
// window are counted, with their targets, instead of written; and the
// first denial after the window closes is recorded, reporting the
// suppressed ones and its own target, and opens a fresh window.
func (h *RBACHandler) admitDenial(key denialKey, target string,
	now time.Time) (bool, denialWindow, []denialSummary) {

	h.denialMu.Lock()
	defer h.denialMu.Unlock()

	if h.denials == nil {
		h.denials = make(map[denialKey]*denialState)
	}

	record := true
	var closed denialWindow

	switch state, ok := h.denials[key]; {
	case !ok:
		state = &denialState{key: key, firstSeen: now}
		h.denials[key] = state
		h.pushNewestDenial(state)
	case now.Sub(state.firstSeen) < denialCoalesceWindow:
		state.suppressed.note(target)
		record = false
	default:
		if state.suppressed.count > 0 {
			closed = state.suppressed
			closed.note(target)
		}
		state.firstSeen = now
		state.suppressed = denialWindow{}
		h.unlinkDenial(state)
		h.pushNewestDenial(state)
	}

	return record, closed, h.evictDenials(key, now)
}

// pushNewestDenial appends state to the newest end of the eviction
// list. The caller must hold h.denialMu.
func (h *RBACHandler) pushNewestDenial(state *denialState) {
	state.prev = h.denialNewest
	state.next = nil
	if h.denialNewest != nil {
		h.denialNewest.next = state
	} else {
		h.denialOldest = state
	}
	h.denialNewest = state
}

// unlinkDenial removes state from the eviction list. The caller must
// hold h.denialMu.
func (h *RBACHandler) unlinkDenial(state *denialState) {
	if state.prev != nil {
		state.prev.next = state.next
	} else {
		h.denialOldest = state.next
	}
	if state.next != nil {
		state.next.prev = state.prev
	} else {
		h.denialNewest = state.prev
	}
	state.prev, state.next = nil, nil
}

// recordDenialSummaries writes one summary row per evicted entry that
// had suppressed repeats, so that a burst which stops before the window
// closes still leaves its count in the log. Failures are logged and
// otherwise ignored, as elsewhere on the denial path.
func (h *RBACHandler) recordDenialSummaries(r *http.Request,
	expired []denialSummary) {

	for i := range expired {
		summary := &expired[i]
		details := map[string]any{"window_closed": true}
		summary.window.addTo(details)
		if err := h.authStore.RecordDeniedWithDetails(summary.key.summaryActor(),
			summary.key.action, summary.key.reason, details); err != nil {
			log.Printf("[ERROR] Failed to record RBAC denial summary for %s %s: %v", logging.SanitizeForLog(r.Method), logging.SanitizeForLog(r.URL.Path), err) //nolint:gosec // G706: r.Method and r.URL.Path passed through logging.SanitizeForLog
		}
	}
}

// evictDenials drops entries from the oldest end of the eviction list
// while their window has closed or the map is over its cap. The list is
// ordered by firstSeen, so it stops at the first entry that is neither,
// and the work done is proportional to the entries dropped. The key
// just handled is kept, because its window has only now been opened.
// Every dropped entry that still held suppressed repeats is returned as
// a summary for the caller to record once the lock is released. The
// caller must hold h.denialMu.
func (h *RBACHandler) evictDenials(keep denialKey, now time.Time) []denialSummary {
	var expired []denialSummary

	for state := h.denialOldest; state != nil; {
		next := state.next
		closed := now.Sub(state.firstSeen) >= denialCoalesceWindow
		if !closed && len(h.denials) <= maxDenialKeys {
			break
		}
		if state.key != keep {
			if state.suppressed.count > 0 {
				expired = append(expired, denialSummary{
					key:    state.key,
					window: state.suppressed,
				})
			}
			h.unlinkDenial(state)
			delete(h.denials, state.key)
		}
		state = next
	}

	return expired
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
// have recorded had it been allowed; see deniedRoute.
func deniedAction(r *http.Request) string {
	action, _ := deniedRoute(r)
	return action
}

// deniedRoute maps a refused request to the audit action name it would
// have recorded had it been allowed, so that a denial and the change it
// was refused share a vocabulary, and to the object it was aimed at.
//
// Neither result carries text taken verbatim from the request, so a
// client cannot inflate the coalescing key or the audit row, nor split
// one burst into a row per path. The target is the resource kind, plus
// the numeric id parsed from the path where the route names one (for
// example "groups/5" or "connections/7"); a route the mapper does not
// recognize records unmatchedDenialTarget, and an action of
// "rbac.<method>" in lower case, or "rbac.other" for a method outside
// the standard set, which keeps the event rather than dropping it.
func deniedRoute(r *http.Request) (action, target string) {
	if action := deniedConnectionAction(r.Method, r.URL.Path); action != "" {
		return action, denialResourceTarget("connections",
			strings.TrimPrefix(r.URL.Path, connectionPathPrefix))
	}

	if parts, ok := rbacPathSegments(r.URL.Path); ok {
		if mapper, ok := deniedResourceActions[parts[0]]; ok {
			if action := mapper(r.Method, parts); action != "" {
				return action, denialResourceTarget(parts[0],
					strings.Join(parts[1:], "/"))
			}
		}
	}

	if action, target := deniedAPIRoute(r.Method, r.URL.Path); action != "" {
		return action, target
	}

	return fallbackDeniedAction(r.Method), unmatchedDenialTarget
}

// standardMethods is the set of HTTP methods a fallback action may
// name. Any other method is recorded as "rbac.other", since the method
// is the client's own text and is otherwise unbounded.
var standardMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodConnect: true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// fallbackDeniedAction is the action recorded for a request on a route
// the mapper does not recognize.
func fallbackDeniedAction(method string) string {
	if standardMethods[method] {
		return "rbac." + strings.ToLower(method)
	}
	return "rbac.other"
}

// denialResourceTarget names the object a refusal was aimed at: kind,
// which the caller takes from a fixed vocabulary, followed by the
// numeric id that starts rest when there is one. The id is parsed and
// formatted again, so a path that pads or otherwise varies it still
// yields one target.
func denialResourceTarget(kind, rest string) string {
	idPart, _, _ := strings.Cut(strings.Trim(rest, "/"), "/")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		return kind
	}
	return kind + "/" + strconv.FormatInt(id, 10)
}

// apiPathPrefix is the common prefix of the REST routes.
const apiPathPrefix = "/api/v1/"

// deniedAPIResources maps the first path segment of the non-RBAC routes
// whose token-scope refusals are audited to the resource name their
// actions use.
var deniedAPIResources = map[string]string{
	"clusters":              "cluster",
	"cluster-groups":        "cluster_group",
	"notification-channels": "notification_channel",
	"alert-rules":           "alert_rule",
}

// deniedAPIRoute maps a write on one of deniedAPIResources to
// "<resource>.create", ".update" or ".delete" and the object it names.
// A write below an object, such as adding a server to a cluster, is
// recorded as an update of that object. It returns an empty action for
// any other request.
func deniedAPIRoute(method, path string) (action, target string) {
	if !strings.HasPrefix(path, apiPathPrefix) {
		return "", ""
	}
	parts := strings.Split(strings.Trim(
		strings.TrimPrefix(path, apiPathPrefix), "/"), "/")
	resource, ok := deniedAPIResources[parts[0]]
	if !ok {
		return "", ""
	}

	var verb string
	switch {
	case method == http.MethodPost && len(parts) == 1:
		verb = "create"
	case method == http.MethodDelete && len(parts) == 2:
		verb = "delete"
	case method == http.MethodPost || method == http.MethodPut ||
		method == http.MethodPatch || method == http.MethodDelete:
		verb = "update"
	default:
		return "", ""
	}

	return resource + "." + verb,
		denialResourceTarget(parts[0], strings.Join(parts[1:], "/"))
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
