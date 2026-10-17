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
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// ConnectionHandler handles REST API requests for database connection management
type ConnectionHandler struct {
	datastore     *database.Datastore
	authStore     *auth.AuthStore
	hostValidator *HostValidator
	rbacChecker   *auth.RBACChecker

	// visibilityListerFn builds the auth.ConnectionVisibilityLister passed
	// to RBACChecker.VisibleConnectionIDs. Tests can substitute a stub to
	// exercise the lister-error branch of listConnections without
	// breaking the shared datastore. When nil the handler falls back to
	// newConnectionVisibilityLister(h.datastore).
	visibilityListerFn func() auth.ConnectionVisibilityLister

	// denialRecorder writes a refused request to the audit log through
	// the RBAC handler's coalescing denial recorder, so a token-scope,
	// ownership or permission refusal here is audited the same way as
	// one on an RBAC route.
	// When nil, refusals are not audited (for example, when the server
	// runs without an auth store).
	denialRecorder denialRecorder

	// purgeNudge starts the background purge of a deleted connection's
	// metrics; see SetPurgeNudge. It may be nil.
	purgeNudge func()
}

// SetDenialRecorder installs the function used to audit refused
// requests; the server wires it to RBACHandler.RecordDenial.
func (h *ConnectionHandler) SetDenialRecorder(record func(*http.Request, string)) {
	h.denialRecorder = record
}

// refuseOutOfTokenScope audits a connection token-scope refusal and
// responds 403 with connectionOutOfTokenScope.
func (h *ConnectionHandler) refuseOutOfTokenScope(w http.ResponseWriter, r *http.Request) {
	h.refuse(w, r, connectionOutOfTokenScope)
}

// refuse audits a refused request and responds 403 with msg.
func (h *ConnectionHandler) refuse(w http.ResponseWriter, r *http.Request, msg string) {
	if h.denialRecorder != nil {
		h.denialRecorder(r, msg)
	}
	RespondError(w, http.StatusForbidden, msg)
}

// NewConnectionHandlerWithSecurity creates a new connection handler with custom security settings
func NewConnectionHandlerWithSecurity(datastore *database.Datastore, authStore *auth.AuthStore,
	rbacChecker *auth.RBACChecker, allowInternal bool, allowedHosts, blockedHosts []string) *ConnectionHandler {
	return &ConnectionHandler{
		datastore:     datastore,
		authStore:     authStore,
		hostValidator: NewHostValidator(allowInternal, allowedHosts, blockedHosts),
		rbacChecker:   rbacChecker,
	}
}

// ConnectionUpdateRequest is the request body for updating a connection (legacy, name-only)
type ConnectionUpdateRequest struct {
	Name *string `json:"name,omitempty"`
}

// ConnectionCreateRequest is the request body for creating a connection
type ConnectionCreateRequest struct {
	Name         string  `json:"name"`
	Description  *string `json:"description,omitempty"`
	Host         string  `json:"host"`
	HostAddr     *string `json:"hostaddr,omitempty"`
	Port         int     `json:"port"`
	DatabaseName string  `json:"database_name"`
	Username     string  `json:"username"`
	Password     string  `json:"password"`
	SSLMode      *string `json:"ssl_mode,omitempty"`
	SSLCert      *string `json:"ssl_cert_path,omitempty"`
	SSLKey       *string `json:"ssl_key_path,omitempty"`
	SSLRootCert  *string `json:"ssl_root_cert_path,omitempty"`
	IsShared     bool    `json:"is_shared"`
	IsMonitored  bool    `json:"is_monitored"`
}

// ConnectionFullUpdateRequest is the request body for full connection update
type ConnectionFullUpdateRequest struct {
	Name         *string `json:"name,omitempty"`
	Description  *string `json:"description,omitempty"`
	Host         *string `json:"host,omitempty"`
	HostAddr     *string `json:"hostaddr,omitempty"`
	Port         *int    `json:"port,omitempty"`
	DatabaseName *string `json:"database_name,omitempty"`
	Username     *string `json:"username,omitempty"`
	Password     *string `json:"password,omitempty"`
	SSLMode      *string `json:"ssl_mode,omitempty"`
	SSLCert      *string `json:"ssl_cert_path,omitempty"`
	SSLKey       *string `json:"ssl_key_path,omitempty"`
	SSLRootCert  *string `json:"ssl_root_cert_path,omitempty"`
	IsShared     *bool   `json:"is_shared,omitempty"`
	IsMonitored  *bool   `json:"is_monitored,omitempty"`
}

