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
}

// NewConnectionHandler creates a new connection handler
func NewConnectionHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *ConnectionHandler {
	return &ConnectionHandler{
		datastore:     datastore,
		authStore:     authStore,
		hostValidator: DefaultHostValidator(),
		rbacChecker:   rbacChecker,
	}
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
	Host         string  `json:"host"`
	HostAddr     *string `json:"hostaddr,omitempty"`
	Port         int     `json:"port"`
	DatabaseName string  `json:"database_name"`
	Username     string  `json:"username"`
	Password     string  `json:"password"`
	SSLMode      *string `json:"sslmode,omitempty"`
	SSLCert      *string `json:"sslcert,omitempty"`
	SSLKey       *string `json:"sslkey,omitempty"`
	SSLRootCert  *string `json:"sslrootcert,omitempty"`
	IsShared     bool    `json:"is_shared"`
	IsMonitored  bool    `json:"is_monitored"`
}

// ConnectionFullUpdateRequest is the request body for full connection update
type ConnectionFullUpdateRequest struct {
	Name         *string `json:"name,omitempty"`
	Host         *string `json:"host,omitempty"`
	HostAddr     *string `json:"hostaddr,omitempty"`
	Port         *int    `json:"port,omitempty"`
	DatabaseName *string `json:"database_name,omitempty"`
	Username     *string `json:"username,omitempty"`
	Password     *string `json:"password,omitempty"`
	SSLMode      *string `json:"sslmode,omitempty"`
	SSLCert      *string `json:"sslcert,omitempty"`
	SSLKey       *string `json:"sslkey,omitempty"`
	SSLRootCert  *string `json:"sslrootcert,omitempty"`
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
		mux.HandleFunc("/api/v1/connections", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/v1/connections/", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/v1/connections/current", authWrapper(h.handleNotConfigured))
		return
	}

	// List all connections
	mux.HandleFunc("/api/v1/connections", authWrapper(h.handleConnections))

	// Connection-specific endpoints (databases list)
	mux.HandleFunc("/api/v1/connections/", authWrapper(h.handleConnectionSubpath))

	// Current connection selection
	mux.HandleFunc("/api/v1/connections/current", authWrapper(h.handleCurrentConnection))
}

// handleNotConfigured returns an error when datastore is not configured
func (h *ConnectionHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	RespondError(w, http.StatusServiceUnavailable,
		"Database connection management is not available. The datastore is not configured.")
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
	if err != nil {
		log.Printf("[ERROR] Failed to list connections: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to list connections")
		return
	}

	// Filter connections based on RBAC privileges
	accessibleIDs := h.rbacChecker.GetAccessibleConnections(r.Context())
	if accessibleIDs != nil {
		// Build a set of accessible connection IDs for efficient lookup
		accessibleSet := make(map[int]bool, len(accessibleIDs))
		for _, id := range accessibleIDs {
			accessibleSet[id] = true
		}
		filtered := connections[:0]
		for _, conn := range connections {
			if accessibleSet[conn.ID] {
				filtered = append(filtered, conn)
			}
		}
		connections = filtered
	}

	RespondJSON(w, http.StatusOK, connections)
}

// createConnection handles POST /api/v1/connections
func (h *ConnectionHandler) createConnection(w http.ResponseWriter, r *http.Request) {
	// Get current user info
	username, _, err := h.getUserInfoFromRequest(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	// Parse request body
	var req ConnectionCreateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate required fields
	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
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

	// Only users with manage_connections permission can create shared connections
	if req.IsShared && !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have permission to create shared connections")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Create connection
	params := database.ConnectionCreateParams{
		Name:          req.Name,
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
	if err != nil {
		log.Printf("[ERROR] Failed to create connection: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to create connection")
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

	http.NotFound(w, r)
}

// getConnection handles GET /api/v1/connections/{id}
func (h *ConnectionHandler) getConnection(w http.ResponseWriter, r *http.Request, id int) {
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

// updateConnection handles PUT /api/v1/connections/{id}
func (h *ConnectionHandler) updateConnection(w http.ResponseWriter, r *http.Request, id int) {
	// Get current user info for permission check
	username, _, err := h.getUserInfoFromRequest(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get connection to check ownership
	conn, err := h.datastore.GetConnection(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Connection not found for update (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Permission check: must be owner or have manage_connections permission
	isOwner := conn.OwnerUsername.Valid && conn.OwnerUsername.String == username
	if !hasManageConns && !isOwner {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you must be the owner or have the manage_connections permission to update this connection")
		return
	}

	// Parse request body - try full update request first
	var req ConnectionFullUpdateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate name if provided
	if req.Name != nil && *req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name cannot be empty")
		return
	}

	// Only users with manage_connections permission can make connections shared
	if req.IsShared != nil && *req.IsShared && !hasManageConns {
		RespondError(w, http.StatusForbidden,
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
	username, _, err := h.getUserInfoFromRequest(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get connection to check ownership
	conn, err := h.datastore.GetConnection(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Connection not found for delete (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Permission check: must be owner or have manage_connections permission
	isOwner := conn.OwnerUsername.Valid && conn.OwnerUsername.String == username
	if !hasManageConns && !isOwner {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you must be the owner or have the manage_connections permission to delete this connection")
		return
	}

	// Delete the connection
	if err := h.datastore.DeleteConnection(ctx, id); err != nil {
		log.Printf("[ERROR] Failed to delete connection (id=%d): %v", id, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to delete connection")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// listDatabases handles GET /api/v1/connections/{id}/databases
func (h *ConnectionHandler) listDatabases(w http.ResponseWriter, r *http.Request, connectionID int) {
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

// getUserInfoFromRequest extracts username and superuser status from the request.
// Deprecated: Use GetUserInfoFromRequest from request_helpers.go instead.
func (h *ConnectionHandler) getUserInfoFromRequest(r *http.Request) (string, bool, error) {
	info, err := GetUserInfoFromRequest(r, h.authStore)
	if err != nil {
		return "", false, err
	}
	return info.Username, info.IsSuperuser, nil
}

// handleCurrentConnection handles GET/POST/DELETE /api/v1/connections/current
func (h *ConnectionHandler) handleCurrentConnection(w http.ResponseWriter, r *http.Request) {
	// Extract token hash from the request
	tokenHash := h.getTokenHashFromRequest(r)
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

	// Get connection details
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := h.datastore.GetConnection(ctx, session.ConnectionID)
	if err != nil {
		log.Printf("[ERROR] Failed to get connection details (id=%d): %v", session.ConnectionID, err)
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
	var req CurrentConnectionRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.ConnectionID <= 0 {
		RespondError(w, http.StatusBadRequest, "connection_id is required")
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
	if err := h.authStore.SetConnectionSession(tokenHash, req.ConnectionID, req.DatabaseName); err != nil {
		log.Printf("[ERROR] Failed to save connection selection: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to save connection selection")
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
	if err := h.authStore.ClearConnectionSession(tokenHash); err != nil {
		log.Printf("[ERROR] Failed to clear connection selection: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to clear connection selection")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// getTokenHashFromRequest extracts and hashes the token from the Authorization header.
// Deprecated: Use GetTokenHashFromRequest from request_helpers.go instead.
func (h *ConnectionHandler) getTokenHashFromRequest(r *http.Request) string {
	return GetTokenHashFromRequest(r)
}
