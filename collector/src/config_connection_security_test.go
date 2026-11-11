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
	"os"
	"path/filepath"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/flagutil"
)

// TestConfigConnectionSecurity_SSLCertDir covers the
// connection_security.ssl_cert_dir option: it loads from YAML, the
// -connection-ssl-cert-dir flag overrides it only when passed, and
// Validate refuses a relative directory.
func TestConfigConnectionSecurity_SSLCertDir(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "collector.yaml")
	content := `datastore:
  host: localhost
  database: ai_workbench
  username: postgres
  port: 5432
connection_security:
  ssl_cert_dir: /etc/ai-workbench/certs
`
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	config := NewConfig()
	if err := config.LoadFromFile(configPath); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if got := config.ConnectionSecurity.SSLCertDir; got != "/etc/ai-workbench/certs" {
		t.Fatalf("SSLCertDir = %q, want /etc/ai-workbench/certs", got)
	}

	orig := *connectionSSLCertDir
	t.Cleanup(func() { *connectionSSLCertDir = orig })
	*connectionSSLCertDir = "/srv/certs"

	config.ApplyFlags(flagutil.Set{})
	if got := config.ConnectionSecurity.SSLCertDir; got != "/etc/ai-workbench/certs" {
		t.Errorf("unpassed flag changed SSLCertDir to %q", got)
	}
	config.ApplyFlags(flagutil.Set{flagConnectionSSLCertDir: true})
	if got := config.ConnectionSecurity.SSLCertDir; got != "/srv/certs" {
		t.Errorf("SSLCertDir = %q, want /srv/certs from the flag", got)
	}

	if err := config.Validate(); err != nil {
		t.Errorf("Validate() with an absolute directory = %v, want nil", err)
	}
	config.ConnectionSecurity.SSLCertDir = "certs"
	if err := config.Validate(); err == nil {
		t.Error("Validate() with a relative directory = nil, want an error")
	}
	config.ConnectionSecurity.SSLCertDir = ""
	if err := config.Validate(); err != nil {
		t.Errorf("Validate() with no directory = %v, want nil", err)
	}
}
