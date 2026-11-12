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
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// TestInitDatastoreWiresConnectionSecurity checks that initDatastore
// hands connection_security to the datastore: ssl_cert_dir for the
// connect-time check on stored SSL file paths, and the dial policy that
// every monitored connection's resolved addresses are held to. The
// always-blocked floor, which is what the datastore falls back to with
// no policy set, permits 10.0.0.5, so the cases that refuse it fail if
// the SetDialPolicy call is removed, and every case fails if the
// SetSSLCertDir call is.
func TestInitDatastoreWiresConnectionSecurity(t *testing.T) {
	db := testDatabaseConfig(t)

	// lookupAs returns the addresses a monitored connection to host
	// would be allowed to dial, when host resolves to addr.
	lookupAs := func(t *testing.T, s *Server, host, addr string) ([]string, error) {
		t.Helper()
		cfg := &pgconn.Config{LookupFunc: func(context.Context, string) ([]string, error) {
			return []string{addr}, nil
		}}
		s.datastore.GuardMonitoredConnConfig(cfg)
		return cfg.LookupFunc(context.Background(), host)
	}

	tests := []struct {
		name    string
		cs      config.ConnectionSecurityConfig
		host    string
		addr    string
		refused bool
	}{
		// 10.0.0.5 passes the floor, so only the configured policy
		// (allow_internal_networks off) refuses it.
		{"internal ranges refused by default", config.ConnectionSecurityConfig{}, "db.example.com", "10.0.0.5", true},
		{"allow_internal_networks reaches the policy",
			config.ConnectionSecurityConfig{AllowInternalNetworks: true}, "db.example.com", "10.0.0.5", false},
		{"blocked_hosts reaches the policy",
			config.ConnectionSecurityConfig{AllowInternalNetworks: true, BlockedHosts: []string{"db.example.com"}},
			"db.example.com", "10.0.0.5", true},
		{"allowed_hosts reaches the policy",
			config.ConnectionSecurityConfig{AllowedHosts: []string{"db.example.com"}}, "db.example.com", "10.0.0.5", false},
		// The floor still holds under the most permissive settings.
		{"the floor still applies",
			config.ConnectionSecurityConfig{AllowInternalNetworks: true, AllowedHosts: []string{"db.example.com"}},
			"db.example.com", "169.254.169.254", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := tt.cs
			cs.SSLCertDir = "/etc/pgedge/test-certs"
			s := &Server{cfg: &config.Config{Database: db, ConnectionSecurity: cs}}
			captureStderr(t, func() {
				if err := s.initDatastore("a-server-secret"); err != nil {
					t.Fatalf("initDatastore: %v", err)
				}
			})
			t.Cleanup(s.datastore.Close)

			if got := s.datastore.SSLCertDir(); got != cs.SSLCertDir {
				t.Errorf("SSLCertDir() = %q, want %q", got, cs.SSLCertDir)
			}
			addrs, err := lookupAs(t, s, tt.host, tt.addr)
			if tt.refused {
				if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
					t.Errorf("lookup = %v, %v; want ErrNoPermittedAddress", addrs, err)
				}
				return
			}
			if err != nil || len(addrs) != 1 || addrs[0] != tt.addr {
				t.Errorf("lookup = %v, %v; want [%s]", addrs, err, tt.addr)
			}
		})
	}
}
