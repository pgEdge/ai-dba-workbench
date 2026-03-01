/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package database provides datastore access for the alerter.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/connstring"
)

// Datastore provides access to the PostgreSQL datastore
type Datastore struct {
	pool   *pgxpool.Pool
	config *config.Config
}

// NewDatastore creates a new datastore connection
func NewDatastore(cfg *config.Config) (*Datastore, error) {
	connString := connstring.BuildFromConfig(cfg.Datastore, "pgEdge AI DBA Workbench - Alerter")

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure pool settings
	maxConns := cfg.Pool.MaxConnections
	if maxConns > 0 && maxConns <= 32767 {
		poolConfig.MaxConns = int32(maxConns)
	}
	poolConfig.MaxConnIdleTime = time.Duration(cfg.Pool.MaxIdleSeconds) * time.Second

	// Create connection pool
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to connect to datastore: %w", err)
	}

	return &Datastore{
		pool:   pool,
		config: cfg,
	}, nil
}

// Close closes the datastore connection pool
func (d *Datastore) Close() {
	if d.pool != nil {
		d.pool.Close()
	}
}

// Pool returns the underlying connection pool
func (d *Datastore) Pool() *pgxpool.Pool {
	return d.pool
}
