/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"fmt"
	"net"
	"time"
)

// healthCheckTimeout bounds the -health-check connection attempt, so that
// a container health check fails rather than hangs when nothing answers.
const healthCheckTimeout = 5 * time.Second

// runHealthCheck reports whether a TCP listener accepts connections at
// address (host:port). A container health check runs the server binary
// itself with -health-check, so the probe does not depend on which tools
// the base image ships. It checks only that the port accepts a TCP
// connection, and so works whether the listener speaks plain HTTP or TLS.
func runHealthCheck(address string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return fmt.Errorf("health check of %s failed: %w", address, err)
	}
	return conn.Close()
}
