/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// tlsTestHost is the only name the test server certificate carries. It
// is never resolved: the connection reaches the server through hostaddr.
const tlsTestHost = "db.example.com"

// hostNameOnlyCert returns a self-signed certificate that names only
// tlsTestHost, with no IP address SAN, and its PEM encoding for use as
// sslrootcert.
func hostNameOnlyCert(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: tlsTestHost},
		DNSNames:              []string{tlsTestHost},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// fakeServerResult is what fakeTLSPostgres saw of one connection.
type fakeServerResult struct {
	sni     string
	startup map[string]string
	err     error
}

// fakeTLSPostgres accepts one connection on ln and plays the server side
// of a PostgreSQL SSLRequest, TLS handshake (with cert) and startup,
// accepting the startup without authentication. It reports the SNI the
// client sent and the startup parameters it received.
func fakeTLSPostgres(ln net.Listener, cert tls.Certificate) <-chan fakeServerResult {
	done := make(chan fakeServerResult, 1)
	go func() {
		var res fakeServerResult
		defer func() { done <- res }()

		raw, err := ln.Accept()
		if err != nil {
			res.err = err
			return
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(10 * time.Second))

		msg, err := pgproto3.NewBackend(raw, raw).ReceiveStartupMessage()
		if err != nil {
			res.err = err
			return
		}
		if _, ok := msg.(*pgproto3.SSLRequest); !ok {
			res.err = fmt.Errorf("first message is %T, want SSLRequest", msg)
			return
		}
		if _, err := raw.Write([]byte("S")); err != nil {
			res.err = err
			return
		}

		tlsConn := tls.Server(raw, &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
			GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				res.sni = hello.ServerName
				return nil, nil
			},
		})
		if err := tlsConn.Handshake(); err != nil {
			res.err = err
			return
		}

		backend := pgproto3.NewBackend(tlsConn, tlsConn)
		msg, err = backend.ReceiveStartupMessage()
		if err != nil {
			res.err = err
			return
		}
		startup, ok := msg.(*pgproto3.StartupMessage)
		if !ok {
			res.err = fmt.Errorf("startup message is %T", msg)
			return
		}
		res.startup = startup.Parameters

		backend.Send(&pgproto3.AuthenticationOk{})
		backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 1}})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		if err := backend.Flush(); err != nil {
			res.err = err
			return
		}
		// Wait for the client to terminate and close.
		_, _ = backend.Receive()
	}()
	return done
}

// clearPGEnvForTLSTest stops PG* variables in the test environment
// from adding settings pgx would apply on top of the connection string.
func clearPGEnvForTLSTest(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PGHOST", "PGHOSTADDR", "PGPORT", "PGSSLMODE", "PGSSLCERT",
		"PGSSLKEY", "PGSSLROOTCERT", "PGSSLPASSWORD", "PGSSLSNI",
		"PGSSLNEGOTIATION", "PGSERVICE", "PGSERVICEFILE", "PGPASSFILE",
		"PGTARGETSESSIONATTRS", "PGCONNECT_TIMEOUT",
	} {
		t.Setenv(name, "")
	}
}

// TestHostAddrVerifyFullHostNameCertificate is the regression test for
// issue #840: a monitored connection with a host and a hostaddr, using
// sslmode=verify-full against a certificate that names only the host,
// must dial the hostaddr, send the host as SNI, verify the certificate
// against the host name, and not send hostaddr as a startup parameter.
// The host name does not resolve, so the connection can only succeed by
// dialing the hostaddr.
func TestHostAddrVerifyFullHostNameCertificate(t *testing.T) {
	clearPGEnvForTLSTest(t)
	cert, certPEM := hostNameOnlyCert(t)
	dir := sslTestDir(t)
	rootCert := filepath.Join(dir, "root.crt")
	writeTestFile(t, rootCert, certPEM)

	ds := &Datastore{}
	ds.SetSSLCertDir(dir)

	connect := func(t *testing.T, host, hostaddr string) (fakeServerResult, error) {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		done := fakeTLSPostgres(ln, cert)

		port, err := strconv.Atoi(strings.TrimPrefix(
			ln.Addr().String(), "127.0.0.1:"))
		if err != nil {
			t.Fatal(err)
		}
		conn := &MonitoredConnection{
			ID:           1,
			Host:         host,
			Port:         port,
			DatabaseName: "mydb",
			Username:     "user",
			SSLMode:      sql.NullString{String: "verify-full", Valid: true},
			SSLRootCert:  sql.NullString{String: rootCert, Valid: true},
		}
		if hostaddr != "" {
			conn.HostAddr = sql.NullString{String: hostaddr, Valid: true}
		}
		connStr, err := ds.BuildConnectionString(conn, "pw", "")
		if err != nil {
			t.Fatalf("BuildConnectionString: %v", err)
		}
		cfg, err := pgconn.ParseConfig(connStr)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		ds.GuardMonitoredConnConfig(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pgConn, connErr := pgconn.ConnectConfig(ctx, cfg)
		if connErr == nil {
			_ = pgConn.Close(ctx)
		} else {
			// Unblock the server if the client gave up first.
			_ = ln.Close()
		}
		return <-done, connErr
	}

	t.Run("hostaddr with host name", func(t *testing.T) {
		res, err := connect(t, tlsTestHost, "127.0.0.1")
		if err != nil {
			t.Fatalf("connect failed: %v (server: %v)", err, res.err)
		}
		if res.err != nil {
			t.Fatalf("server: %v", res.err)
		}
		if res.sni != tlsTestHost {
			t.Errorf("SNI = %q, want %q", res.sni, tlsTestHost)
		}
		if v, ok := res.startup["hostaddr"]; ok {
			t.Errorf("hostaddr = %q sent as a startup parameter", v)
		}
		if res.startup["user"] != "user" || res.startup["database"] != "mydb" {
			t.Errorf("startup parameters = %v, want user and mydb", res.startup)
		}
	})

	// The control: connecting by the address alone checks the
	// certificate against the IP, which it does not name, so it must
	// fail. This is what every hostaddr connection did before #840.
	t.Run("address alone fails verification", func(t *testing.T) {
		res, err := connect(t, "127.0.0.1", "")
		var hostErr x509.HostnameError
		if !errors.As(err, &hostErr) {
			t.Fatalf("connect error = %v, want an x509.HostnameError", err)
		}
		if res.startup != nil {
			t.Errorf("server received a startup message despite the failed verification")
		}
	})
}