// CurrentConnectionRequest is the request body for setting current connection
type CurrentConnectionRequest struct {
	ConnectionID int     `json:"connection_id"`
	DatabaseName *string `json:"database_name,omitempty"`
}

// CurrentConnectionResponse is the response for current connection endpoints
type CurrentConnectionResponse struct {
	ConnectionID int     `json:"connection_id"`
	DatabaseName *string `json:"database_name,omitempty"`
	Host         string  `json:"host"`
	Port         int     `json:"port"`
	Name         string  `json:"name"`
}

// RegisterRoutes registers connection management routes on the mux
func (h *ConnectionHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		// Datastore not configured, register handlers that return appropriate errors
		notConfigured := HandleNotConfigured("Database connection management")
		mux.HandleFunc("/api/v1/connections", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/connections/", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/connections/current", authWrapper(notConfigured))
		return
	}

	// List all connections
	mux.HandleFunc("/api/v1/connections", authWrapper(h.handleConnections))

	// Connection-specific endpoints (databases list)
	mux.HandleFunc("/api/v1/connections/", authWrapper(h.handleConnectionSubpath))

	// Current connection selection
	mux.HandleFunc("/api/v1/connections/current", authWrapper(h.handleCurrentConnection))
}

// handleConnections handles GET/POST /api/connections
func (h *ConnectionHandler) handleConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listConnections(w, r)
	case http.MethodPost:
		h.createConnection(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listConnections handles GET /api/v1/connections
func (h *ConnectionHandler) listConnections(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	connections, err := h.datastore.GetAllConnections(ctx)
	if respondDBError(w, err, "list connections") {
		return
	}

	// RBAC: restrict to visible connections. VisibleConnectionIDs
	// returns allConnections=true for superusers and wildcard token
	// scopes; otherwise it returns the explicit set of visible IDs
	// (owned, shared, and group/token granted). Reuse the slice we
	// just loaded so visibility resolution and the response body are
	// computed from the same snapshot and we avoid a second
	// datastore read on every request.
	var lister auth.ConnectionVisibilityLister
	if h.visibilityListerFn != nil {
		lister = h.visibilityListerFn()
	} else {
		lister = database.NewSliceVisibilityLister(connections)
	}
	visibleIDs, allConnections, err := h.rbacChecker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to list connections")
		return
	}
	if !allConnections {
		visibleSet := make(map[int]bool, len(visibleIDs))
		for _, id := range visibleIDs {
			visibleSet[id] = true
		}
		filtered := connections[:0]
		for i := range connections {
			if visibleSet[connections[i].ID] {
				filtered = append(filtered, connections[i])
			}
		}
		connections = filtered
	}

	RespondJSON(w, http.StatusOK, connections)
}

