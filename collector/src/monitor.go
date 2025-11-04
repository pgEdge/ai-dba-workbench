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

// Monitor manages the monitoring of PostgreSQL servers
type Monitor struct {
    datastore   *Datastore
    config      *Config
    connections map[int]*MonitoredConn
    probes      map[int]*Probe
    mu          sync.RWMutex
}

// MonitoredConn represents an active connection to a monitored server
type MonitoredConn struct {
    connection MonitoredConnection
    db         *sql.DB
}

// Probe represents a monitoring probe configuration
type Probe struct {
    ID                 int
    Name               string
    Description        string
    SQLQuery           string
    CollectionInterval int
    RetentionDays      int
    Enabled            bool
}

// initMonitor initializes the monitoring system
func initMonitor(ds *Datastore, config *Config) (*Monitor, error) {
    m := &Monitor{
        datastore:   ds,
        config:      config,
        connections: make(map[int]*MonitoredConn),
        probes:      make(map[int]*Probe),
    }

    // Load monitored connections
    if err := m.loadConnections(); err != nil {
        return nil, fmt.Errorf("failed to load connections: %w", err)
    }

    // Load probes
    if err := m.loadProbes(); err != nil {
        return nil, fmt.Errorf("failed to load probes: %w", err)
    }

    return m, nil
}

// loadConnections loads all monitored connections from the datastore
func (m *Monitor) loadConnections() error {
    connections, err := m.datastore.GetMonitoredConnections()
    if err != nil {
        return err
    }

    m.mu.Lock()
    defer m.mu.Unlock()

    for _, conn := range connections {
        monConn := &MonitoredConn{
            connection: conn,
        }

        // Establish connection to monitored server
        if err := monConn.connect(m.config); err != nil {
            log.Printf("Warning: failed to connect to %s: %v", conn.Name, err)
            continue
        }

        m.connections[conn.ID] = monConn
        log.Printf("Connected to monitored server: %s", conn.Name)
    }

    return nil
}

// loadProbes loads all enabled probes from the datastore
func (m *Monitor) loadProbes() error {
    conn, err := m.datastore.GetConnection()
    if err != nil {
        return fmt.Errorf("failed to get connection: %w", err)
    }
    defer m.datastore.ReturnConnection(conn)

    rows, err := conn.Query(`
        SELECT id, name, description, sql_query, collection_interval, retention_days, enabled
        FROM probes
        WHERE enabled = TRUE
    `)
    if err != nil {
        return fmt.Errorf("failed to query probes: %w", err)
    }
    defer rows.Close()

    m.mu.Lock()
    defer m.mu.Unlock()

    for rows.Next() {
        probe := &Probe{}
        if err := rows.Scan(
            &probe.ID, &probe.Name, &probe.Description, &probe.SQLQuery,
            &probe.CollectionInterval, &probe.RetentionDays, &probe.Enabled,
        ); err != nil {
            return fmt.Errorf("failed to scan probe row: %w", err)
        }

        m.probes[probe.ID] = probe
        log.Printf("Loaded probe: %s (interval: %ds)", probe.Name, probe.CollectionInterval)
    }

    return rows.Err()
}

// Start starts the monitoring threads
func (m *Monitor) Start(shutdown <-chan struct{}) {
    log.Println("Monitor starting...")

    // Create a wait group for all probe threads
    var wg sync.WaitGroup

    m.mu.RLock()
    probes := make([]*Probe, 0, len(m.probes))
    for _, probe := range m.probes {
        probes = append(probes, probe)
    }
    m.mu.RUnlock()

    // Start a goroutine for each probe
    for _, probe := range probes {
        wg.Add(1)
        go func(p *Probe) {
            defer wg.Done()
            m.runProbe(p, shutdown)
        }(probe)
    }

    // Wait for all probe threads to finish
    wg.Wait()
    log.Println("Monitor stopped")
}

