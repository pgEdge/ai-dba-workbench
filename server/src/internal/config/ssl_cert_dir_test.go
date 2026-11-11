/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSSLCertDirSources checks connection_security.ssl_cert_dir from each
// source in priority order: file, then environment, then the flag, and
// that it defaults to empty.
func TestSSLCertDirSources(t *testing.T) {
	t.Setenv("PGEDGE_CONNECTION_SECURITY_SSL_CERT_DIR", "")

	cfg, err := LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ConnectionSecurity.SSLCertDir != "" {
		t.Errorf("default ssl_cert_dir = %q, want empty", cfg.ConnectionSecurity.SSLCertDir)
	}

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "connection_security:\n  ssl_cert_dir: /etc/pgedge/file-certs\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	fileFlags := CLIFlags{ConfigFileSet: true, ConfigFile: configPath}

	cfg, err = LoadConfig(configPath, fileFlags)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.ConnectionSecurity.SSLCertDir; got != "/etc/pgedge/file-certs" {
		t.Errorf("file ssl_cert_dir = %q", got)
	}

	t.Setenv("PGEDGE_CONNECTION_SECURITY_SSL_CERT_DIR", "/etc/pgedge/env-certs")
	cfg, err = LoadConfig(configPath, fileFlags)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.ConnectionSecurity.SSLCertDir; got != "/etc/pgedge/env-certs" {
		t.Errorf("env ssl_cert_dir = %q", got)
	}

	fileFlags.ConnectionSSLCertDirSet = true
	fileFlags.ConnectionSSLCertDir = "/etc/pgedge/flag-certs"
	cfg, err = LoadConfig(configPath, fileFlags)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.ConnectionSecurity.SSLCertDir; got != "/etc/pgedge/flag-certs" {
		t.Errorf("flag ssl_cert_dir = %q", got)
	}
}

// TestSSLCertDirMustBeAbsolute checks that a relative directory, which
// would depend on the server's working directory, is refused at load.
func TestSSLCertDirMustBeAbsolute(t *testing.T) {
	t.Setenv("PGEDGE_CONNECTION_SECURITY_SSL_CERT_DIR", "")
	_, err := LoadConfig("", CLIFlags{
		ConnectionSSLCertDirSet: true,
		ConnectionSSLCertDir:    "certs",
	})
	if err == nil || !strings.Contains(err.Error(), "ssl_cert_dir must be an absolute path") {
		t.Fatalf("error = %v, want the absolute-path refusal", err)
	}
}
