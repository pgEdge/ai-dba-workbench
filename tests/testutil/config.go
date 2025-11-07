/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package testutil

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"
)

// CreateTestConfig creates a test configuration file
func CreateTestConfig(dbName string) (string, error) {
    // Get the tests directory (parent of current working directory if in integration/)
    testsDir, err := getTestsDir()
    if err != nil {
        return "", fmt.Errorf("failed to find tests directory: %w", err)
    }

    // Read template
    templatePath := filepath.Join(testsDir, "config", "test.conf.template")
    template, err := os.ReadFile(templatePath)
    if err != nil {
        return "", fmt.Errorf("failed to read config template: %w", err)
    }

    // Replace database name
    config := strings.ReplaceAll(string(template), "TEST_DATABASE_NAME", dbName)

    // Write config file
    configPath := filepath.Join(testsDir, "config", fmt.Sprintf("test-%s.conf", dbName))
    if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
        return "", fmt.Errorf("failed to write config file: %w", err)
    }

    return configPath, nil
}

// CleanupTestConfig removes a test configuration file
func CleanupTestConfig(configPath string) error {
    if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("failed to remove config file: %w", err)
    }
    return nil
}
