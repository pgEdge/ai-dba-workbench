/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package overview

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// openAPISpecPath is the path to the OpenAPI specification for RFC 8631
// API discovery.
const openAPISpecPath = "/api/v1/openapi.json"

// errScopeWithConnectionIDs answers a request that names both explicit
// connections and a scope, so that a scope the caller may not see is
// never looked up alongside connections they can.
const errScopeWithConnectionIDs = "connection_ids cannot be combined with scope_type or scope_id"

// validScopeTypes lists the scope_type values accepted by the handler.
var validScopeTypes = map[string]bool{
	"server":  true,
	"cluster": true,
	"group":   true,
}

// Handler serves the current AI-generated estate overview via a REST
// endpoint. It satisfies the routeRegistrar interface used by the
// server's handler setup.
type Handler struct {
	generator   *Generator
	hub         *Hub
	rbacChecker *auth.RBACChecker
	datastore   *database.Datastore
	limits      generationLimits
}

// NewHandlerWithRBAC creates a new overview handler that enforces scope
// visibility against the caller's RBAC view. The gate is never
// disabled: a nil rbacChecker gives every caller an empty visible set
// (issue #561), and a nil datastore leaves the checker able to admit
// only superusers, wildcard grants and explicitly granted connections,
// with cluster and group scopes treated as having no members.
func NewHandlerWithRBAC(generator *Generator, hub *Hub, rbacChecker *auth.RBACChecker, datastore *database.Datastore) *Handler {
	return &Handler{
		generator:   generator,
		hub:         hub,
		rbacChecker: rbacChecker,
		datastore:   datastore,
	}
}

// RegisterRoutes registers the overview endpoint on the provided mux.
// The authWrapper function is applied so that the endpoint requires
// authentication.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/v1/overview", authWrapper(h.handleOverview))
	mux.HandleFunc("/api/v1/overview/stream", authWrapper(h.handleSSE))
}

// generatingResponse is returned in place of an overview: "generating"
// before the first estate summary exists, "empty" when the caller can
// see no connections in the scope, and "error" with a client-facing
// message when the estate summary could not be generated.
type generatingResponse struct {
	Status  string `json:"status"`
	Summary any    `json:"summary"`
	Error   string `json:"error,omitempty"`
}

