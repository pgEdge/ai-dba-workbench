/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package session manages user session state including database context
package session

import (
    "fmt"
    "sync"
)

// DatabaseContext represents the current database context for a user session
type DatabaseContext struct {
    ConnectionID int    `json:"connectionId"`
    DatabaseName string `json:"databaseName,omitempty"` // Optional database name override
}

// SessionManager manages session state for all users
type SessionManager struct {
    mu       sync.RWMutex
    contexts map[string]*DatabaseContext // keyed by username
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
    return &SessionManager{
        contexts: make(map[string]*DatabaseContext),
    }
}

// SetContext sets the database context for a user
func (sm *SessionManager) SetContext(username string, connectionID int, databaseName string) error {
    if username == "" {
        return fmt.Errorf("username cannot be empty")
    }

    sm.mu.Lock()
    defer sm.mu.Unlock()

    sm.contexts[username] = &DatabaseContext{
        ConnectionID: connectionID,
        DatabaseName: databaseName,
    }

    return nil
}

// GetContext retrieves the database context for a user
// Returns nil if no context is set
func (sm *SessionManager) GetContext(username string) *DatabaseContext {
    if username == "" {
        return nil
    }

    sm.mu.RLock()
    defer sm.mu.RUnlock()

    ctx, exists := sm.contexts[username]
    if !exists {
        return nil
    }

    // Return a copy to prevent external modification
    return &DatabaseContext{
        ConnectionID: ctx.ConnectionID,
        DatabaseName: ctx.DatabaseName,
    }
}

// ClearContext removes the database context for a user
func (sm *SessionManager) ClearContext(username string) {
    if username == "" {
        return
    }

    sm.mu.Lock()
    defer sm.mu.Unlock()

    delete(sm.contexts, username)
}

// HasContext checks if a user has a context set
func (sm *SessionManager) HasContext(username string) bool {
    if username == "" {
        return false
    }

    sm.mu.RLock()
    defer sm.mu.RUnlock()

    _, exists := sm.contexts[username]
    return exists
}

// Global session manager instance
var globalManager = NewSessionManager()

// SetContext sets the database context for a user (convenience function)
func SetContext(username string, connectionID int, databaseName string) error {
    return globalManager.SetContext(username, connectionID, databaseName)
}

// GetContext retrieves the database context for a user (convenience function)
func GetContext(username string) *DatabaseContext {
    return globalManager.GetContext(username)
}

// ClearContext removes the database context for a user (convenience function)
func ClearContext(username string) {
    globalManager.ClearContext(username)
}

// HasContext checks if a user has a context set (convenience function)
func HasContext(username string) bool {
    return globalManager.HasContext(username)
}
