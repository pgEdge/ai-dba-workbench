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
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/fileutil"
)

// cliCommandMainEnv names the environment variable that makes the test
// binary, re-executed by TestMainCLICommandConfigFile, run main() with
// -add-group. Its value is the -config path, or "" for none, and the
// -data-dir path, separated by a newline.
const cliCommandMainEnv = "AI_DBA_TEST_CLI_COMMAND_MAIN"

// TestMainCLICommandConfigFile runs a management command through main()
// in a child process and checks how it treats the configuration file
// (issue #861). A -config path that does not exist must stop the
// command, as it stops start-up, rather than let it fall back to the
// default data directory and secret and write to an auth.db the
// configured server never reads. Without -config, a missing default
// still falls back to the defaults, and an existing file still works.
//
// Every case passes -data-dir, so that a command which wrongly ran
// would write only to a temporary directory, and the child isolates the
// default configuration search from the host.
func TestMainCLICommandConfigFile(t *testing.T) {
	if spec := os.Getenv(cliCommandMainEnv); spec != "" {
		configPath, dataDir, _ := strings.Cut(spec, "\n")
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		fileutil.SetSystemConfigDirForTest(t, "")

		args := []string{"ai-dba-server"}
		if configPath != "" {
			args = append(args, "-config", configPath)
		}
		args = append(args, "-data-dir", dataDir, "-add-group", "-group", "operators")
		flag.CommandLine = flag.NewFlagSet("ai-dba-server", flag.ExitOnError)
		os.Args = args
		main()
		// main returns after a command has run; anything after that
		// would start the server, so exit here as the real binary does.
		os.Exit(0)
	}

	existing := writeCheckConfig(t, "http:\n  address: \":8443\"\n")
	missing := filepath.Join(t.TempDir(), "ai-dba-server.yml")

	tests := []struct {
		name       string
		configPath string
		wantCode   int
		wantOut    string
		wantStore  bool
	}{
		{
			name:       "missing explicit config",
			configPath: missing,
			wantCode:   1,
			wantOut:    "ERROR: configuration file not found: " + missing,
		},
		{
			name:      "no config and no default",
			wantCode:  0,
			wantOut:   "Group 'operators' created successfully",
			wantStore: true,
		},
		{
			name:       "existing explicit config",
			configPath: existing,
			wantCode:   0,
			wantOut:    "Group 'operators' created successfully",
			wantStore:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := filepath.Join(t.TempDir(), "data")
			code, out := runMainChild(t, "TestMainCLICommandConfigFile",
				cliCommandMainEnv, tt.configPath+"\n"+dataDir)
			if code != tt.wantCode {
				t.Fatalf("exit status %d, want %d; output:\n%s", code, tt.wantCode, out)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Fatalf("output lacks %q:\n%s", tt.wantOut, out)
			}
			_, statErr := os.Stat(filepath.Join(dataDir, "auth.db"))
			if tt.wantStore && statErr != nil {
				t.Fatalf("expected the command to create auth.db: %v", statErr)
			}
			if !tt.wantStore {
				if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
					t.Fatalf("the command wrote to the data directory (stat: %v)", err)
				}
			}
		})
	}
}
