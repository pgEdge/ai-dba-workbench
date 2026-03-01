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
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/memory"
)

// MemoryHandler handles REST API requests for chat memories
type MemoryHandler struct {
	memoryStore *memory.Store
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
}

// NewMemoryHandler creates a new memory handler
func NewMemoryHandler(
	memoryStore *memory.Store,
	authStore *auth.AuthStore,
	rbacChecker *auth.RBACChecker,
) *MemoryHandler {
	return &MemoryHandler{
		memoryStore: memoryStore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// RegisterRoutes registers memory management routes on the mux
func (h *MemoryHandler) RegisterRoutes(
	mux *http.ServeMux,
	authWrapper func(http.HandlerFunc) http.HandlerFunc,
) {
	if h.memoryStore == nil {
		notConfigured := HandleNotConfigured("Memory management")
		mux.HandleFunc("/api/v1/memories", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/memories/", authWrapper(notConfigured))
		return
	}

	mux.HandleFunc("/api/v1/memories", authWrapper(h.listMemories))
	mux.HandleFunc("/api/v1/memories/", authWrapper(h.handleMemoryByID))
}

// listMemories handles GET /api/v1/memories
func (h *MemoryHandler) listMemories(w http.ResponseWriter, r *http.Request) {
	if !RequireGET(w, r) {
		return
	}

	username := auth.GetUsernameFromContext(r.Context())
	if username == "" {
		RespondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	category := ParseQueryString(r, "category")
	limit := ParseLimitWithDefaults(r, 100, 1000)

	memories, err := h.memoryStore.ListByUser(r.Context(), username, category, limit)
	if err != nil {
		log.Printf("[ERROR] Failed to list memories: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list memories")
		return
	}

	if memories == nil {
		memories = []memory.Memory{}
	}

	RespondJSON(w, http.StatusOK, map[string]any{
		"memories": memories,
	})
}

// handleMemoryByID dispatches requests to /api/v1/memories/{id} by method
func (h *MemoryHandler) handleMemoryByID(w http.ResponseWriter, r *http.Request) {
	// Parse the ID from the URL path
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/memories/")
	if idStr == "" {
		RespondError(w, http.StatusBadRequest, "Memory ID is required")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid memory ID")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		h.deleteMemory(w, r, id)
	case http.MethodPatch:
		h.updateMemoryPinned(w, r, id)
	default:
		w.Header().Set("Allow", "DELETE, PATCH")
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// deleteMemory handles DELETE /api/v1/memories/{id}
func (h *MemoryHandler) deleteMemory(
	w http.ResponseWriter,
	r *http.Request,
	id int64,
) {
	username := auth.GetUsernameFromContext(r.Context())
	if username == "" {
		RespondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	// Fetch the memory to check its scope
	mem, err := h.memoryStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			RespondError(w, http.StatusNotFound, "Memory not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch memory %d: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch memory")
		return
	}

	if mem.Scope == "system" {
		// System-scoped memories require the store_system_memory permission
		if !h.rbacChecker.HasAdminPermission(r.Context(), "store_system_memory") {
			RespondError(w, http.StatusForbidden,
				"Permission denied: you do not have permission to delete system memories")
			return
		}
		// Admin-level deletion without ownership check
		if err := h.memoryStore.DeleteByID(r.Context(), id); err != nil {
			if errors.Is(err, memory.ErrNotFound) {
				RespondError(w, http.StatusNotFound, "Memory not found")
				return
			}
			log.Printf("[ERROR] Failed to delete memory %d: %v", id, err)
			RespondError(w, http.StatusInternalServerError, "Failed to delete memory")
			return
		}
	} else {
		// User-scoped memory: must be owned by the caller
		if err := h.memoryStore.Delete(r.Context(), id, username); err != nil {
			if errors.Is(err, memory.ErrNotFound) {
				RespondError(w, http.StatusNotFound, "Memory not found")
				return
			}
			log.Printf("[ERROR] Failed to delete memory %d: %v", id, err)
			RespondError(w, http.StatusInternalServerError, "Failed to delete memory")
			return
		}
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// updatePinnedRequest is the request body for PATCH /api/v1/memories/{id}
type updatePinnedRequest struct {
	Pinned bool `json:"pinned"`
}

// updateMemoryPinned handles PATCH /api/v1/memories/{id}
func (h *MemoryHandler) updateMemoryPinned(
	w http.ResponseWriter,
	r *http.Request,
	id int64,
) {
	username := auth.GetUsernameFromContext(r.Context())
	if username == "" {
		RespondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	var req updatePinnedRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if err := h.memoryStore.UpdatePinned(r.Context(), id, username, req.Pinned); err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			RespondError(w, http.StatusNotFound, "Memory not found or not accessible")
			return
		}
		log.Printf("[ERROR] Failed to update pinned status for memory %d: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update memory")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"pinned": req.Pinned,
	})
}
