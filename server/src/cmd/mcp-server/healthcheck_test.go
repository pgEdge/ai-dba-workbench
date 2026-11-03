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
	"net"
	"strings"
	"testing"
	"time"
)

func TestRunHealthCheckSucceedsWhenListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	if err := runHealthCheck(ln.Addr().String(), time.Second); err != nil {
		t.Fatalf("expected the health check to pass, got %v", err)
	}
}

func TestRunHealthCheckFailsWhenNothingListens(t *testing.T) {
	// Take a free port, then release it so that nothing listens there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	address := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to close listener: %v", err)
	}

	err = runHealthCheck(address, time.Second)
	if err == nil {
		t.Fatal("expected the health check to fail")
	}
	if !strings.Contains(err.Error(), address) {
		t.Errorf("expected the error to name %s, got %v", address, err)
	}
}
