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
	"database/sql"
	"errors"

	"github.com/pgedge/ai-workbench/pkg/sslpaths"
)

// ErrUnsafeSSLPath is returned at connect time when a stored sslcert,
// sslkey or sslrootcert path fails the directory check. Its text names
// no path, so it is safe to return to a caller.
var ErrUnsafeSSLPath = errors.New("stored connection SSL file path is not permitted")

// SetSSLCertDir sets the directory that stored SSL file paths must lie
// in when BuildConnectionString builds a connection string. The server
// sets it once at start-up from connection_security.ssl_cert_dir.
func (d *Datastore) SetSSLCertDir(dir string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sslCertDir = dir
}

// SSLCertDir returns the directory set by SetSSLCertDir, or "" for a nil
// Datastore, which then refuses every SSL file path.
func (d *Datastore) SSLCertDir() string {
	if d == nil {
		return ""
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.sslCertDir
}

// checkStoredSSLPaths applies sslpaths.ValidateSSLFilePaths to a stored
// connection at connect time, so a row written before the API checked
// these paths, or by any other route, cannot make the server read a file
// outside the directory.
func (d *Datastore) checkStoredSSLPaths(conn *MonitoredConnection) error {
	stored := func(v sql.NullString) *string {
		if v.Valid && v.String != "" {
			s := v.String
			return &s
		}
		return nil
	}
	return sslpaths.ValidateSSLFilePaths(d.SSLCertDir(), sslpaths.SSLFilePaths{
		SSLCert:     stored(conn.SSLCert),
		SSLKey:      stored(conn.SSLKey),
		SSLRootCert: stored(conn.SSLRootCert),
	})
}
