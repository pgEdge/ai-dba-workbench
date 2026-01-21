/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// ConnectionHandler handles REST API requests for database connection management
type ConnectionHandler struct {
	datastore *database.Datastore
	authStore *auth.AuthStore
}

// NewConnectionHandler creates a new connection handler
func NewConnectionHandler(datastore *database.Datastore, authStore *auth.AuthStore) *ConnectionHandler {
	return &ConnectionHandler{
		datastore: datastore,
		authStore: authStore,
	}
}

// ConnectionUpdateRequest is the request body for updating a connection
type ConnectionUpdateRequest struct {
	Name *string `json:"name,omitempty"`
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

// ErrorResponse is a standard error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// RegisterRoutes registers connection management routes on the mux
func (h *ConnectionHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		// Datastore not configured, register handlers that return appropriate errors
		mux.HandleFunc("/api/connections", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/connections/", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/connections/current", authWrapper(h.handleNotConfigured))
		return
	}

	// List all connections
	mux.HandleFunc("/api/connections", authWrapper(h.handleConnections))

	// Connection-specific endpoints (databases list)
	mux.HandleFunc("/api/connections/", authWrapper(h.handleConnectionSubpath))

	// Current connection selection
	mux.HandleFunc("/api/connections/current", authWrapper(h.handleCurrentConnection))
}

// handleNotConfigured returns an error when datastore is not configured
func (h *ConnectionHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	//nolint:errcheck // Encoding simple error response
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: "Database connection management is not available. The datastore is not configured.",
	})
}

// handleConnections handles GET /api/connections
func (h *ConnectionHandler) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	connections, err := h.datastore.GetAllConnections(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to list connections: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding connections list
	json.NewEncoder(w).Encode(connections)
}

// handleConnectionSubpath handles /api/connections/{id} and /api/connections/{id}/databases
func (h *ConnectionHandler) handleConnectionSubpath(w http.ResponseWriter, r *http.Request) {
	// Parse the path: /api/connections/{id} or /api/connections/{id}/databases
	path := strings.TrimPrefix(r.URL.Path, "/api/connections/")

	// Handle /api/connections/current separately
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid connection ID",
		})
		return
	}

	// Handle /api/connections/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getConnection(w, r, connectionID)
		case http.MethodPut:
			h.updateConnection(w, r, connectionID)
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Handle /api/connections/{id}/databases
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

// getConnection handles GET /api/connections/{id}
func (h *ConnectionHandler) getConnection(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := h.datastore.GetConnection(ctx, id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Connection not found: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding connection
	json.NewEncoder(w).Encode(conn)
}

// updateConnection handles PUT /api/connections/{id}
func (h *ConnectionHandler) updateConnection(w http.ResponseWriter, r *http.Request, id int) {
	// Get current user info for permission check
	username, isSuperuser, err := h.getUserInfoFromRequest(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid or missing authentication token",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get connection to check ownership
	conn, err := h.datastore.GetConnection(ctx, id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Connection not found: %v", err),
		})
		return
	}

	// Permission check: must be owner or superuser
	isOwner := conn.OwnerUsername.Valid && conn.OwnerUsername.String == username
	if !isSuperuser && !isOwner {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Permission denied: you must be the owner or a superuser to update this connection",
		})
		return
	}

	// Parse request body
	var req ConnectionUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid request body",
		})
		return
	}

	// Update connection name if provided
	if req.Name != nil {
		if *req.Name == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			//nolint:errcheck // Encoding simple error response
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "Name cannot be empty",
			})
			return
		}

		conn, err = h.datastore.UpdateConnectionName(ctx, id, *req.Name)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			//nolint:errcheck // Encoding simple error response
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: fmt.Sprintf("Failed to update connection: %v", err),
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding connection
	json.NewEncoder(w).Encode(conn)
}

// listDatabases handles GET /api/connections/{id}/databases
func (h *ConnectionHandler) listDatabases(w http.ResponseWriter, r *http.Request, connectionID int) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	databases, err := h.datastore.ListDatabases(ctx, connectionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to list databases: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding databases list
	json.NewEncoder(w).Encode(databases)
}

// getUserInfoFromRequest extracts username and superuser status from the request
func (h *ConnectionHandler) getUserInfoFromRequest(r *http.Request) (string, bool, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", false, fmt.Errorf("missing authorization header")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return "", false, fmt.Errorf("invalid authorization header format")
	}

	// Validate session token and get username
	username, err := h.authStore.ValidateSessionToken(token)
	if err != nil {
		return "", false, err
	}

	// Look up user to get superuser status
	user, err := h.authStore.GetUser(username)
	if err != nil {
		return username, false, nil // User exists but couldn't get details
	}

	return username, user.IsSuperuser, nil
}

// handleCurrentConnection handles GET/POST/DELETE /api/connections/current
func (h *ConnectionHandler) handleCurrentConnection(w http.ResponseWriter, r *http.Request) {
	// Extract token hash from the request
	tokenHash := h.getTokenHashFromRequest(r)
	if tokenHash == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid or missing authentication token",
		})
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

// getCurrentConnection handles GET /api/connections/current
func (h *ConnectionHandler) getCurrentConnection(w http.ResponseWriter, r *http.Request, tokenHash string) {
	session, err := h.authStore.GetConnectionSession(tokenHash)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to get current connection: %v", err),
		})
		return
	}

	if session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "No database connection selected",
		})
		return
	}

	// Get connection details
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := h.datastore.GetConnection(ctx, session.ConnectionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to get connection details: %v", err),
		})
		return
	}

	response := CurrentConnectionResponse{
		ConnectionID: session.ConnectionID,
		DatabaseName: session.DatabaseName,
		Host:         conn.Host,
		Port:         conn.Port,
		Name:         conn.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding connection response
	json.NewEncoder(w).Encode(response)
}

// setCurrentConnection handles POST /api/connections/current
func (h *ConnectionHandler) setCurrentConnection(w http.ResponseWriter, r *http.Request, tokenHash string) {
	var req CurrentConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid request body",
		})
		return
	}

	if req.ConnectionID <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "connection_id is required",
		})
		return
	}

	// Verify the connection exists
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := h.datastore.GetConnection(ctx, req.ConnectionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Connection not found: %v", err),
		})
		return
	}

	// Save the selection
	if err := h.authStore.SetConnectionSession(tokenHash, req.ConnectionID, req.DatabaseName); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to save connection selection: %v", err),
		})
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

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding connection response
	json.NewEncoder(w).Encode(response)
}

// clearCurrentConnection handles DELETE /api/connections/current
func (h *ConnectionHandler) clearCurrentConnection(w http.ResponseWriter, r *http.Request, tokenHash string) {
	if err := h.authStore.ClearConnectionSession(tokenHash); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to clear connection selection: %v", err),
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// getTokenHashFromRequest extracts and hashes the token from the Authorization header
func (h *ConnectionHandler) getTokenHashFromRequest(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return ""
	}

	return auth.GetTokenHashByRawToken(token)
}
