/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package sslpaths confines the SSL certificate, key and root
// certificate files of a monitored connection to one directory. pgx
// reads those files as soon as it parses a connection string, so a
// stored path is a file-read primitive for whoever can create a
// connection. The server checks the paths when a connection is written
// and again at connect time, and the collector checks them before it
// builds a pool.
package sslpaths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// SSLRootCertSystem is the sslrootcert value that selects the platform
// trust store rather than a file, so it is not a path and needs no
// directory check.
const SSLRootCertSystem = "system"

// ErrSSLCertDirUnset is returned when a connection names an SSL file but
// no connection_security.ssl_cert_dir is configured. Its text is meant
// for the API caller: it says what the operator has to do. The server
// and the collector each have the option, under the same name.
var ErrSSLCertDirUnset = errors.New(
	"SSL certificate and key files are disabled on this server; an " +
		"administrator must set connection_security.ssl_cert_dir to the " +
		"directory that holds them")

// SSLFilePathError reports which SSL field was refused and why. Its
// message names the field and the rule, never the path, so a handler may
// return it to the caller.
type SSLFilePathError struct {
	Field  string
	Reason string
}

func (e *SSLFilePathError) Error() string {
	return fmt.Sprintf("%s %s", e.Field, e.Reason)
}

// ValidateSSLFilePath checks that p, a path the server or collector
// will open when it connects to a monitored database, is an absolute
// path inside baseDir. Each process reads these files with its own
// privileges, so without the check anyone allowed to create a
// connection could make it open any file it can read, and learn from
// the error whether the file exists.
//
// The path must be absolute and must not contain a ".." element. It is
// then resolved through filepath.EvalSymlinks; when the file does not
// exist yet, the nearest existing ancestor is resolved instead and the
// rest of the path appended, so a symbolic link in the directory cannot
// point outside it whether or not the file has been created. The result
// must lie strictly inside the resolved baseDir.
//
// An empty path is always accepted, since it names no file. Any other
// path is refused with ErrSSLCertDirUnset when baseDir is empty.
func ValidateSSLFilePath(baseDir, p string) error {
	if p == "" {
		return nil
	}
	if baseDir == "" {
		return ErrSSLCertDirUnset
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return errors.New("must not contain control characters")
		}
	}
	if !filepath.IsAbs(p) {
		return errors.New("must be an absolute path")
	}
	for _, elem := range strings.Split(filepath.ToSlash(p), "/") {
		if elem == ".." {
			return errors.New("must not contain '..'")
		}
	}

	base := resolvePath(filepath.Clean(baseDir))
	target := resolvePath(filepath.Clean(p))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(rel) {
		return errors.New("must be inside the configured SSL certificate directory")
	}
	return nil
}

// resolvePath returns p with every symbolic link resolved. When p does
// not exist, it resolves the nearest ancestor that does and appends the
// remainder unchanged; when nothing resolves, it returns p as given,
// which callers have already cleaned.
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	rest := ""
	dir := p
	for {
		parent := filepath.Dir(dir)
		rest = filepath.Join(filepath.Base(dir), rest)
		if parent == dir {
			return p
		}
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		} else if !errors.Is(err, os.ErrNotExist) {
			return p
		}
	}
}

// SSLFilePaths holds the three SSL file settings of a connection. A nil
// field is one a request leaves unchanged.
type SSLFilePaths struct {
	SSLCert     *string
	SSLKey      *string
	SSLRootCert *string
}

// ValidateSSLFilePaths checks each non-nil field with
// ValidateSSLFilePath, accepting sslrootcert=system. It returns
// ErrSSLCertDirUnset unwrapped, so a caller can tell the operator what to
// configure, and otherwise an *SSLFilePathError naming the field.
func ValidateSSLFilePaths(baseDir string, paths SSLFilePaths) error {
	fields := []struct {
		name  string
		value *string
	}{
		{"ssl_cert_path", paths.SSLCert},
		{"ssl_key_path", paths.SSLKey},
		{"ssl_root_cert_path", paths.SSLRootCert},
	}
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		if f.name == "ssl_root_cert_path" && *f.value == SSLRootCertSystem {
			continue
		}
		err := ValidateSSLFilePath(baseDir, *f.value)
		if errors.Is(err, ErrSSLCertDirUnset) {
			return err
		}
		if err != nil {
			return &SSLFilePathError{Field: f.name, Reason: err.Error()}
		}
	}
	return nil
}
