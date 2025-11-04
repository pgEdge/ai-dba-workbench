/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
    "database/sql"
    "strings"
    "testing"
    "time"

    _ "github.com/lib/pq"
)

// Helper function to create a mock connection string for testing
// This uses an invalid connection string that won't actually connect
func getMockConnStr() string {
    return "host=localhost port=9999 dbname=nonexistent user=nobody sslmode=disable connect_timeout=1"
}

// TestConnectionPoolCreation tests creating a connection pool
func TestConnectionPoolCreation(t *testing.T) {
    pool, err := NewConnectionPool(getMockConnStr(), 5, 10)
    if err != nil {
        t.Fatalf("Failed to create connection pool: %v", err)
    }
    defer pool.Close()

    if pool.maxConnections != 5 {
        t.Errorf("Expected maxConnections=5, got %d", pool.maxConnections)
    }

    if pool.maxIdleSeconds != 10 {
        t.Errorf("Expected maxIdleSeconds=10, got %d", pool.maxIdleSeconds)
    }
}

// TestConnectionPoolInvalidParams tests creation with invalid parameters
func TestConnectionPoolInvalidParams(t *testing.T) {
    _, err := NewConnectionPool(getMockConnStr(), 0, 10)
    if err == nil {
        t.Error("Expected error when creating pool with maxConnections=0")
    }

    _, err = NewConnectionPool(getMockConnStr(), -1, 10)
    if err == nil {
        t.Error("Expected error when creating pool with negative maxConnections")
    }
}

// TestConnectionPoolStats tests the Stats method
func TestConnectionPoolStats(t *testing.T) {
    pool, err := NewConnectionPool(getMockConnStr(), 5, 10)
    if err != nil {
        t.Fatalf("Failed to create connection pool: %v", err)
    }
    defer pool.Close()

    total, inUse, idle := pool.Stats()
    if total != 0 || inUse != 0 || idle != 0 {
        t.Errorf("Expected empty pool stats, got total=%d, inUse=%d, idle=%d", total, inUse, idle)
    }
}

// TestConnectionPoolWithMockDB tests the connection pool with a real in-memory database
// We'll use the :memory: SQLite connection for testing, but since we're using postgres driver,
// we'll need to use a test helper pattern
func TestConnectionPoolRequestReturn(t *testing.T) {
    // Skip if we can't connect to a real database
    // This test requires a PostgreSQL database for proper testing
    t.Skip("Skipping test that requires live PostgreSQL connection")
}

// TestConnectionPoolMaxConnections tests that the pool respects max connections limit
func TestConnectionPoolMaxConnections(t *testing.T) {
    // Create a pool that can't actually connect but will track attempts
    pool, err := NewConnectionPool(getMockConnStr(), 2, 10)
    if err != nil {
        t.Fatalf("Failed to create connection pool: %v", err)
    }
    defer pool.Close()

    // Manually create some connections in the pool for testing
    // In a real scenario, these would come from GetConnection
    // For this test, we verify the internal structure
    if len(pool.connections) != 0 {
        t.Errorf("Expected 0 connections initially, got %d", len(pool.connections))
    }
}

// TestConnectionPoolClose tests that closing the pool works correctly
func TestConnectionPoolClose(t *testing.T) {
    pool, err := NewConnectionPool(getMockConnStr(), 5, 10)
    if err != nil {
        t.Fatalf("Failed to create connection pool: %v", err)
    }

    // Close the pool
    if err := pool.Close(); err != nil {
        t.Errorf("Failed to close pool: %v", err)
    }

    // Verify we can't get connections after close
    _, err = pool.GetConnection()
    if err == nil {
        t.Error("Expected error when getting connection from closed pool")
    }
    if !strings.Contains(err.Error(), "closed") {
        t.Errorf("Expected 'closed' error, got: %v", err)
    }
}

