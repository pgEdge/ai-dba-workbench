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
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/flagutil"
)

// TestApplyFlagOverrides_ExplicitDefaultValueWins covers the case the
// old value-comparison logic could not express: the operator passes a
// flag whose value equals the flag's registered default, and the flag
// must still override the configuration file.
func TestApplyFlagOverrides_ExplicitDefaultValueWins(t *testing.T) {
	cfg := config.NewConfig()
	// Values as if they had come from a configuration file.
	cfg.Datastore.Host = "file-host"
	cfg.Datastore.Port = 6000
	cfg.Datastore.SSLMode = "require"

	// Every value below is the corresponding flag's registered
	// default, yet each flag was explicitly passed, so each must
	// still be applied.
	err := applyFlagOverrides(cfg, flagOverrides{
		DBHost:    "",
		DBPort:    0,
		DBSSLMode: "",
		Passed: flagutil.Set{
			flagDBHost:    true,
			flagDBPort:    true,
			flagDBSSLMode: true,
		},
	})
	if err != nil {
		t.Fatalf("applyFlagOverrides: %v", err)
	}
	if cfg.Datastore.Host != "" {
		t.Errorf("Host = %q, want the explicitly passed empty value", cfg.Datastore.Host)
	}
	if cfg.Datastore.Port != 0 {
		t.Errorf("Port = %d, want the explicitly passed 0", cfg.Datastore.Port)
	}
	if cfg.Datastore.SSLMode != "" {
		t.Errorf("SSLMode = %q, want the explicitly passed empty value", cfg.Datastore.SSLMode)
	}
}

// TestApplyFlagOverrides_ConfigMatchingFlagDefaultSurvives is the
// mirror image: configuration values that coincide with the flags'
// registered defaults must survive when no flag was passed.
func TestApplyFlagOverrides_ConfigMatchingFlagDefaultSurvives(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Datastore.Host = ""
	cfg.Datastore.Port = 0
	cfg.Datastore.SSLMode = ""

	if err := applyFlagOverrides(cfg, flagOverrides{
		DBHost:    "unused-host",
		DBPort:    9999,
		DBSSLMode: "unused-mode",
	}); err != nil {
		t.Fatalf("applyFlagOverrides: %v", err)
	}
	if cfg.Datastore.Host != "" || cfg.Datastore.Port != 0 ||
		cfg.Datastore.SSLMode != "" {
		t.Errorf("unpassed flags overrode the config: %+v", cfg.Datastore)
	}
}

// TestApplyFlagOverrides_ExplicitEmptyPasswordFile verifies that an
// explicitly empty -db-password-file is treated as "no password
// file" rather than as a path to read, which would fail.
func TestApplyFlagOverrides_ExplicitEmptyPasswordFile(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Datastore.Password = "from-config"

	if err := applyFlagOverrides(cfg, flagOverrides{
		DBPasswordFile: "",
		Passed:         flagutil.Set{flagDBPasswordFile: true},
	}); err != nil {
		t.Fatalf("applyFlagOverrides: %v", err)
	}
	if cfg.Datastore.Password != "from-config" {
		t.Errorf("Password = %q, want it left alone", cfg.Datastore.Password)
	}
}