// createConnection handles POST /api/v1/connections
func (h *ConnectionHandler) createConnection(w http.ResponseWriter, r *http.Request) {
	// Get current user info. The username is stamped onto the row as
	// owner_username, so we authenticate before the permission gate.
	username, _, err := getUserInfoCompat(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	// Issue #233 (follow-up to #207): gate ALL connection creation on
	// manage_connections, not only the shared branch. The previous gate
	// only fired when req.IsShared was true, so any authenticated caller
	// could create a non-shared server connection and silently expand
	// their visible topology. Check before decoding the body so denied
	// callers cannot probe payload shape via validation error messages.
	// The refusal is audited (issue #648).
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}

	// A new connection, shared or not, changes the estate every caller
	// sees, so, as for creating a cluster or a cluster group, an API
	// token must cover every connection at read_write (issues #471 and
	// #668). A token confined to some servers, or to read-only access,
	// may not add one. The refusal is audited as connection.create.
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker,
		denialRecorder(h.denialRecorder)) {
		return
	}

	// Parse request body
	req, ok := decodeBody[ConnectionCreateRequest](w, r)
	if !ok {
		return
	}

	// Validate required fields. Issue #269: validate the name's characters,
	// not just non-emptiness.
	if err := ValidateDisplayName(req.Name); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Host == "" {
		RespondError(w, http.StatusBadRequest, "Host is required")
		return
	}
	if req.Port <= 0 {
		RespondError(w, http.StatusBadRequest, "Port is required and must be positive")
		return
	}
	if req.DatabaseName == "" {
		RespondError(w, http.StatusBadRequest, "Maintenance Database is required")
		return
	}
	if req.Username == "" {
		RespondError(w, http.StatusBadRequest, "Username is required")
		return
	}
	if req.Password == "" {
		RespondError(w, http.StatusBadRequest, "Password is required")
		return
	}

	// Enforce the VARCHAR(255) limits on the backing columns before any
	// datastore call so over-length input returns a clear 400 instead of
	// a generic 500 when the database rejects the row (issue #270). The Name
	// column is already guarded by ValidateDisplayName above, which rejects
	// over-length raw names; the remaining fields have no such validator and
	// so are checked here.
	if !validateMaxLen(w, "Host", req.Host, maxFieldLength) {
		return
	}
	if !validateMaxLen(w, "Maintenance Database", req.DatabaseName, maxFieldLength) {
		return
	}
	if !validateMaxLen(w, "Username", req.Username, maxFieldLength) {
		return
	}

	// Validate host to prevent SSRF attacks
	if err := h.hostValidator.ValidateHost(req.Host); err != nil {
		log.Printf("[ERROR] Invalid host validation: %v", err)
		RespondError(w, http.StatusBadRequest, "Invalid host")
		return
	}

	// Validate port
	if err := h.hostValidator.ValidatePort(req.Port); err != nil {
		log.Printf("[ERROR] Invalid port validation: %v", err)
		RespondError(w, http.StatusBadRequest, "Invalid port")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Create connection
	params := database.ConnectionCreateParams{
		Name:          req.Name,
		Description:   req.Description,
		Host:          req.Host,
		HostAddr:      req.HostAddr,
		Port:          req.Port,
		DatabaseName:  req.DatabaseName,
		Username:      req.Username,
		Password:      req.Password,
		SSLMode:       req.SSLMode,
		SSLCert:       req.SSLCert,
		SSLKey:        req.SSLKey,
		SSLRootCert:   req.SSLRootCert,
		IsShared:      req.IsShared,
		IsMonitored:   req.IsMonitored,
		OwnerUsername: username,
	}

	conn, err := h.datastore.CreateConnection(ctx, params)
	if respondDBError(w, err, "create connection") {
		return
	}

	RespondJSON(w, http.StatusCreated, conn)
}

