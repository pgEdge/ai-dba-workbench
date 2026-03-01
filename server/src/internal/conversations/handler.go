/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package conversations

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/api"
	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// Handler handles conversation API requests
type Handler struct {
	store     *Store
	authStore *auth.AuthStore
}

// NewHandler creates a new conversation handler
func NewHandler(store *Store, authStore *auth.AuthStore) *Handler {
	return &Handler{
		store:     store,
		authStore: authStore,
	}
}

// extractUsername extracts the username from the session token
func (h *Handler) extractUsername(r *http.Request) (string, error) {
	token := auth.ExtractBearerToken(r)
	if token == "" {
		return "", fmt.Errorf("missing authentication credentials")
	}

	username, err := h.authStore.ValidateSessionToken(token)
	if err != nil {
		return "", fmt.Errorf("invalid or expired session")
	}

	return username, nil
}

// HandleList handles GET /api/conversations
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	username, err := h.extractUsername(r)
	if err != nil {
		api.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Parse pagination parameters
	limit := 50
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	conversations, err := h.store.List(r.Context(), username, limit, offset)
	if err != nil {
		api.RespondError(w, http.StatusInternalServerError, "Failed to list conversations")
		return
	}

	// Return empty array instead of null
	if conversations == nil {
		conversations = []ConversationSummary{}
	}

	api.RespondJSON(w, http.StatusOK, map[string]any{
		"conversations": conversations,
	})
}

// HandleGet handles GET /api/v1/conversations/{id}
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	username, err := h.extractUsername(r)
	if err != nil {
		api.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Extract ID from path
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/conversations/")
	if id == "" {
		api.RespondError(w, http.StatusBadRequest, "Conversation ID required")
		return
	}

	conv, err := h.store.Get(r.Context(), id, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			api.RespondError(w, http.StatusNotFound, "Conversation not found")
		} else {
			api.RespondError(w, http.StatusInternalServerError, "Failed to get conversation")
		}
		return
	}

	api.RespondJSON(w, http.StatusOK, conv)
}

// CreateRequest represents a request to create a conversation
type CreateRequest struct {
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Connection string    `json:"connection"`
	Messages   []Message `json:"messages"`
}

// HandleCreate handles POST /api/conversations
func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	username, err := h.extractUsername(r)
	if err != nil {
		api.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Messages) == 0 {
		api.RespondError(w, http.StatusBadRequest, "Messages required")
		return
	}

	conv, err := h.store.Create(r.Context(), username, req.Provider, req.Model, req.Connection, req.Messages)
	if err != nil {
		api.RespondError(w, http.StatusInternalServerError, "Failed to create conversation")
		return
	}

	api.RespondJSON(w, http.StatusCreated, conv)
}

// UpdateRequest represents a request to update a conversation
type UpdateRequest struct {
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Connection string    `json:"connection"`
	Messages   []Message `json:"messages"`
}

// HandleUpdate handles PUT /api/v1/conversations/{id}
func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	username, err := h.extractUsername(r)
	if err != nil {
		api.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Extract ID from path
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/conversations/")
	if id == "" {
		api.RespondError(w, http.StatusBadRequest, "Conversation ID required")
		return
	}

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	conv, err := h.store.Update(r.Context(), id, username, req.Provider, req.Model, req.Connection, req.Messages)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			api.RespondError(w, http.StatusNotFound, "Conversation not found")
		} else if errors.Is(err, ErrAccessDenied) {
			api.RespondError(w, http.StatusForbidden, "Access denied")
		} else {
			api.RespondError(w, http.StatusInternalServerError, "Failed to update conversation")
		}
		return
	}

	api.RespondJSON(w, http.StatusOK, conv)
}

// RenameRequest represents a request to rename a conversation
type RenameRequest struct {
	Title string `json:"title"`
}

// HandleRename handles PATCH /api/v1/conversations/{id}
func (h *Handler) HandleRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	username, err := h.extractUsername(r)
	if err != nil {
		api.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Extract ID from path
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/conversations/")
	if id == "" {
		api.RespondError(w, http.StatusBadRequest, "Conversation ID required")
		return
	}

	var req RenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Title == "" {
		api.RespondError(w, http.StatusBadRequest, "Title required")
		return
	}

	err = h.store.Rename(r.Context(), id, username, req.Title)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrAccessDenied) {
			api.RespondError(w, http.StatusNotFound, "Conversation not found")
		} else {
			api.RespondError(w, http.StatusInternalServerError, "Failed to rename conversation")
		}
		return
	}

	api.RespondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// HandleDelete handles DELETE /api/v1/conversations/{id}
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	username, err := h.extractUsername(r)
	if err != nil {
		api.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Extract ID from path
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/conversations/")
	if id == "" {
		api.RespondError(w, http.StatusBadRequest, "Conversation ID required")
		return
	}

	err = h.store.Delete(r.Context(), id, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrAccessDenied) {
			api.RespondError(w, http.StatusNotFound, "Conversation not found")
		} else {
			api.RespondError(w, http.StatusInternalServerError, "Failed to delete conversation")
		}
		return
	}

	api.RespondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// HandleDeleteAll handles DELETE /api/conversations (with query param all=true)
func (h *Handler) HandleDeleteAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	username, err := h.extractUsername(r)
	if err != nil {
		api.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	count, err := h.store.DeleteAll(r.Context(), username)
	if err != nil {
		api.RespondError(w, http.StatusInternalServerError, "Failed to delete conversations")
		return
	}

	api.RespondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"deleted": count,
	})
}

// RegisterRoutes registers conversation routes with the given mux
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	// List conversations
	mux.HandleFunc("/api/v1/conversations", authWrapper(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleList(w, r)
		case http.MethodPost:
			h.HandleCreate(w, r)
		case http.MethodDelete:
			// Delete all conversations
			if r.URL.Query().Get("all") == "true" {
				h.HandleDeleteAll(w, r)
			} else {
				api.RespondError(w, http.StatusBadRequest, "Use ?all=true to delete all conversations")
			}
		default:
			api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	}))

	// Single conversation operations
	mux.HandleFunc("/api/v1/conversations/", authWrapper(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleGet(w, r)
		case http.MethodPut:
			h.HandleUpdate(w, r)
		case http.MethodPatch:
			h.HandleRename(w, r)
		case http.MethodDelete:
			h.HandleDelete(w, r)
		default:
			api.RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	}))
}