// runProbe runs a single probe on all monitored connections
func (m *Monitor) runProbe(probe *Probe, shutdown <-chan struct{}) {
    ticker := time.NewTicker(time.Duration(probe.CollectionInterval) * time.Second)
    defer ticker.Stop()

    log.Printf("Starting probe thread: %s", probe.Name)

    for {
        select {
        case <-shutdown:
            log.Printf("Probe thread stopping: %s", probe.Name)
            return
        case <-ticker.C:
            m.executeProbe(probe)
        }
    }
}

// executeProbe executes a probe against all monitored connections
func (m *Monitor) executeProbe(probe *Probe) {
    m.mu.RLock()
    connections := make([]*MonitoredConn, 0, len(m.connections))
    for _, conn := range m.connections {
        connections = append(connections, conn)
    }
    m.mu.RUnlock()

    for _, conn := range connections {
        if err := m.collectMetrics(conn, probe); err != nil {
            log.Printf("Error collecting metrics for %s with probe %s: %v",
                conn.connection.Name, probe.Name, err)
        }
    }
}

// collectMetrics executes a probe query and stores the results
func (m *Monitor) collectMetrics(conn *MonitoredConn, probe *Probe) error {
    // Execute the probe query
    rows, err := conn.db.Query(probe.SQLQuery)
    if err != nil {
        return fmt.Errorf("failed to execute probe query: %w", err)
    }
    defer rows.Close()

    // For now, just log that we executed the probe
    // In a full implementation, we would store the results in partitioned tables
    log.Printf("Executed probe %s on %s", probe.Name, conn.connection.Name)

    return nil
}

// connect establishes a connection to a monitored PostgreSQL server
func (mc *MonitoredConn) connect(config *Config) error {
    connStr := mc.buildConnectionString(config)

    db, err := sql.Open("postgres", connStr)
    if err != nil {
        return fmt.Errorf("failed to open connection: %w", err)
    }

    // Set connection pool settings
    db.SetMaxOpenConns(5)
    db.SetMaxIdleConns(2)
    db.SetConnMaxLifetime(5 * time.Minute)

    // Test the connection
    if err := db.Ping(); err != nil {
        db.Close()
        return fmt.Errorf("failed to ping server: %w", err)
    }

    mc.db = db
    return nil
}

// buildConnectionString builds a connection string for a monitored connection
func (mc *MonitoredConn) buildConnectionString(config *Config) string {
    conn := mc.connection

    params := make(map[string]string)
    params["dbname"] = conn.DatabaseName
    params["user"] = conn.Username

    if conn.HostAddr.Valid && conn.HostAddr.String != "" {
        params["hostaddr"] = conn.HostAddr.String
    } else {
        params["host"] = conn.Host
    }

    if conn.Port != 0 {
        params["port"] = fmt.Sprintf("%d", conn.Port)
    }

    // Decrypt password if present (placeholder - would need actual decryption)
    if conn.PasswordEncrypted.Valid && conn.PasswordEncrypted.String != "" {
        // TODO: Implement actual password decryption using server_secret
        params["password"] = conn.PasswordEncrypted.String
    }

    // SSL parameters
    if conn.SSLMode.Valid && conn.SSLMode.String != "" {
        params["sslmode"] = conn.SSLMode.String
    }
    if conn.SSLCert.Valid && conn.SSLCert.String != "" {
        params["sslcert"] = conn.SSLCert.String
    }
    if conn.SSLKey.Valid && conn.SSLKey.String != "" {
        params["sslkey"] = conn.SSLKey.String
    }
    if conn.SSLRootCert.Valid && conn.SSLRootCert.String != "" {
        params["sslrootcert"] = conn.SSLRootCert.String
    }

    // Build the connection string
    var connStr string
    for key, value := range params {
        if connStr != "" {
            connStr += " "
        }
        connStr += fmt.Sprintf("%s='%s'", key, value)
    }

    return connStr
}