// handleOverview handles GET /api/v1/overview and returns the current
// estate overview as JSON. It accepts optional query parameters
// scope_type and scope_id to return a scoped summary instead of the
// estate-wide overview.
func (h *Handler) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"service-desc\"", openAPISpecPath))

	refresh := r.URL.Query().Get("refresh") == "true"
	connectionIDsStr := r.URL.Query().Get("connection_ids")
	scopeName := r.URL.Query().Get("scope_name")
	scopeType := r.URL.Query().Get("scope_type")
	scopeIDStr := r.URL.Query().Get("scope_id")

	if connectionIDsStr != "" && (scopeType != "" || scopeIDStr != "") {
		http.Error(w, errScopeWithConnectionIDs, http.StatusBadRequest)
		return
	}

	// When connection_ids is provided, use the explicit connection list
	// approach. The caller's scope_name labels the summary; the
	// generator sanitizes it and keys the cache on it.
	if connectionIDsStr != "" {
		connectionIDs, err := parseConnectionIDs(connectionIDsStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		filtered, ok := h.filterConnectionIDs(r.Context(), w, connectionIDs)
		if !ok {
			return
		}
		if len(filtered) == 0 {
			if err := json.NewEncoder(w).Encode(generatingResponse{Status: statusEmpty, Summary: nil}); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: overview: failed to encode empty response: %v\n", err)
			}
			return
		}

		overview, ok, err := h.connectionsSummary(w, r, filtered, scopeName, refresh)
		if !ok {
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: overview: connections summary failed: %v\n", err)
			http.Error(w, "Failed to generate connections summary", http.StatusInternalServerError)
			return
		}

		if err := json.NewEncoder(w).Encode(overview); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: overview: failed to encode connections response: %v\n", err)
		}
		return
	}

	// When no scope parameters are provided, return the estate-wide
	// overview. Restricted callers receive a scoped summary covering
	// only their visible connections instead of the full estate
	// snapshot.
	if scopeType == "" && scopeIDStr == "" {
		visible, allConns, err := h.resolveVisible(r.Context())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: overview: failed to resolve visible connections: %v\n", err)
			http.Error(w, "Failed to resolve connection visibility", http.StatusInternalServerError)
			return
		}
		if !allConns {
			ids := make([]int, 0, len(visible))
			for id := range visible {
				ids = append(ids, id)
			}
			ids = dedupeAndSort(ids)
			if len(ids) == 0 {
				if err := json.NewEncoder(w).Encode(generatingResponse{Status: statusEmpty, Summary: nil}); err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: overview: failed to encode empty response: %v\n", err)
				}
				return
			}
			overview, ok, err := h.connectionsSummary(w, r, ids, "", refresh)
			if !ok {
				return
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: overview: connections summary failed: %v\n", err)
				http.Error(w, "Failed to generate connections summary", http.StatusInternalServerError)
				return
			}
			if err := json.NewEncoder(w).Encode(overview); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: overview: failed to encode connections response: %v\n", err)
			}
			return
		}
		if refresh && h.allowEstateRefresh(r) {
			h.generator.ForceRefresh()
		}
		h.serveEstateOverview(w)
		return
	}

	// Both parameters must be provided together.
	if scopeType == "" || scopeIDStr == "" {
		http.Error(w, "Both scope_type and scope_id are required", http.StatusBadRequest)
		return
	}

	if !validScopeTypes[scopeType] {
		http.Error(w, "scope_type must be server, cluster, or group", http.StatusBadRequest)
		return
	}

	scopeID, err := strconv.Atoi(scopeIDStr)
	if err != nil || scopeID <= 0 {
		http.Error(w, "scope_id must be a positive integer", http.StatusBadRequest)
		return
	}

	intersect, unrestricted, ok := h.scopeVisible(r.Context(), w, scopeType, scopeID)
	if !ok {
		return
	}

	var overview *Overview
	admitted := false
	if unrestricted {
		overview, admitted, err = h.scopedSummary(w, r, scopeType, scopeID, refresh)
	} else {
		// Restricted visibility: generate a summary over only the scope
		// members the caller may see. The scope name is still useful for
		// the LLM prompt; fetch it so the summary reads correctly.
		scopeName := h.resolveScopeName(r.Context(), scopeType, scopeID)
		overview, admitted, err = h.connectionsSummary(w, r, intersect, scopeName, refresh)
	}
	if !admitted {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: scoped summary failed: %v\n", err)
		http.Error(w, "Failed to generate scoped summary", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(overview); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: failed to encode scoped response: %v\n", err)
	}
}

// serveEstateOverview returns the cached estate-wide overview, or a
// status response when no overview is available yet.
func (h *Handler) serveEstateOverview(w http.ResponseWriter) {
	if err := json.NewEncoder(w).Encode(overviewPayload(h.generator.EstateStatus())); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: failed to encode overview response: %v\n", err)
	}
}

// overviewPayload returns the value to encode for an overview: a
// generatingResponse for a status notice, or the overview itself.
func overviewPayload(overview *Overview) any {
	if overview.Status != "" {
		return generatingResponse{Status: overview.Status, Error: overview.Error}
	}
	return overview
}

