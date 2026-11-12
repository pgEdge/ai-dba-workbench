/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package sslpaths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sslTestDir returns a fresh certificate directory with every symbolic
// link in its path resolved, so expectations do not depend on whether
// the system temporary directory is itself a link.
func sslTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// strPtr returns a pointer to s.
func strPtr(s string) *string { return &s }

func TestValidateSSLFilePath(t *testing.T) {
	dir := sslTestDir(t)
	outside := sslTestDir(t)
	writeTestFile(t, filepath.Join(dir, "ca.crt"), []byte("x"))
	writeTestFile(t, filepath.Join(outside, "secret"), []byte("x"))
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "link.crt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "sub", "inner.crt"), []byte("x"))
	if err := os.Symlink(filepath.Join(dir, "sub", "inner.crt"), filepath.Join(dir, "inlink.crt")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		base    string
		path    string
		wantErr string
	}{
		{"empty path", dir, "", ""},
		{"empty path without directory", "", "", ""},
		{"existing file", dir, filepath.Join(dir, "ca.crt"), ""},
		{"file not yet created", dir, filepath.Join(dir, "new.crt"), ""},
		{"missing subdirectory", dir, filepath.Join(dir, "a", "b", "c.crt"), ""},
		{"nested file", dir, filepath.Join(dir, "sub", "inner.crt"), ""},
		{"link staying inside", dir, filepath.Join(dir, "inlink.crt"), ""},
		{"redundant separators", dir, dir + "//./ca.crt", ""},
		{"directory unset", "", filepath.Join(dir, "ca.crt"), "connection_security.ssl_cert_dir"},
		{"relative path", dir, "ca.crt", "absolute"},
		{"dot relative path", dir, "./ca.crt", "absolute"},
		{"dot-dot escape", dir, filepath.Join(dir) + "/../" + filepath.Base(outside) + "/secret", "'..'"},
		{"dot-dot staying inside", dir, dir + "/sub/../ca.crt", "'..'"},
		{"file outside", dir, "/etc/passwd", "inside"},
		{"sibling with shared prefix", dir, dir + "-evil/ca.crt", "inside"},
		{"directory itself", dir, dir, "inside"},
		{"link to file outside", dir, filepath.Join(dir, "link.crt"), "inside"},
		{"link to directory outside", dir, filepath.Join(dir, "linkdir", "secret"), "inside"},
		{"missing file under outside link", dir, filepath.Join(dir, "linkdir", "x", "y.crt"), "inside"},
		{"control character", dir, filepath.Join(dir, "ca\n.crt"), "control"},
		{"NUL", dir, filepath.Join(dir, "ca\x00.crt"), "control"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSSLFilePath(tc.base, tc.path)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSSLFilePath(%q) = %v, want nil", tc.path, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateSSLFilePath(%q) = %v, want an error containing %q",
					tc.path, err, tc.wantErr)
			}
			if strings.Contains(err.Error(), tc.path) && tc.path != "" {
				t.Errorf("error %q repeats the path", err)
			}
		})
	}
}

// TestValidateSSLFilePathLinkedBase checks that a configured directory
// reached through a symbolic link accepts files named by either path.
func TestValidateSSLFilePathLinkedBase(t *testing.T) {
	real := sslTestDir(t)
	link := filepath.Join(sslTestDir(t), "certs")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(real, "ca.crt"), []byte("x"))
	for _, p := range []string{filepath.Join(real, "ca.crt"), filepath.Join(link, "ca.crt"), filepath.Join(link, "new.crt")} {
		if err := ValidateSSLFilePath(link, p); err != nil {
			t.Errorf("ValidateSSLFilePath(%q) = %v, want nil", p, err)
		}
	}
}

// TestResolvePathUnresolvable checks that a path EvalSymlinks fails on
// for a reason other than a missing component is refused rather than
// returned unchanged, whilst a relative path that merely does not exist
// is still returned as given.
func TestResolvePathUnresolvable(t *testing.T) {
	dir := sslTestDir(t)
	file := filepath.Join(dir, "file")
	writeTestFile(t, file, []byte("x"))
	// A path through a regular file fails with ENOTDIR, not ENOENT.
	p := filepath.Join(file, "child", "x.crt")
	if got, err := resolvePath(p); !errors.Is(err, errUnresolvable) || got != "" {
		t.Errorf("resolvePath(%q) = %q, %v, want errUnresolvable", p, got, err)
	}
	if got, err := resolvePath("relative/x.crt"); err != nil || got != "relative/x.crt" {
		t.Errorf("resolvePath(relative) = %q, %v, want it unchanged", got, err)
	}
}

