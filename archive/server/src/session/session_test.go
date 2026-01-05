/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package session

import (
    "sync"
    "testing"
)

func TestSetAndGetContext(t *testing.T) {
    sm := NewSessionManager()

    // Test setting context
    err := sm.SetContext("alice", 1, "testdb")
    if err != nil {
        t.Errorf("SetContext failed: %v", err)
    }

    // Test getting context
    ctx := sm.GetContext("alice")
    if ctx == nil {
        t.Fatal("GetContext returned nil")
    }

    if ctx.ConnectionID != 1 {
        t.Errorf("Expected ConnectionID 1, got %d", ctx.ConnectionID)
    }

    if ctx.DatabaseName != "testdb" {
        t.Errorf("Expected DatabaseName 'testdb', got '%s'", ctx.DatabaseName)
    }
}

func TestGetContextNonExistent(t *testing.T) {
    sm := NewSessionManager()

    ctx := sm.GetContext("nonexistent")
    if ctx != nil {
        t.Error("Expected nil for nonexistent user")
    }
}

func TestClearContext(t *testing.T) {
    sm := NewSessionManager()

    // Set context
    err := sm.SetContext("alice", 1, "testdb")
    if err != nil {
        t.Errorf("SetContext failed: %v", err)
    }

    // Verify it exists
    if !sm.HasContext("alice") {
        t.Error("Context should exist after SetContext")
    }

    // Clear context
    sm.ClearContext("alice")

    // Verify it's gone
    if sm.HasContext("alice") {
        t.Error("Context should not exist after ClearContext")
    }

    ctx := sm.GetContext("alice")
    if ctx != nil {
        t.Error("Expected nil after ClearContext")
    }
}

func TestHasContext(t *testing.T) {
    sm := NewSessionManager()

    // Initially should not have context
    if sm.HasContext("alice") {
        t.Error("Should not have context initially")
    }

    // Set context
    err := sm.SetContext("alice", 1, "testdb")
    if err != nil {
        t.Errorf("SetContext failed: %v", err)
    }

    // Should now have context
    if !sm.HasContext("alice") {
        t.Error("Should have context after SetContext")
    }
}

func TestEmptyUsername(t *testing.T) {
    sm := NewSessionManager()

    // Test SetContext with empty username
    err := sm.SetContext("", 1, "testdb")
    if err == nil {
        t.Error("Expected error for empty username in SetContext")
    }

    // Test GetContext with empty username
    ctx := sm.GetContext("")
    if ctx != nil {
        t.Error("Expected nil for empty username in GetContext")
    }

    // Test HasContext with empty username
    if sm.HasContext("") {
        t.Error("Expected false for empty username in HasContext")
    }

    // Test ClearContext with empty username (should not panic)
    sm.ClearContext("")
}

func TestContextIsolation(t *testing.T) {
    sm := NewSessionManager()

    // Set contexts for multiple users
    err := sm.SetContext("alice", 1, "alicedb")
    if err != nil {
        t.Errorf("SetContext failed: %v", err)
    }

    err = sm.SetContext("bob", 2, "bobdb")
    if err != nil {
        t.Errorf("SetContext failed: %v", err)
    }

    // Verify each user has their own context
    aliceCtx := sm.GetContext("alice")
    if aliceCtx == nil || aliceCtx.ConnectionID != 1 || aliceCtx.DatabaseName != "alicedb" {
        t.Error("Alice's context is incorrect")
    }

    bobCtx := sm.GetContext("bob")
    if bobCtx == nil || bobCtx.ConnectionID != 2 || bobCtx.DatabaseName != "bobdb" {
        t.Error("Bob's context is incorrect")
    }
}

func TestContextUpdate(t *testing.T) {
    sm := NewSessionManager()

    // Set initial context
    err := sm.SetContext("alice", 1, "testdb")
    if err != nil {
        t.Errorf("SetContext failed: %v", err)
    }

    // Update context
    err = sm.SetContext("alice", 2, "newdb")
    if err != nil {
        t.Errorf("SetContext update failed: %v", err)
    }

    // Verify updated values
    ctx := sm.GetContext("alice")
    if ctx == nil {
        t.Fatal("GetContext returned nil after update")
    }

    if ctx.ConnectionID != 2 {
        t.Errorf("Expected ConnectionID 2 after update, got %d", ctx.ConnectionID)
    }

    if ctx.DatabaseName != "newdb" {
        t.Errorf("Expected DatabaseName 'newdb' after update, got '%s'", ctx.DatabaseName)
    }
}

func TestEmptyDatabaseName(t *testing.T) {
    sm := NewSessionManager()

    // Set context without database name
    err := sm.SetContext("alice", 1, "")
    if err != nil {
        t.Errorf("SetContext failed: %v", err)
    }

    ctx := sm.GetContext("alice")
    if ctx == nil {
        t.Fatal("GetContext returned nil")
    }

    if ctx.ConnectionID != 1 {
        t.Errorf("Expected ConnectionID 1, got %d", ctx.ConnectionID)
    }

    if ctx.DatabaseName != "" {
        t.Errorf("Expected empty DatabaseName, got '%s'", ctx.DatabaseName)
    }
}

func TestConcurrentAccess(t *testing.T) {
    sm := NewSessionManager()
    var wg sync.WaitGroup
    numGoroutines := 100

    // Concurrent writes
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            username := "user" + string(rune('0'+id%10))
            err := sm.SetContext(username, id, "db"+string(rune('0'+id%10)))
            if err != nil {
                t.Errorf("SetContext failed: %v", err)
            }
        }(i)
    }

    wg.Wait()

    // Concurrent reads
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            username := "user" + string(rune('0'+id%10))
            ctx := sm.GetContext(username)
            if ctx == nil {
                t.Error("GetContext returned nil")
            }
        }(i)
    }

    wg.Wait()
}

func TestGlobalFunctions(t *testing.T) {
    // Note: These test the global convenience functions
    // They share global state, so we need to be careful

    username := "test_global_user"

    // Clean up before test
    ClearContext(username)

    // Test global SetContext
    err := SetContext(username, 1, "testdb")
    if err != nil {
        t.Errorf("Global SetContext failed: %v", err)
    }

    // Test global HasContext
    if !HasContext(username) {
        t.Error("Global HasContext returned false")
    }

    // Test global GetContext
    ctx := GetContext(username)
    if ctx == nil {
        t.Fatal("Global GetContext returned nil")
    }

    if ctx.ConnectionID != 1 {
        t.Errorf("Expected ConnectionID 1, got %d", ctx.ConnectionID)
    }

    // Test global ClearContext
    ClearContext(username)

    if HasContext(username) {
        t.Error("Global HasContext should return false after clear")
    }
}