// handleSSE serves an SSE stream of Overview updates for a given scope.
// Clients receive the current cached overview immediately, then subsequent
// updates as they are broadcast by the generator.
func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify streaming support.
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Disable the write deadline so the connection stays open.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		fmt.Fprintf(os.Stderr, "overview: failed to disable write deadline: %v\n", err)
	}

	// Determine scope from query parameters (reuse existing parsing logic).
	connectionIDsStr := r.URL.Query().Get("connection_ids")
	scopeName := r.URL.Query().Get("scope_name")
	scopeType := r.URL.Query().Get("scope_type")
	scopeIDStr := r.URL.Query().Get("scope_id")

	if connectionIDsStr != "" && (scopeType != "" || scopeIDStr != "") {
		http.Error(w, errScopeWithConnectionIDs, http.StatusBadRequest)
		return
	}

	// Build the scope key for subscribing. Estate-wide subscribers use
	// an empty key. A connections key carries the IDs and the effective
	// scope name, as the generator's cache key does, and connections
	// marks a summary over an explicit (possibly empty) ID list.
	var scopeKey string
	var connections bool
	var connectionIDs []int
	var effectiveName string
	var scopeID int
	if connectionIDsStr != "" {
		requested, err := parseConnectionIDs(connectionIDsStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		filtered, ok := h.filterConnectionIDs(r.Context(), w, requested)
		if !ok {
			return
		}
		// filterConnectionIDs returns a deduplicated, sorted slice so the
		// cache/SSE scope key is stable across equivalent inputs.
		connections = true
		connectionIDs = filtered
		effectiveName = sanitizeScopeName(scopeName)
	} else if scopeType != "" || scopeIDStr != "" {
		if scopeType == "" || scopeIDStr == "" {
			http.Error(w, "Both scope_type and scope_id are required", http.StatusBadRequest)
			return
		}
		if !validScopeTypes[scopeType] {
			http.Error(w, "scope_type must be server, cluster, or group", http.StatusBadRequest)
			return
		}
		var err error
		scopeID, err = strconv.Atoi(scopeIDStr)
		if err != nil || scopeID <= 0 {
			http.Error(w, "scope_id must be a positive integer", http.StatusBadRequest)
			return
		}
		intersect, unrestricted, ok := h.scopeVisible(r.Context(), w, scopeType, scopeID)
		if !ok {
			return
		}
		if unrestricted {
			scopeKey = fmt.Sprintf("%s:%d", scopeType, scopeID)
		} else {
			// Restricted visibility routes through the connections-summary
			// path over the visible members only. The name is looked up
			// only now that scopeVisible has passed, and any scope_name
			// from the caller is ignored. When the intersection is empty
			// the subscription is accepted but nothing is generated.
			connections = true
			connectionIDs = intersect
			if len(intersect) > 0 {
				effectiveName = sanitizeScopeName(h.resolveScopeName(r.Context(), scopeType, scopeID))
			}
		}
	} else {
		// No scope params: restricted callers receive a scoped SSE feed
		// covering only their visible connections instead of the full
		// estate-wide stream.
		visible, allConns, err := h.resolveVisible(r.Context())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: overview/sse: failed to resolve visible connections: %v\n", err)
			http.Error(w, "Failed to resolve connection visibility", http.StatusInternalServerError)
			return
		}
		if !allConns {
			ids := make([]int, 0, len(visible))
			for id := range visible {
				ids = append(ids, id)
			}
			// A restricted caller who can see nothing must get the
			// empty connections key, which nothing generates for,
			// rather than the estate-wide key, which is sent the cached
			// estate overview.
			connections = true
			connectionIDs = dedupeAndSort(ids)
		}
	}
	if connections {
		scopeKey = connectionsKey(connectionIDs, effectiveName)
	}

	// Check the caller's generation limits while an error response can
	// still be sent; a stale summary is served instead where one exists.
	stale, admitted := h.admitStream(w, r, scopeKey)
	if !admitted {
		return
	}

	// Set SSE headers and flush immediately so the client receives the
	// response headers without waiting for the first event.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	// Subscribe to updates.
	sub := h.hub.Subscribe(scopeKey)
	defer h.hub.Unsubscribe(sub)

	// Give the client something to show at once, so the card never
	// waits on a broadcast that may not come (issue #808). The estate
	// stream gets the cached overview or a status notice. A scoped
	// stream fetches its summary in the background and receives the
	// result on direct, which covers a cache hit or a failure, neither
	// of which broadcasts; a fresh summary also arrives through the hub
	// and the duplicate is dropped below. A caller who can see no
	// connections gets the empty notice, and the restricted path never
	// falls back to the unrestricted scope summary.
	var last *Overview
	send := func(overview *Overview) {
		if overview == last {
			return
		}
		last = overview
		h.writeSSEEvent(w, overview)
		flusher.Flush()
	}

	// The estate and empty notices are written before the stream loop
	// starts, so a summary broadcast cannot be overtaken by them.
	direct := make(chan *Overview, 1)
	deliver := func(overview *Overview, err error) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "overview: scoped generation failed: %v\n", err)
			overview = statusOverview(statusError, generationFailedMessage)
		}
		direct <- overview
	}
	// A scoped stream whose summary is not freshly cached opens with a
	// generating notice, as the estate stream does, so the card says
	// so rather than showing its skeleton for as long as the LLM call
	// takes. The scope key is also the generator's cache key.
	noticeIfUncached := func() {
		if _, fresh := h.generator.cachedSummary(scopeKey); !fresh {
			send(statusOverview(statusGenerating, ""))
		}
	}
	switch {
	case scopeKey == "":
		send(h.generator.EstateStatus())
	case connections && len(connectionIDs) == 0:
		send(statusOverview(statusEmpty, ""))
	case stale != nil:
		// The caller has used up their generations, so they get the
		// stale cached summary rather than a new one.
		send(stale)
	case connections:
		noticeIfUncached()
		go func() {
			deliver(h.generator.GetConnectionsSummary(connectionIDs, effectiveName, false))
		}()
	default:
		// The key is "<scope_type>:<scope_id>", validated above.
		noticeIfUncached()
		go func() {
			deliver(h.generator.GetScopedSummary(scopeType, scopeID, false))
		}()
	}

	// Stream loop: wait for updates or client disconnect.
	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case overview := <-direct:
			send(overview)
		case overview, ok := <-sub.C:
			if !ok {
				return
			}
			send(overview)
		case <-keepalive.C:
			fmt.Fprintf(w, ":keepalive\n\n")
			flusher.Flush()
		}
	}
}

