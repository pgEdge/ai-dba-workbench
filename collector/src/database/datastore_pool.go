/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/pgedge/ai-workbench/collector/src/logger"
	"sync"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver for database/sql
)

// PooledConnection represents a connection in the pool with metadata
type PooledConnection struct {
	conn       *sql.DB
	returnedAt time.Time
	inUse      bool
	createdAt  time.Time
}

// ConnectionPool manages a pool of database connections
type ConnectionPool struct {
	connStr        string
	maxConnections int
	maxIdleSeconds int
	connections    []*PooledConnection
	mu             sync.Mutex
	shutdown       chan struct{}
	cleanupWg      sync.WaitGroup
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(connStr string, maxConnections, maxIdleSeconds int) (*ConnectionPool, error) {
	if maxConnections <= 0 {
		return nil, fmt.Errorf("maxConnections must be greater than 0")
	}

	pool := &ConnectionPool{
		connStr:        connStr,
		maxConnections: maxConnections,
		maxIdleSeconds: maxIdleSeconds,
		connections:    make([]*PooledConnection, 0, maxConnections),
		shutdown:       make(chan struct{}),
	}

	// Start cleanup goroutine
	pool.cleanupWg.Add(1)
	go pool.cleanupIdleConnections()

	return pool, nil
}

// GetConnection retrieves a connection from the pool or creates a new one
// If the pool is exhausted, it will wait for a connection to become available
// until the context deadline is reached
func (p *ConnectionPool) GetConnection(ctx context.Context) (*sql.DB, error) {
	// Check if context is already canceled
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Check if pool is shutting down (without holding lock)
	select {
	case <-p.shutdown:
		return nil, fmt.Errorf("connection pool is closed")
	default:
	}

	// Retry loop - keep trying until we get a connection or context times out
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		// Try to find an available connection (with minimal lock time)
		p.mu.Lock()
		var candidateConn *PooledConnection
		for _, pc := range p.connections {
			if !pc.inUse {
				candidateConn = pc
				candidateConn.inUse = true // Mark as in-use before releasing lock
				break
			}
		}

		canCreateNew := len(p.connections) < p.maxConnections
		p.mu.Unlock()

		// If we found a candidate connection, test it (without holding lock)
		if candidateConn != nil {
			if err := candidateConn.conn.PingContext(ctx); err == nil {
				// Connection is good, return it
				return candidateConn.conn, nil
			}

			// Connection is dead, close it and remove from pool
			if cerr := candidateConn.conn.Close(); cerr != nil {
				logger.Errorf("Error closing dead connection: %v", cerr)
			}

			// Remove the dead connection from the pool
			p.mu.Lock()
			p.removeConnection(candidateConn)
			p.mu.Unlock()

			// Continue to try creating a new connection or finding another available one
			// canCreateNew will be re-evaluated at the top of the loop
			continue
		}

		// No available connection - try to create a new one if we can
		if canCreateNew {
			// Create a new connection (without holding lock)
			conn, err := sql.Open("postgres", p.connStr)
			if err != nil {
				return nil, fmt.Errorf("failed to create connection: %w", err)
			}

			// Test the connection with timeout (without holding lock)
			if err := conn.PingContext(ctx); err != nil {
				if cerr := conn.Close(); cerr != nil {
					logger.Errorf("Error closing failed connection: %v", cerr)
				}
				return nil, fmt.Errorf("failed to ping new connection: %w", err)
			}

			// Add to pool (briefly acquire lock)
			pc := &PooledConnection{
				conn:       conn,
				inUse:      true,
				createdAt:  time.Now(),
				returnedAt: time.Time{},
			}

			p.mu.Lock()
			// Check again if we're still under the limit (race condition)
			if len(p.connections) >= p.maxConnections {
				p.mu.Unlock()
				// Close the connection we just created since pool is now full
				if cerr := conn.Close(); cerr != nil {
					logger.Errorf("Error closing excess connection: %v", cerr)
				}
				// Continue waiting for an available connection instead of returning error
				continue
			}
			p.connections = append(p.connections, pc)
			p.mu.Unlock()

			return conn, nil
		}

		// Pool is exhausted and we can't create new connections
		// Wait for a connection to be returned or context to be canceled
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connection pool exhausted (max: %d), timed out waiting for available connection: %w",
				p.maxConnections, ctx.Err())
		case <-p.shutdown:
			return nil, fmt.Errorf("connection pool is closed")
		case <-ticker.C:
			// Continue loop to try again
		}
	}
}

// ReturnConnection returns a connection to the pool
func (p *ConnectionPool) ReturnConnection(conn *sql.DB) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find the connection in the pool
	for _, pc := range p.connections {
		if pc.conn == conn {
			if !pc.inUse {
				return fmt.Errorf("connection was not in use")
			}
			pc.inUse = false
			pc.returnedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("connection not found in pool")
}

// removeConnection removes a connection from the pool (must be called with lock held)
func (p *ConnectionPool) removeConnection(target *PooledConnection) {
	for i, pc := range p.connections {
		if pc == target {
			p.connections = append(p.connections[:i], p.connections[i+1:]...)
			return
		}
	}
}

// cleanupIdleConnections periodically closes idle connections
func (p *ConnectionPool) cleanupIdleConnections() {
	defer p.cleanupWg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.shutdown:
			return
		case <-ticker.C:
			p.performCleanup()
		}
	}
}

// performCleanup closes connections that have been idle too long
func (p *ConnectionPool) performCleanup() {
	if p.maxIdleSeconds <= 0 {
		return // Cleanup disabled
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	toRemove := make([]*PooledConnection, 0)

	for _, pc := range p.connections {
		if !pc.inUse {
			var idleTime time.Duration
			// If connection has been returned, use returnedAt, otherwise use createdAt
			// This ensures we clean up connections that were created but never returned
			if !pc.returnedAt.IsZero() {
				idleTime = now.Sub(pc.returnedAt)
			} else {
				// Connection created but never returned - measure idle time from creation
				idleTime = now.Sub(pc.createdAt)
			}

			if idleTime.Seconds() >= float64(p.maxIdleSeconds) {
				toRemove = append(toRemove, pc)
			}
		}
	}

	// Close and remove idle connections
	for _, pc := range toRemove {
		if err := pc.conn.Close(); err != nil {
			logger.Errorf("Error closing idle connection: %v", err)
		}
		p.removeConnection(pc)
	}

	if len(toRemove) > 0 {
		logger.Infof("Closed %d idle connections", len(toRemove))
	}
}

// Close closes all connections in the pool
func (p *ConnectionPool) Close() error {
	// Signal shutdown
	close(p.shutdown)

	// Wait for cleanup goroutine to finish
	p.cleanupWg.Wait()

	// Close all connections
	p.mu.Lock()
	defer p.mu.Unlock()

	connCount := len(p.connections)
	if connCount > 0 {
		logger.Infof("Closing %d connection(s) in pool...", connCount)
	}

	var lastErr error
	closedCount := 0
	for _, pc := range p.connections {
		if err := pc.conn.Close(); err != nil {
			lastErr = err
			logger.Errorf("Error closing connection: %v", err)
		} else {
			closedCount++
		}
	}

	if connCount > 0 {
		logger.Infof("Closed %d of %d connection(s) in pool", closedCount, connCount)
	}

	p.connections = nil
	return lastErr
}

// Stats returns statistics about the connection pool
func (p *ConnectionPool) Stats() (total, inUse, idle int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	total = len(p.connections)
	for _, pc := range p.connections {
		if pc.inUse {
			inUse++
		} else {
			idle++
		}
	}
	return
}