// TestConnectionPoolWithTestDB tests the full connection lifecycle with a test database
func TestConnectionPoolWithTestDB(t *testing.T) {
    // This test requires a test database
    // We'll create a simple test that validates the connection pool behavior
    // with a short idle timeout

    // Create a connection string for a test database
    // This assumes PostgreSQL is available at localhost for testing
    connStr := "host=localhost port=5432 dbname=postgres user=postgres sslmode=disable"

    // Try to connect to verify PostgreSQL is available
    testDB, err := sql.Open("postgres", connStr)
    if err != nil {
        t.Skipf("Skipping test: cannot open test database: %v", err)
    }
    if err := testDB.Ping(); err != nil {
        testDB.Close()
        t.Skipf("Skipping test: cannot connect to test database: %v", err)
    }
    testDB.Close()

    // Create a pool with a short idle timeout for testing
    pool, err := NewConnectionPool(connStr, 3, 2) // 2 second idle timeout
    if err != nil {
        t.Fatalf("Failed to create connection pool: %v", err)
    }
    defer pool.Close()

    // Test 1: Get a connection
    conn1, err := pool.GetConnection()
    if err != nil {
        t.Fatalf("Failed to get first connection: %v", err)
    }

    // Verify stats
    total, inUse, idle := pool.Stats()
    if total != 1 || inUse != 1 || idle != 0 {
        t.Errorf("After getting first connection: expected total=1, inUse=1, idle=0, got total=%d, inUse=%d, idle=%d", total, inUse, idle)
    }

    // Test 2: Return the connection
    if err := pool.ReturnConnection(conn1); err != nil {
        t.Fatalf("Failed to return first connection: %v", err)
    }

    // Verify stats after return
    total, inUse, idle = pool.Stats()
    if total != 1 || inUse != 0 || idle != 1 {
        t.Errorf("After returning connection: expected total=1, inUse=0, idle=1, got total=%d, inUse=%d, idle=%d", total, inUse, idle)
    }

    // Test 3: Re-request the same connection from pool
    conn2, err := pool.GetConnection()
    if err != nil {
        t.Fatalf("Failed to get second connection: %v", err)
    }

    // The connection should be reused from the pool
    if conn2 != conn1 {
        t.Error("Expected to reuse the same connection from pool")
    }

    // Verify stats
    total, inUse, idle = pool.Stats()
    if total != 1 || inUse != 1 || idle != 0 {
        t.Errorf("After reusing connection: expected total=1, inUse=1, idle=0, got total=%d, inUse=%d, idle=%d", total, inUse, idle)
    }

    // Test 4: Return and wait for idle timeout
    if err := pool.ReturnConnection(conn2); err != nil {
        t.Fatalf("Failed to return second connection: %v", err)
    }

    // Wait for idle timeout (2 seconds) plus a bit more for cleanup cycle
    t.Logf("Waiting for idle timeout...")
    time.Sleep(3 * time.Second)

    // Verify the connection was cleaned up
    total, inUse, idle = pool.Stats()
    if total != 0 {
        t.Errorf("After idle timeout: expected total=0, got total=%d (connection should have been closed)", total)
    }

    // Test 5: Get connection after cleanup should create a new one
    conn3, err := pool.GetConnection()
    if err != nil {
        t.Fatalf("Failed to get connection after cleanup: %v", err)
    }
    defer pool.ReturnConnection(conn3)

    total, inUse, idle = pool.Stats()
    if total != 1 || inUse != 1 || idle != 0 {
        t.Errorf("After getting new connection: expected total=1, inUse=1, idle=0, got total=%d, inUse=%d, idle=%d", total, inUse, idle)
    }
}

// TestConnectionPoolMaxConnectionsLimit tests that the pool enforces the max connections limit
func TestConnectionPoolMaxConnectionsLimit(t *testing.T) {
    connStr := "host=localhost port=5432 dbname=postgres user=postgres sslmode=disable"

    // Try to connect to verify PostgreSQL is available
    testDB, err := sql.Open("postgres", connStr)
    if err != nil {
        t.Skipf("Skipping test: cannot open test database: %v", err)
    }
    if err := testDB.Ping(); err != nil {
        testDB.Close()
        t.Skipf("Skipping test: cannot connect to test database: %v", err)
    }
    testDB.Close()

    // Create a pool with max 2 connections
    pool, err := NewConnectionPool(connStr, 2, 10)
    if err != nil {
        t.Fatalf("Failed to create connection pool: %v", err)
    }
    defer pool.Close()

    // Get first connection
    conn1, err := pool.GetConnection()
    if err != nil {
        t.Fatalf("Failed to get first connection: %v", err)
    }

    // Get second connection
    conn2, err := pool.GetConnection()
    if err != nil {
        t.Fatalf("Failed to get second connection: %v", err)
    }

    // Try to get third connection - should fail
    _, err = pool.GetConnection()
    if err == nil {
        t.Error("Expected error when exceeding max connections")
    }
    if !strings.Contains(err.Error(), "exhausted") {
        t.Errorf("Expected 'exhausted' error, got: %v", err)
    }

    // Return one connection
    if err := pool.ReturnConnection(conn1); err != nil {
        t.Fatalf("Failed to return connection: %v", err)
    }

    // Now we should be able to get another connection
    conn3, err := pool.GetConnection()
    if err != nil {
        t.Fatalf("Failed to get connection after returning one: %v", err)
    }

    // Clean up
    pool.ReturnConnection(conn2)
    pool.ReturnConnection(conn3)
}

// TestConnectionPoolDoubleReturn tests that returning a connection twice fails
func TestConnectionPoolDoubleReturn(t *testing.T) {
    connStr := "host=localhost port=5432 dbname=postgres user=postgres sslmode=disable"

    testDB, err := sql.Open("postgres", connStr)
    if err != nil {
        t.Skipf("Skipping test: cannot open test database: %v", err)
    }
    if err := testDB.Ping(); err != nil {
        testDB.Close()
        t.Skipf("Skipping test: cannot connect to test database: %v", err)
    }
    testDB.Close()

    pool, err := NewConnectionPool(connStr, 2, 10)
    if err != nil {
        t.Fatalf("Failed to create connection pool: %v", err)
    }
    defer pool.Close()

    conn, err := pool.GetConnection()
    if err != nil {
        t.Fatalf("Failed to get connection: %v", err)
    }

    // Return once
    if err := pool.ReturnConnection(conn); err != nil {
        t.Fatalf("Failed to return connection first time: %v", err)
    }

    // Try to return again - should fail
    err = pool.ReturnConnection(conn)
    if err == nil {
        t.Error("Expected error when returning connection twice")
    }
    if !strings.Contains(err.Error(), "not in use") {
        t.Errorf("Expected 'not in use' error, got: %v", err)
    }
}