// filterConnectionIDs restricts the supplied connection ID list to those
// visible to the caller. A nil checker makes every ID invisible, so
// the result is empty rather than unfiltered (issue #561). The second
// return value is false when the function wrote an error response and
// the caller must return immediately.
//
// The output is deduplicated and sorted so that equivalent input lists
// (same members, different order or duplicates) produce the same
// "connections:" cache key and SSE scope key, preventing duplicate cache
// entries and duplicate connection IDs from reaching the summary
// generator.
func (h *Handler) filterConnectionIDs(ctx context.Context, w http.ResponseWriter, ids []int) ([]int, bool) {
	visible, allConnections, err := h.resolveVisible(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: failed to resolve visible connections: %v\n", err)
		http.Error(w, "Failed to resolve connection visibility", http.StatusInternalServerError)
		return nil, false
	}
	if allConnections {
		return dedupeAndSort(ids), true
	}
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if !visible[id] {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sortInts(out)
	return out, true
}

// dedupeAndSort returns a new slice containing the unique elements of
// ids in ascending order. The input is not modified.
func dedupeAndSort(ids []int) []int {
	if len(ids) == 0 {
		return []int{}
	}
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sortInts(out)
	return out
}

// scopeVisible reports whether the caller may see the requested scope
// and, when they can, returns the intersection of the scope's member
// connection IDs with the caller's visible set. The check is never
// skipped: a nil checker sees no scope at all (issue #561).
// When the caller is a superuser or has a wildcard grant, (nil, true,
// true) is returned so the caller can use the full-scope snapshot path.
// On denial the function writes a 404 response and returns ok=false.
// The caller must return immediately when ok is false. Matching the
// behavior of sibling handlers, 404 is used for scope-denial to avoid
// leaking the existence of scopes the caller cannot see.
func (h *Handler) scopeVisible(ctx context.Context, w http.ResponseWriter, scopeType string, scopeID int) (intersect []int, unrestricted bool, ok bool) {
	visible, allConnections, err := h.resolveVisible(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: failed to resolve visible connections: %v\n", err)
		http.Error(w, "Failed to resolve connection visibility", http.StatusInternalServerError)
		return nil, false, false
	}
	if allConnections {
		return nil, true, true
	}

	switch scopeType {
	case "server":
		if !visible[scopeID] {
			http.Error(w, "Scope not found", http.StatusNotFound)
			return nil, false, false
		}
		return []int{scopeID}, false, true
	case "cluster", "group":
		ids, lookupErr := h.scopeMemberIDs(ctx, scopeType, scopeID)
		if lookupErr != nil {
			fmt.Fprintf(os.Stderr, "ERROR: overview: failed to check %s %d visibility: %v\n", scopeType, scopeID, lookupErr)
			http.Error(w, "Failed to check scope visibility", http.StatusInternalServerError)
			return nil, false, false
		}
		intersect = intersectVisible(ids, visible)
	default:
		http.Error(w, "Invalid scope_type", http.StatusBadRequest)
		return nil, false, false
	}

	if len(intersect) == 0 {
		http.Error(w, "Scope not found", http.StatusNotFound)
		return nil, false, false
	}
	return intersect, false, true
}

// scopeMemberIDs returns the member connection IDs of a cluster or
// group scope. Without a datastore there is no membership to read, so
// the scope has no members and a restricted caller cannot see it.
func (h *Handler) scopeMemberIDs(ctx context.Context, scopeType string, scopeID int) ([]int, error) {
	if h.datastore == nil {
		return nil, nil
	}
	if scopeType == "cluster" {
		return h.datastore.GetConnectionIDsForCluster(ctx, scopeID)
	}
	return h.datastore.GetConnectionIDsForGroup(ctx, scopeID)
}

// intersectVisible returns the subset of members that are also in the
// visible set. The result is deduplicated and sorted in ascending order
// so that equivalent inputs (same members in different order or with
// duplicates) produce the same cache/SSE scope key downstream.
func intersectVisible(members []int, visible map[int]bool) []int {
	if len(members) == 0 {
		return []int{}
	}
	seen := make(map[int]struct{}, len(members))
	out := make([]int, 0, len(members))
	for _, id := range members {
		if !visible[id] {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sortInts(out)
	return out
}

// resolveScopeName fetches the human-readable name for a scope so that
// the generator prompt can include it in the summary. Returns an empty
// string when the lookup fails; the generator tolerates an empty name.
func (h *Handler) resolveScopeName(ctx context.Context, scopeType string, scopeID int) string {
	if h.datastore == nil {
		return ""
	}
	switch scopeType {
	case "server":
		conn, err := h.datastore.GetConnection(ctx, scopeID)
		if err != nil || conn == nil {
			return ""
		}
		return conn.Name
	case "cluster":
		cluster, err := h.datastore.GetCluster(ctx, scopeID)
		if err != nil || cluster == nil {
			return ""
		}
		return cluster.Name
	case "group":
		group, err := h.datastore.GetClusterGroup(ctx, scopeID)
		if err != nil || group == nil {
			return ""
		}
		return group.Name
	}
	return ""
}

// resolveVisible returns the caller's visible connection-ID set via the
// overview handler's RBAC checker. The allConnections flag is true when
// the caller has unrestricted visibility (superuser or wildcard grant).
// A nil checker returns an empty set, because RBACChecker's methods
// deny on a nil receiver.
func (h *Handler) resolveVisible(ctx context.Context) (map[int]bool, bool, error) {
	lister := newOverviewVisibilityLister(h.datastore)
	ids, all, err := h.rbacChecker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		return nil, false, err
	}
	if all {
		return nil, true, nil
	}
	visible := make(map[int]bool, len(ids))
	for _, id := range ids {
		visible[id] = true
	}
	return visible, false, nil
}

// overviewVisibilityLister adapts *database.Datastore.GetAllConnections
// to the auth.ConnectionVisibilityLister interface. This duplicates the
// api-package adapter so the overview package does not depend on the
// api package.
type overviewVisibilityLister struct {
	ds *database.Datastore
}

func newOverviewVisibilityLister(ds *database.Datastore) auth.ConnectionVisibilityLister {
	if ds == nil {
		return nil
	}
	return &overviewVisibilityLister{ds: ds}
}

func (l *overviewVisibilityLister) GetAllConnections(ctx context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	conns, err := l.ds.GetAllConnections(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]auth.ConnectionVisibilityInfo, 0, len(conns))
	for i := range conns {
		result = append(result, auth.ConnectionVisibilityInfo{
			ID:            conns[i].ID,
			IsShared:      conns[i].IsShared,
			OwnerUsername: conns[i].OwnerUsername,
		})
	}
	return result, nil
}

// writeSSEEvent marshals an Overview to JSON and writes it as an SSE
// event. A status notice has no generation time, so it carries no
// event ID.
func (h *Handler) writeSSEEvent(w http.ResponseWriter, overview *Overview) {
	data, err := json.Marshal(overviewPayload(overview))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: failed to marshal SSE event: %v\n", err)
		return
	}
	if overview.Status == "" {
		fmt.Fprintf(w, "id: %s\n", overview.GeneratedAt.Format(time.RFC3339)) //nolint:gosec // G705: an RFC 3339 timestamp in an SSE stream, not HTML
	}
	fmt.Fprintf(w, "event: overview\ndata: %s\n\n", string(data)) //nolint:gosec // G705: JSON in an SSE stream, not HTML
}

// parseConnectionIDs parses a comma-separated string of connection IDs
// into a slice of positive integers. It returns an error when the input
// is empty, contains non-integer values, or contains non-positive IDs.
func parseConnectionIDs(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("connection_ids must not be empty")
	}

	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("connection_ids must be comma-separated positive integers")
		}
		if id <= 0 {
			return nil, fmt.Errorf("connection_ids must be comma-separated positive integers")
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("connection_ids must contain at least one valid ID")
	}

	return ids, nil
}
