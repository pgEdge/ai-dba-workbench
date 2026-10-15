/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSelfSignedCert writes a throwaway self-signed certificate and
// key for localhost into dir and returns their paths.
func writeSelfSignedCert(t *testing.T, dir string) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate a key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create a certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal the key: %v", err)
	}

	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatalf("failed to write the certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(
		&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatalf("failed to write the key: %v", err)
	}
	return certFile, keyFile
}

// TestRunHTTPReportsStartUpErrors covers the ways RunHTTP fails before
// or as it starts listening, each of which returns an error rather
// than serving.
func TestRunHTTPReportsStartUpErrors(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t, t.TempDir())
	setupErr := errors.New("handler setup failed")

	tests := []struct {
		name   string
		config *HTTPConfig
		want   string
	}{
		{
			name: "handler setup fails",
			config: &HTTPConfig{
				Addr: "127.0.0.1:0",
				SetupHandlers: func(*http.ServeMux) error {
					return setupErr
				},
			},
			want: "failed to setup custom handlers",
		},
		{
			name: "CORS origin with an unusable address",
			config: &HTTPConfig{
				Addr:          "127.0.0.1:-1",
				CORSOrigin:    "https://dba.example.com",
				SetupHandlers: func(*http.ServeMux) error { return nil },
			},
			want: "invalid port",
		},
		{
			name: "TLS certificate missing",
			config: &HTTPConfig{
				Addr:      "127.0.0.1:0",
				TLSEnable: true,
				CertFile:  filepath.Join(t.TempDir(), "absent.crt"),
				KeyFile:   filepath.Join(t.TempDir(), "absent.key"),
			},
			want: "failed to load TLS config",
		},
		{
			name: "TLS with an unusable address",
			config: &HTTPConfig{
				Addr:      "127.0.0.1:-1",
				TLSEnable: true,
				CertFile:  certFile,
				KeyFile:   keyFile,
			},
			want: "invalid port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&mockToolProvider{})
			err := server.RunHTTP(tt.config)
			if err == nil {
				t.Fatal("RunHTTP returned nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("RunHTTP error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}