// handleConnectionSubpath handles /api/v1/connections/{id} and /api/v1/connections/{id}/databases
func (h *ConnectionHandler) handleConnectionSubpath(w http.ResponseWriter, r *http.Request) {
	// Parse the path: /api/v1/connections/{id} or /api/v1/connections/{id}/databases
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/connections/")

	// Handle /api/v1/connections/current separately
	if path == "current" {
		h.handleCurrentConnection(w, r)
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	// Parse connection ID
	connectionID, err := strconv.Atoi(parts[0])
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid connection ID")
		return
	}

	// Handle /api/v1/connections/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getConnection(w, r, connectionID)
		case http.MethodPut:
			h.updateConnection(w, r, connectionID)
		case http.MethodDelete:
			h.deleteConnection(w, r, connectionID)
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Handle /api/v1/connections/{id}/databases
	if len(parts) == 2 && parts[1] == "databases" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listDatabases(w, r, connectionID)
		return
	}

	// Handle /api/v1/connections/{id}/query
	if len(parts) == 2 && parts[1] == "query" {
		h.executeQuery(w, r, connectionID)
		return
	}

	// Handle /api/v1/connections/{id}/query/validate
	if len(parts) == 3 && parts[1] == "query" && parts[2] == "validate" {
		h.validateQuery(w, r, connectionID)
		return
	}

	// Handle /api/v1/connections/{id}/context
	if len(parts) == 2 && parts[1] == "context" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getConnectionContext(w, r, connectionID)
		return
	}

	// Handle /api/v1/connections/{id}/cluster
	if len(parts) == 2 && parts[1] == "cluster" {
		switch r.Method {
		case http.MethodGet:
			h.handleGetConnectionCluster(w, r, connectionID)
		case http.MethodPut:
			h.handleUpdateConnectionCluster(w, r, connectionID)
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// getConnection handles GET /api/v1/connections/{id}
func (h *ConnectionHandler) getConnection(w http.ResponseWriter, r *http.Request, id int) {
	// Check RBAC access to this connection. CanAccessConnection honors
	// ownership, sharing, group grants, and token scope; it also covers
	// the superuser bypass.
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), id); !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionRead, id, "Access denied")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := h.datastore.GetConnection(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Connection not found (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	RespondJSON(w, http.StatusOK, conn)
}

// connectionOutOfTokenScope is the refusal given when an API token's
// connection scope does not include the connection being changed.
const connectionOutOfTokenScope = "Permission denied: this token's scope does not include the connection"

// updateConnection handles PUT /api/v1/connections/{id}
func (h *ConnectionHandler) updateConnection(w http.ResponseWriter, r *http.Request, id int) {
	// Get current user info for permission check
	username, _, err := getUserInfoCompat(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	// Neither ownership nor manage_connections says which connections
	// an API token was issued for, so a scoped token is confined to its
	// own connections here as everywhere else. The check comes before
	// the lookup so that a refusal does not reveal whether the
	// connection exists.
	if !h.rbacChecker.ConnectionInTokenScope(r.Context(), id) {
		h.refuseOutOfTokenScope(w, r)
		return
	}

	hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, ok := h.loadConnectionForChange(ctx, w, r, id, hasManageConns, "update")
	if !ok {
		return
	}

	// Permission check: must be owner or have manage_connections permission
	isOwner := conn.OwnerUsername.Valid && conn.OwnerUsername.String == username
	if !hasManageConns && !isOwner {
		h.refuse(w, r, connectionUpdateDenied)
		return
	}

	// Parse request body - try full update request first
	var req ConnectionFullUpdateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate name if provided. Issue #269: when a name is supplied, it
	// must satisfy the full character policy, not merely be non-empty.
	if req.Name != nil {
		if err := ValidateDisplayName(*req.Name); err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// Only users with manage_connections permission can make connections shared
	if req.IsShared != nil && *req.IsShared && !hasManageConns {
		h.denialRecorder.refuse(w, r,
			"Permission denied: you do not have permission to make connections shared")
		return
	}

	// Validate host if being updated (SSRF protection)
	if req.Host != nil {
		if err := h.hostValidator.ValidateHost(*req.Host); err != nil {
			log.Printf("[ERROR] Invalid host validation on update: %v", err)
			RespondError(w, http.StatusBadRequest, "Invalid host")
			return
		}
	}

	// Validate port if being updated
	if req.Port != nil {
		if err := h.hostValidator.ValidatePort(*req.Port); err != nil {
			log.Printf("[ERROR] Invalid port validation on update: %v", err)
			RespondError(w, http.StatusBadRequest, "Invalid port")
			return
		}
	}

	// Build update params
	params := database.ConnectionUpdateParams{
		Name:         req.Name,
		Description:  req.Description,
		Host:         req.Host,
		HostAddr:     req.HostAddr,
		Port:         req.Port,
		DatabaseName: req.DatabaseName,
		Username:     req.Username,
		Password:     req.Password,
		SSLMode:      req.SSLMode,
		SSLCert:      req.SSLCert,
		SSLKey:       req.SSLKey,
		SSLRootCert:  req.SSLRootCert,
		IsShared:     req.IsShared,
		IsMonitored:  req.IsMonitored,
	}

	conn, err = h.datastore.UpdateConnectionFull(ctx, id, params)
	if err != nil {
		// The connection was deleted after the lookup above.
		if errors.Is(err, database.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Printf("[ERROR] Failed to update connection (id=%d): %v", id, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to update connection")
		return
	}

	RespondJSON(w, http.StatusOK, conn)
}

// deleteConnection handles DELETE /api/v1/connections/{id}
func (h *ConnectionHandler) deleteConnection(w http.ResponseWriter, r *http.Request, id int) {
	// Get current user info for permission check
	username, _, err := getUserInfoCompat(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	// Neither ownership nor manage_connections says which connections
	// an API token was issued for, so a scoped token is confined to its
	// own connections here as everywhere else. The check comes before
	// the lookup so that a refusal does not reveal whether the
	// connection exists.
	if !h.rbacChecker.ConnectionInTokenScope(r.Context(), id) {
		h.refuseOutOfTokenScope(w, r)
		return
	}

	hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, ok := h.loadConnectionForChange(ctx, w, r, id, hasManageConns, "delete")
	if !ok {
		return
	}

	// Permission check: must be owner or have manage_connections permission
	isOwner := conn.OwnerUsername.Valid && conn.OwnerUsername.String == username
	if !hasManageConns && !isOwner {
		h.refuse(w, r, connectionDeleteDenied)
		return
	}

	// Mark the connection deleted under its own, longer budget; see
	// connectionDeleteContext. The metrics go afterwards, in the
	// background (issue #669).
	deleteCtx, deleteCancel := connectionDeleteContext(r.Context())
	defer deleteCancel()
	if err := h.datastore.DeleteConnection(deleteCtx, id); err != nil {
		// A concurrent delete of the same connection can win the race
		// after the lookup above; that caller's request succeeded, so
		// this one answers as for any other missing connection.
		if errors.Is(err, database.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Printf("[ERROR] Failed to delete connection (id=%d): %v", id, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to delete connection")
		return
	}

	if h.purgeNudge != nil {
		h.purgeNudge()
	}

	w.WriteHeader(http.StatusNoContent)
}

// Refusal messages for a caller who can see a connection but may not
// change it.
const (
	connectionUpdateDenied = "Permission denied: you must be the owner or have the manage_connections permission to update this connection"
	connectionDeleteDenied = "Permission denied: you must be the owner or have the manage_connections permission to delete this connection"
)

// loadConnectionForChange loads connection id for an update or a
// delete, answering 404 when the caller cannot see it, exactly as when
// it does not exist (issue #677). The visibility check runs whether or
// not the connection exists, and both refusals are the same response,
// so neither the body nor the work done tells a caller which it was;
// an unseen connection is not audited, as there is nothing the caller
// was refused that it could know of. A caller holding manage_connections
// (canManage) may change any connection, so it is not limited to the
// ones it can see. The bool is false when the response has been written.
func (h *ConnectionHandler) loadConnectionForChange(ctx context.Context, w http.ResponseWriter,
	r *http.Request, id int, canManage bool, action string) (*database.MonitoredConnection, bool) {
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), id); !canManage && !canAccess {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return nil, false
	}

	conn, err := h.datastore.GetConnection(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Connection not found for %s (id=%d): %v", action, id, err)
		RespondError(w, http.StatusNotFound, "Connection not found")
		return nil, false
	}
	return conn, true
}

// SetPurgeNudge installs the function deleteConnection calls once a
// connection is marked deleted, to start the background purge of its
// metrics straight away. A nil function leaves the purge to its own
// interval.
func (h *ConnectionHandler) SetPurgeNudge(nudge func()) {
	h.purgeNudge = nudge
}

// connectionDeleteContext returns the context for marking a connection
// deleted. It carries database.ConnectionDeleteTimeout rather than the
// ten seconds the ownership lookup gets, because the mark step also
// deletes the connection's alerts and other dependent rows, which can
// be many for a connection monitored for a long time (issues #480 and
// #669). It is also detached from the request's cancellation, so a
// browser or reverse proxy that gives up waiting does not roll the
// delete back. The deadline still bounds the work.
func connectionDeleteContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), database.ConnectionDeleteTimeout)
}

// listDatabases handles GET /api/v1/connections/{id}/databases
func (h *ConnectionHandler) listDatabases(w http.ResponseWriter, r *http.Request, connectionID int) {
	// Check RBAC access to this connection.
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connectionID); !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionRead,
			connectionID, "Access denied")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	databases, err := h.datastore.ListDatabases(ctx, connectionID)
	if err != nil {
		log.Printf("[ERROR] Failed to list databases for connection %d: %v", connectionID, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to list databases")
		return
	}

	RespondJSON(w, http.StatusOK, databases)
}

// handleCurrentConnection handles GET/POST/DELETE /api/v1/connections/current
func (h *ConnectionHandler) handleCurrentConnection(w http.ResponseWriter, r *http.Request) {
	// Extract token hash from the request
	tokenHash := GetTokenHashFromRequest(r)
	if tokenHash == "" {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getCurrentConnection(w, r, tokenHash)
	case http.MethodPost:
		h.setCurrentConnection(w, r, tokenHash)
	case http.MethodDelete:
		h.clearCurrentConnection(w, r, tokenHash)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getCurrentConnection handles GET /api/v1/connections/current
func (h *ConnectionHandler) getCurrentConnection(w http.ResponseWriter, r *http.Request, tokenHash string) {
	session, err := h.authStore.GetConnectionSession(tokenHash)
	if err != nil {
		log.Printf("[ERROR] Failed to get current connection session: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to get current connection")
		return
	}

	if session == nil {
		RespondError(w, http.StatusNotFound, "No database connection selected")
		return
	}

	// Verify the caller may access the session's connection. A session
	// may outlive a revoked grant or ownership change, so re-check on
	// every read rather than trusting the stored selection. The client
	// reads this route whenever it loads, so a revoked grant is refused
	// on each visit; the coalescer folds those into one row a minute
	// (issue #675).
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), session.ConnectionID); !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionRead,
			session.ConnectionID, "Access denied")
		return
	}

	// Get connection details
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := h.datastore.GetConnection(ctx, session.ConnectionID)
	if err != nil {
		log.Printf("[ERROR] Failed to get connection details (id=%d): %v", session.ConnectionID, err) //nolint:gosec // G706: ConnectionID is an integer, cannot contain newlines
		RespondError(w, http.StatusInternalServerError,
			"Failed to get connection details")
		return
	}

	response := CurrentConnectionResponse{
		ConnectionID: session.ConnectionID,
		DatabaseName: session.DatabaseName,
		Host:         conn.Host,
		Port:         conn.Port,
		Name:         conn.Name,
	}

	RespondJSON(w, http.StatusOK, response)
}

