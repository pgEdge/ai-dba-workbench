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
	"testing"
)

// TestSecretFileResolutionMatches checks that the partial read the
// command line uses agrees with the full configuration load about where
// the server secret lives.
//
// The two must never diverge. The server keys the audit chain from the
// secret LoadConfig resolves, whilst every CLI mutation keys it from
// the one LoadConfigSecretFile resolves, so a difference between them
// would have the two writing rows neither could verify, and the first
// anyone would hear of it is a verify-audit-log that fails. Nothing in
// LoadConfig expands or overrides secret_file today: loadConfigFile is
// a plain YAML unmarshal with no environment expansion, applyEnvOverrides
// names only PGEDGE_MEMORY_ENABLED and PGEDGE_AUDIT_RETENTION_DAYS, and
// the CLIFlags.SecretFile branch in applyCLIFlags is reachable from no
// flag. This test is what notices if any of that changes.
func TestSecretFileResolutionMatches(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "absolute path",
			yaml: "secret_file: /etc/pgedge/other.secret\n",
			want: "/etc/pgedge/other.secret",
		},
		{
			name: "unset",
			yaml: "data_dir: /var/lib/pgedge\n",
			want: "",
		},
		{
			name: "value that looks like an environment reference",
			yaml: "secret_file: ${HOME}/secret\n",
			want: "${HOME}/secret",
		},
		{
			name: "quoted value with a space",
			yaml: "secret_file: \"/etc/pgedge/my secret\"\n",
			want: "/etc/pgedge/my secret",
		},
	}

	// A value that would differ if anything expanded it.
	t.Setenv("HOME", "/home/expanded")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ai-dba-server.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
				t.Fatalf("failed to write the config file: %v", err)
			}

			partial, err := LoadConfigSecretFile(path)
			if err != nil {
				t.Fatalf("LoadConfigSecretFile failed: %v", err)
			}
			if partial != tc.want {
				t.Errorf("LoadConfigSecretFile = %q, want %q", partial, tc.want)
			}

			full, err := LoadConfig(path, CLIFlags{ConfigFileSet: true})
			if err != nil {
				t.Fatalf("LoadConfig failed: %v", err)
			}
			if full.SecretFile != partial {
				t.Errorf("LoadConfig resolved secret_file to %q whilst the "+
					"command line resolves %q; the server and the CLI would "+
					"key the audit chain from different secrets",
					full.SecretFile, partial)
			}
		})
	}
}
