/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package database provides database connection management for the MCP server
package database

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/pgEdge/ai-workbench/server/src/config"
)

// GetConnectionString builds a PostgreSQL connection string from config
func GetConnectionString(cfg *config.Config) string {
    connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s",
        cfg.GetPgHost(), cfg.GetPgPort(), cfg.GetPgDatabase(), cfg.GetPgUsername())

    if cfg.GetPgPassword() != "" {
        connStr += fmt.Sprintf(" password=%s", cfg.GetPgPassword())
    }

    if cfg.GetPgHostAddr() != "" {
        connStr += fmt.Sprintf(" hostaddr=%s", cfg.GetPgHostAddr())
    }

    if cfg.GetPgSSLMode() != "" {
        connStr += fmt.Sprintf(" sslmode=%s", cfg.GetPgSSLMode())
    }

    if cfg.GetPgSSLCert() != "" {
        connStr += fmt.Sprintf(" sslcert=%s", cfg.GetPgSSLCert())
    }

    if cfg.GetPgSSLKey() != "" {
        connStr += fmt.Sprintf(" sslkey=%s", cfg.GetPgSSLKey())
    }

    if cfg.GetPgSSLRootCert() != "" {
        connStr += fmt.Sprintf(" sslrootcert=%s", cfg.GetPgSSLRootCert())
    }

    connStr += " application_name='pgEdge AI Workbench - MCP Server'"

    return connStr
}

// Connect establishes a connection pool to the datastore database
func Connect(cfg *config.Config) (*pgxpool.Pool, error) {
    ctx := context.Background()
    connStr := GetConnectionString(cfg)

    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        return nil, fmt.Errorf("failed to create connection pool: %w", err)
    }

    // Test the connection
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }

    return pool, nil
}