// setCurrentConnection handles POST /api/v1/connections/current
func (h *ConnectionHandler) setCurrentConnection(w http.ResponseWriter, r *http.Request, tokenHash string) {
	req, ok := decodeBody[CurrentConnectionRequest](w, r)
	if !ok {
		return
	}

	if req.ConnectionID <= 0 {
		RespondError(w, http.StatusBadRequest, "connection_id is required")
		return
	}

	// Refuse a database override that could never name a database
	// before it is stored; it is validated again wherever it is read.
	if req.DatabaseName != nil {
		if err := database.ValidateDatabaseName(*req.DatabaseName); err != nil {
			RespondError(w, http.StatusBadRequest,
				"Invalid database name: "+err.Error())
			return
		}
	}

	// Check RBAC access before persisting the session; otherwise a
	// caller could pin their session to a connection they cannot use.
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), req.ConnectionID); !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionCurrentUpdate,
			req.ConnectionID, "Access denied")
		return
	}

	// Verify the connection exists
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := h.datastore.GetConnection(ctx, req.ConnectionID)
	if err != nil {
		log.Printf("[ERROR] Connection not found for set current (id=%d): %v", req.ConnectionID, err)
		RespondError(w, http.StatusBadRequest, "Connection not found")
		return
	}

	// Save the selection
	if err := h.authStore.SetConnectionSession(tokenHash, req.ConnectionID, req.DatabaseName); respondDBError(w, err, "save connection selection") {
		return
	}

	// Return the response
	response := CurrentConnectionResponse{
		ConnectionID: req.ConnectionID,
		DatabaseName: req.DatabaseName,
		Host:         conn.Host,
		Port:         conn.Port,
		Name:         conn.Name,
	}

	RespondJSON(w, http.StatusOK, response)
}

