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
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq"
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
func (p *ConnectionPool) GetConnection() (*sql.DB, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if pool is shutting down
	select {
	case <-p.shutdown:
		return nil, fmt.Errorf("connection pool is closed")
	default:
	}

	// First, try to find an available connection in the pool
	for _, pc := range p.connections {
		if !pc.inUse {
			// Test if connection is still alive
			if err := pc.conn.Ping(); err == nil {
				pc.inUse = true
				return pc.conn, nil
			}
			// Connection is dead, close it and remove from pool
			if cerr := pc.conn.Close(); cerr != nil {
				log.Printf("Error closing dead connection: %v", cerr)
			}
			p.removeConnection(pc)
		}
	}

	// No available connection, check if we can create a new one
	if len(p.connections) >= p.maxConnections {
		return nil, fmt.Errorf("connection pool exhausted (max: %d)", p.maxConnections)
	}

	// Create a new connection
	conn, err := sql.Open("postgres", p.connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	// Test the connection
	if err := conn.Ping(); err != nil {
		if cerr := conn.Close(); cerr != nil {
			log.Printf("Error closing failed connection: %v", cerr)
		}
		return nil, fmt.Errorf("failed to ping new connection: %w", err)
	}

	// Add to pool
	pc := &PooledConnection{
		conn:       conn,
		inUse:      true,
		createdAt:  time.Now(),
		returnedAt: time.Time{},
	}
	p.connections = append(p.connections, pc)

	return conn, nil
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
		if !pc.inUse && !pc.returnedAt.IsZero() {
			idleTime := now.Sub(pc.returnedAt)
			if idleTime.Seconds() >= float64(p.maxIdleSeconds) {
				toRemove = append(toRemove, pc)
			}
		}
	}

	// Close and remove idle connections
	for _, pc := range toRemove {
		if err := pc.conn.Close(); err != nil {
			log.Printf("Error closing idle connection: %v", err)
		}
		p.removeConnection(pc)
	}

	if len(toRemove) > 0 {
		log.Printf("Closed %d idle connections", len(toRemove))
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

	var lastErr error
	for _, pc := range p.connections {
		if err := pc.conn.Close(); err != nil {
			lastErr = err
			log.Printf("Error closing connection: %v", err)
		}
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