// TestValidateSSLFilePathUnresolvable checks that ValidateSSLFilePath
// fails closed on every EvalSymlinks error other than a missing file: a
// symbolic link loop, a path through a regular file and, unless running
// as root, a directory that cannot be searched. A plain missing file
// beside them is still accepted.
func TestValidateSSLFilePathUnresolvable(t *testing.T) {
	dir := sslTestDir(t)
	if err := os.Symlink(filepath.Join(dir, "loop-b"), filepath.Join(dir, "loop-a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "loop-a"), filepath.Join(dir, "loop-b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("self.crt", filepath.Join(dir, "self.crt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "plain"), []byte("x"))

	paths := []string{
		filepath.Join(dir, "loop-a"),
		filepath.Join(dir, "loop-a", "x.crt"),
		filepath.Join(dir, "self.crt"),
		filepath.Join(dir, "plain", "x.crt"),
	}
	if os.Geteuid() != 0 {
		locked := filepath.Join(dir, "locked")
		if err := os.Mkdir(locked, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(locked, "ca.crt"), []byte("x"))
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
		paths = append(paths, filepath.Join(locked, "ca.crt"))
	}
	for _, p := range paths {
		err := ValidateSSLFilePath(dir, p)
		if err == nil || err.Error() != errUnresolvable.Error() {
			t.Errorf("ValidateSSLFilePath(%q) = %v, want %v", p, err, errUnresolvable)
		}
	}
	if err := ValidateSSLFilePath(dir, filepath.Join(dir, "missing", "new.crt")); err != nil {
		t.Errorf("missing ordinary file: %v, want nil", err)
	}

	// A configured directory that loops holds nothing either.
	if err := ValidateSSLFilePath(filepath.Join(dir, "loop-a"), filepath.Join(dir, "loop-a", "ca.crt")); err == nil {
		t.Error("looping base directory accepted")
	}
}

// TestValidateSSLFilePathDanglingLink checks that a symbolic link whose
// target does not exist is refused wherever it sits in the path and
// wherever it points, inside the directory or out, whilst a missing
// file that is not a link is still accepted.
func TestValidateSSLFilePathDanglingLink(t *testing.T) {
	dir := sslTestDir(t)
	outside := sslTestDir(t)
	links := map[string]string{
		"out.crt":    filepath.Join(outside, "missing.crt"),
		"in.crt":     filepath.Join(dir, "missing.crt"),
		"outdir":     filepath.Join(outside, "missing"),
		"chain.crt":  filepath.Join(dir, "out.crt"),
		"relout.crt": "../" + filepath.Base(outside) + "/missing.crt",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{
		filepath.Join(dir, "out.crt"),
		filepath.Join(dir, "in.crt"),
		filepath.Join(dir, "outdir", "x.crt"),
		filepath.Join(dir, "chain.crt"),
		filepath.Join(dir, "relout.crt"),
	} {
		err := ValidateSSLFilePath(dir, p)
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Errorf("ValidateSSLFilePath(%q) = %v, want a dangling link refusal", p, err)
		}
	}
	if err := ValidateSSLFilePath(dir, filepath.Join(dir, "not-yet.crt")); err != nil {
		t.Errorf("missing ordinary file: %v, want nil", err)
	}

	// A configured directory that is itself a dangling link holds
	// nothing.
	base := filepath.Join(outside, "certs")
	if err := os.Symlink(filepath.Join(outside, "gone"), base); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSSLFilePath(base, filepath.Join(base, "ca.crt")); err == nil {
		t.Error("dangling base directory accepted")
	}
}

func TestValidateSSLFilePaths(t *testing.T) {
	dir := sslTestDir(t)
	inside := filepath.Join(dir, "client.crt")

	if err := ValidateSSLFilePaths("", SSLFilePaths{}); err != nil {
		t.Errorf("no fields: %v", err)
	}
	if err := ValidateSSLFilePaths("", SSLFilePaths{
		SSLCert: strPtr(""), SSLKey: strPtr(""), SSLRootCert: strPtr(SSLRootCertSystem),
	}); err != nil {
		t.Errorf("cleared fields and system root: %v", err)
	}
	if err := ValidateSSLFilePaths(dir, SSLFilePaths{
		SSLCert: &inside, SSLKey: strPtr(filepath.Join(dir, "client.key")),
		SSLRootCert: strPtr(filepath.Join(dir, "ca.crt")),
	}); err != nil {
		t.Errorf("paths inside: %v", err)
	}

	err := ValidateSSLFilePaths("", SSLFilePaths{SSLKey: &inside})
	if !errors.Is(err, ErrSSLCertDirUnset) {
		t.Errorf("unset directory: %v, want ErrSSLCertDirUnset", err)
	}

	for _, tc := range []struct {
		field string
		paths SSLFilePaths
	}{
		{"ssl_cert_path", SSLFilePaths{SSLCert: strPtr("/etc/passwd")}},
		{"ssl_key_path", SSLFilePaths{SSLKey: strPtr("/etc/shadow")}},
		{"ssl_root_cert_path", SSLFilePaths{SSLRootCert: strPtr("/etc/hosts")}},
		// "system" is special for sslrootcert alone.
		{"ssl_cert_path", SSLFilePaths{SSLCert: strPtr(SSLRootCertSystem)}},
	} {
		err := ValidateSSLFilePaths(dir, tc.paths)
		var pathErr *SSLFilePathError
		if !errors.As(err, &pathErr) || pathErr.Field != tc.field {
			t.Errorf("%s: error = %v, want an SSLFilePathError for the field", tc.field, err)
			continue
		}
		if !strings.HasPrefix(err.Error(), tc.field+" ") {
			t.Errorf("error %q does not lead with the field", err)
		}
	}
}