// clearCurrentConnection handles DELETE /api/v1/connections/current
func (h *ConnectionHandler) clearCurrentConnection(w http.ResponseWriter, r *http.Request, tokenHash string) {
	if err := h.authStore.ClearConnectionSession(tokenHash); respondDBError(w, err, "clear connection selection") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// getConnectionContext handles GET /api/v1/connections/{id}/context
func (h *ConnectionHandler) getConnectionContext(w http.ResponseWriter, r *http.Request, connectionID int) {
	// Check RBAC access to this connection.
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connectionID); !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionRead,
			connectionID, "Access denied")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	connCtx, err := h.datastore.GetConnectionContext(ctx, connectionID)
	if err != nil {
		log.Printf("[ERROR] Failed to get connection context (id=%d): %v", connectionID, err)
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	RespondJSON(w, http.StatusOK, connCtx)
}

// ConnectionClusterUpdateRequest is the request body for updating a
// connection's cluster assignment.
type ConnectionClusterUpdateRequest struct {
	ClusterID        *int    `json:"cluster_id"`
	Role             *string `json:"role"`
	MembershipSource string  `json:"membership_source"`
}

// knownConnectionRoles lists the values PUT /connections/{id}/cluster
// accepts for a connection's role: the node roles the collector's
// pg_node_role probe reports (and auto-detection copies into
// connections.role), the generic roles the client offers for an "other"
// cluster, and the "unknown" fallback auto-detection writes when no role
// is known.
var knownConnectionRoles = map[string]bool{
	"standalone":            true,
	"binary_primary":        true,
	"binary_standby":        true,
	"binary_cascading":      true,
	"logical_publisher":     true,
	"logical_subscriber":    true,
	"logical_bidirectional": true,
	"spock_node":            true,
	"spock_standby":         true,
	"primary":               true,
	"replica":               true,
	"node":                  true,
	"unknown":               true,
}

