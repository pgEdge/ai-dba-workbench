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
// point outside it whether or not the file has been created. A symbolic
// link whose target does not exist is refused, since its unresolved name
// says nothing about where it points, and so is a path that cannot be
// resolved for any other reason (a permission error, a component that
// is not a directory, a symbolic link loop). The result must lie
// strictly inside the resolved baseDir.
//
// Every refusal after the syntax checks, whether the path resolves
// outside baseDir or cannot be resolved at all, returns the same
// errOutsideDir text. The reason reaches API responses, and resolution
// fails in different ways depending on what exists outside baseDir (a
// path through an existing regular file fails with ENOTDIR, one through
// a missing directory resolves and then lies outside), so distinct
// messages would tell the caller whether a file outside the directory
// exists.
//
// An empty path is always accepted, since it names no file. Any other
// path is refused with ErrSSLCertDirUnset when baseDir is empty.
//
// The check assumes that baseDir and everything in it are writable only
// by root or the calling process's service account. The path is resolved
// here but opened later by pgx, so anyone who could replace a symbolic
// link or file inside baseDir between the two steps could still point
// pgx at a file outside it; the check does not defend against that.
func ValidateSSLFilePath(baseDir, p string) error {
	if p == "" {
		return nil
	}
	if baseDir == "" {
		return ErrSSLCertDirUnset
	}
	if err := checkPathSyntax(p); err != nil {
		return err
	}

	base, err := resolvePath(filepath.Clean(baseDir))
	if err != nil {
		return errOutsideDir
	}
	target, err := resolvePath(filepath.Clean(p))
	if err != nil {
		return errOutsideDir
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(rel) {
		return errOutsideDir
	}
	return nil
}

// errOutsideDir is ValidateSSLFilePath's one refusal for a path that
// does not resolve to a location inside the directory, for whatever
// reason. See ValidateSSLFilePath for why the reasons are not told
// apart.
var errOutsideDir = errors.New("must be inside the configured SSL certificate directory")

// checkPathSyntax applies the checks on p that need no file system
// access: no control characters, an absolute path, and no ".." element.
func checkPathSyntax(p string) error {
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
	return nil
}

// errDanglingLink is resolvePath's refusal of a path that runs through
// a symbolic link whose target does not exist. ValidateSSLFilePath
// reports it, like errUnresolvable, as errOutsideDir.
var errDanglingLink = errors.New("must not be a symbolic link to a file that does not exist")

// errUnresolvable is resolvePath's refusal of a path that cannot be
// resolved for any reason other than part of it not existing: a
// permission error, a component that is not a directory, or a symbolic
// link loop. The OS error is not passed on.
var errUnresolvable = errors.New("must be a path whose symbolic links can be resolved")

// resolvePath returns p with every symbolic link resolved. When p does
// not exist, it resolves the nearest ancestor that does and appends the
// remainder unchanged; when nothing resolves, it returns p as given,
// which callers have already cleaned.
//
// Any EvalSymlinks error other than os.ErrNotExist is refused with
// errUnresolvable: the unresolved path would otherwise pass the
// containment check without anything having said where it leads.
//
// A component that exists (os.Lstat finds it) but does not resolve is a
// symbolic link whose target is missing. Appending its name unresolved
// would make the path look as though it stays inside the directory
// whilst the link points anywhere, and creating the target later would
// redirect pgx there, so such a path is refused with errDanglingLink. A
// missing file or directory that is not a link is still accepted, as
// one that has not been created yet.
func resolvePath(p string) (string, error) {
	rest := ""
	dir := p
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", errUnresolvable
		}
		if _, lerr := os.Lstat(dir); lerr == nil {
			return "", errDanglingLink
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p, nil
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
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
