/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package testutil provides utilities for integration testing
package testutil

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

// TestDatabase manages a test database lifecycle
type TestDatabase struct {
    Name          string
    ConnString    string
    AdminConnStr  string
    Pool          *pgxpool.Pool
    keepDB        bool
}

// NewTestDatabase creates a new test database
func NewTestDatabase() (*TestDatabase, error) {
    // Generate unique database name with timestamp
    dbName := fmt.Sprintf("ai_workbench_test_%d", time.Now().Unix())

    // Get admin connection string from environment or use default
    adminConnStr := os.Getenv("TEST_DB_URL")
    if adminConnStr == "" {
        adminConnStr = "postgres://postgres@localhost:5432/postgres"
    }

    // Check if we should keep the database after tests
    keepDB := os.Getenv("TEST_DB_KEEP") == "1"

    ctx := context.Background()

    // Connect to PostgreSQL to create test database
    adminPool, err := pgxpool.New(ctx, adminConnStr)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
    }
    defer adminPool.Close()

    // Create test database
    _, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
    if err != nil {
        return nil, fmt.Errorf("failed to create test database: %w", err)
    }

    // Build connection string for test database
    testConnStr := fmt.Sprintf("%s/%s", adminConnStr[:len(adminConnStr)-9], dbName)

    // Connect to test database
    testPool, err := pgxpool.New(ctx, testConnStr)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to test database: %w", err)
    }

    return &TestDatabase{
        Name:         dbName,
        ConnString:   testConnStr,
        AdminConnStr: adminConnStr,
        Pool:         testPool,
        keepDB:       keepDB,
    }, nil
}

// Close closes the test database and optionally drops it
func (td *TestDatabase) Close() error {
    if td.Pool != nil {
        td.Pool.Close()
    }

    if td.keepDB {
        fmt.Printf("Keeping test database: %s\n", td.Name)
        return nil
    }

    // Connect to admin database to drop test database
    ctx := context.Background()
    adminPool, err := pgxpool.New(ctx, td.AdminConnStr)
    if err != nil {
        return fmt.Errorf("failed to connect for cleanup: %w", err)
    }
    defer adminPool.Close()

    // Terminate connections to test database
    _, err = adminPool.Exec(ctx, fmt.Sprintf(
        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'",
        td.Name))
    if err != nil {
        return fmt.Errorf("failed to terminate connections: %w", err)
    }

    // Drop test database
    _, err = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", td.Name))
    if err != nil {
        return fmt.Errorf("failed to drop test database: %w", err)
    }

    return nil
}

// GetConnString returns connection string for test database
func (td *TestDatabase) GetConnString() string {
    return td.ConnString
}

// GetPool returns the connection pool for test database
func (td *TestDatabase) GetPool() *pgxpool.Pool {
    return td.Pool
}