// connectionClusterResponse bundles current cluster info with available
// clusters so the UI can populate a selector in a single round-trip.
type connectionClusterResponse struct {
	Info          *database.ConnectionClusterInfo `json:"info"`
	Clusters      []database.ClusterSummary       `json:"clusters"`
	Relationships []database.NodeRelationship     `json:"relationships"`
}

// handleGetConnectionCluster handles GET /api/v1/connections/{id}/cluster
func (h *ConnectionHandler) handleGetConnectionCluster(w http.ResponseWriter, r *http.Request, connectionID int) {
	// Check RBAC access to this connection.
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connectionID); !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionRead,
			connectionID, "Access denied")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	info, err := h.datastore.GetConnectionClusterInfo(ctx, connectionID)
	if err != nil {
		log.Printf("[ERROR] Failed to get connection cluster info (id=%d): %v", connectionID, err)
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Issue #678: a restricted caller sees only the clusters, and the
	// relationships, whose connections they can see.
	visible, allConnections, err := resolveVisibleConnectionSet(ctx, h.rbacChecker, h.datastore)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for connection %d: %v", connectionID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
		return
	}

	clusters, err := h.datastore.ListClustersForAutocomplete(ctx)
	if err == nil && !allConnections {
		clusters, err = filterClustersByVisibility(ctx, h.datastore, clusters, visible)
	}
	if err != nil {
		log.Printf("[ERROR] Failed to list clusters for autocomplete: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
		return
	}

	// Fetch relationships for this connection's cluster if assigned
	connRelationships := []database.NodeRelationship{}
	if info.ClusterID != nil {
		allRels, err := h.datastore.GetClusterRelationships(ctx, *info.ClusterID)
		if err != nil {
			log.Printf("[WARN] Failed to get cluster relationships for connection %d: %v", connectionID, err)
		} else {
			for _, rel := range allRels {
				if rel.SourceConnectionID == connectionID || rel.TargetConnectionID == connectionID {
					connRelationships = append(connRelationships, rel)
				}
			}
		}
	}
	if !allConnections {
		connRelationships = filterRelationshipsByVisibility(connRelationships, visible)
	}

	RespondJSON(w, http.StatusOK, connectionClusterResponse{
		Info:          info,
		Clusters:      clusters,
		Relationships: connRelationships,
	})
}

