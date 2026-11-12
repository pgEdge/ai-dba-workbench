/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// TestHostValidator_CheckDialAddr checks that the dial-time policy
// applies the save-time rules to each address on its own.
func TestHostValidator_CheckDialAddr(t *testing.T) {
	tests := []struct {
		name          string
		allowInternal bool
		allowed       []string
		blocked       []string
		host          string
		ip            string
		wantErr       bool
	}{
		{name: "public address", host: "db.example.com", ip: "8.8.8.8"},
		{name: "rebound to metadata", host: "db.example.com", ip: "169.254.169.254", wantErr: true},
		{name: "rebound to loopback", host: "db.example.com", ip: "127.0.0.1", wantErr: true},
		{name: "rebound to private", host: "db.example.com", ip: "10.1.2.3", wantErr: true},
		{name: "unspecified", host: "::", ip: "::", wantErr: true},
		{name: "internal allowed", allowInternal: true, host: "db.example.com", ip: "10.1.2.3"},
		// The always-blocked floor holds whatever the configuration.
		{name: "link-local with internal allowed", allowInternal: true, host: "db.example.com", ip: "169.254.169.254", wantErr: true},
		{name: "allow-listed name, metadata address", allowed: []string{"db.example.com"}, host: "db.example.com", ip: "169.254.169.254", wantErr: true},
		{name: "allow-listed range covering link-local", allowed: []string{"169.254.0.0/16"}, host: "db.example.com", ip: "169.254.1.1", wantErr: true},
		{name: "allow-listed name, unspecified address", allowed: []string{"db.example.com"}, host: "db.example.com", ip: "0.0.0.0", wantErr: true},
		{name: "allow-listed range", allowed: []string{"10.0.0.0/8"}, host: "db.example.com", ip: "10.1.2.3"},
		{name: "allow-listed range does not cover another", allowed: []string{"10.0.0.0/8"}, host: "db.example.com", ip: "192.168.0.1", wantErr: true},
		{name: "allow-listed name, any address", allowed: []string{"DB.example.com"}, host: "db.example.com.", ip: "127.0.0.1"},
		{name: "block-listed name", allowInternal: true, blocked: []string{"db.example.com"}, host: "DB.Example.Com", ip: "8.8.8.8", wantErr: true},
		{name: "block-listed range", allowInternal: true, blocked: []string{"8.8.8.0/24"}, host: "db.example.com", ip: "8.8.8.8", wantErr: true},
		{name: "block-list beats allow-list", allowed: []string{"10.0.0.0/8"}, blocked: []string{"10.1.0.0/16"}, host: "h", ip: "10.1.2.3", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewHostValidator(tt.allowInternal, tt.allowed, tt.blocked)
			err := v.CheckDialAddr(tt.host, net.ParseIP(tt.ip))
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckDialAddr(%q, %s) = %v, wantErr %v", tt.host, tt.ip, err, tt.wantErr)
			}
			if err != nil && (strings.Contains(err.Error(), tt.ip) || strings.Contains(strings.ToLower(err.Error()), "example")) {
				t.Errorf("error %q names the host or address", err)
			}
		})
	}
}

// TestHostValidator_ValidateHostAllowListedResolution keeps the
// save-time rule that one allow-listed address among a name's results
// permits the name.
func TestHostValidator_ValidateHostAllowListedResolution(t *testing.T) {
	v := NewHostValidator(false, []string{"127.0.0.0/8", "::1/128"}, nil)
	if err := v.ValidateHost("localhost"); err != nil {
		t.Errorf("ValidateHost(localhost) = %v, want nil", err)
	}
	v = NewHostValidator(false, nil, []string{"127.0.0.0/8", "::1/128"})
	if err := v.ValidateHost("localhost"); err == nil {
		t.Error("ValidateHost(localhost) with loopback blocked = nil, want an error")
	}
	v = NewHostValidator(false, nil, nil)
	if err := v.ValidateHost("localhost"); err == nil {
		t.Error("ValidateHost(localhost) = nil, want an internal-address error")
	}
}

// TestOpenQueryPool_DialGuard checks that openQueryPool applies the
// guard it is given, and the always-blocked floor when given none.
func TestOpenQueryPool_DialGuard(t *testing.T) {
	ctx := context.Background()

	pool, err := openQueryPool(ctx,
		"postgres://u@169.254.169.254:5432/x?sslmode=disable&connect_timeout=1", "test", nil)
	if err != nil {
		t.Fatalf("openQueryPool: %v", err)
	}
	err = pool.Ping(ctx)
	pool.Close()
	if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Errorf("nil guard: ping err = %v, want ErrNoPermittedAddress", err)
	}

	strict := NewHostValidator(false, nil, nil)
	guard := func(cfg *pgconn.Config) { database.GuardConnConfig(cfg, strict.CheckDialAddr) }
	pool, err = openQueryPool(ctx,
		"postgres://u@127.0.0.1:1/x?sslmode=disable&connect_timeout=1", "test", guard)
	if err != nil {
		t.Fatalf("openQueryPool: %v", err)
	}
	err = pool.Ping(ctx)
	pool.Close()
	if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Errorf("strict guard: ping err = %v, want ErrNoPermittedAddress", err)
	}

	_, err = openQueryPool(ctx, "postgres://u:s3cret@h:notaport/x", "test", guard)
	if !errors.Is(err, database.ErrInvalidConnectionString) {
		t.Errorf("parse failure: err = %v, want ErrInvalidConnectionString", err)
	}
	if err != nil && strings.Contains(err.Error(), "s3cret") {
		t.Errorf("parse error %q carries the password", err)
	}
}