// handleUpdateConnectionCluster handles PUT /api/v1/connections/{id}/cluster
func (h *ConnectionHandler) handleUpdateConnectionCluster(w http.ResponseWriter, r *http.Request, connectionID int) {
	// Check RBAC access to this connection.
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connectionID); !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionClusterUpdate,
			connectionID, "Access denied")
		return
	}

	// Issue #233 (follow-up to #207): reassigning a connection between
	// clusters mutates the topology, so it must require
	// manage_connections in addition to the visibility check above. The
	// previous gate stopped at CanAccessConnection, which permits any
	// user with read access to the connection (including via a group
	// share) to re-home it between clusters. Place the admin gate
	// AFTER the visibility check (so non-visible callers still see the
	// same 403 as before and learn nothing about the connection's
	// existence) but BEFORE DecodeJSONBody, so denied callers cannot
	// probe payload shape via validation error messages.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		const reason = "Permission denied: requires manage_connections permission"
		h.denialRecorder.refuseTarget(w, r, deniedActionConnectionClusterUpdate,
			connectionDenialTarget(connectionID), reason, reason)
		return
	}
	// Re-homing a connection changes it, so a read-only entry in the
	// token's connection scope is not enough (issue #471). The target
	// cluster gets the same visibility check below as the cluster on
	// POST /clusters/{id}/servers.
	if !h.rbacChecker.ConnectionInTokenScope(r.Context(), connectionID) {
		h.refuseOutOfTokenScope(w, r)
		return
	}

	var req ConnectionClusterUpdateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Role != nil && !knownConnectionRoles[*req.Role] {
		RespondError(w, http.StatusBadRequest, "Invalid role")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// A caller must not move a connection into a cluster whose members
	// it cannot see; such a cluster answers 404, as the GET does, so its
	// existence is not disclosed (issue #471). Unlike POST
	// /clusters/{id}/servers, a cluster with no members is allowed: the
	// server dialog creates a cluster and then assigns the new
	// connection to it with this request.
	if req.ClusterID != nil {
		visible, allConnections, err := resolveVisibleConnectionSet(ctx, h.rbacChecker, h.datastore)
		if respondDBError(w, err, "assign connection to cluster") {
			return
		}
		if !allConnections {
			members, err := h.datastore.GetConnectionIDsForCluster(ctx, *req.ClusterID)
			if respondDBError(w, err, "assign connection to cluster") {
				return
			}
			if membersHidden(members, visible) {
				RespondError(w, http.StatusNotFound, "Cluster not found")
				return
			}
		}
	}

	if req.ClusterID == nil && req.MembershipSource != "manual" {
		// Reset to auto-detection
		if err := h.datastore.ResetMembershipSource(ctx, connectionID); err != nil {
			log.Printf("[ERROR] Failed to reset membership source (id=%d): %v", connectionID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to reset membership source")
			return
		}
	} else {
		membershipSource := req.MembershipSource
		if membershipSource == "" {
			membershipSource = "auto"
		}
		if err := h.datastore.AssignConnectionToCluster(ctx, connectionID, req.ClusterID, req.Role, membershipSource); err != nil {
			// A missing cluster answers 404 like a hidden one, so the
			// two cannot be told apart (issue #471).
			if errors.Is(err, database.ErrClusterNotFound) {
				RespondError(w, http.StatusNotFound, "Cluster not found")
				return
			}
			if errors.Is(err, database.ErrConnectionNotFound) {
				RespondError(w, http.StatusNotFound, "Connection not found")
				return
			}
			log.Printf("[ERROR] Failed to assign connection to cluster (id=%d): %v", connectionID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to assign connection to cluster")
			return
		}
	}

	// Return updated info
	info, err := h.datastore.GetConnectionClusterInfo(ctx, connectionID)
	if err != nil {
		log.Printf("[ERROR] Failed to get updated connection cluster info (id=%d): %v", connectionID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to get updated cluster info")
		return
	}

	RespondJSON(w, http.StatusOK, info)
}
