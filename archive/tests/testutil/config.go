/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package testutil

import (
    "fmt"
    "net/url"
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

    // Extract password from TEST_AI_WORKBENCH_SERVER and create password file if needed
    adminConnStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
    if adminConnStr == "" {
        adminConnStr = "postgres://postgres@localhost:5432/postgres"
    }

    // Parse connection string to extract password
    parsedURL, err := url.Parse(adminConnStr)
    if err != nil {
        return "", fmt.Errorf("failed to parse connection string: %w", err)
    }

    // Create password file if password is present
    passwordFile := ""
    if parsedURL.User != nil {
        if password, hasPassword := parsedURL.User.Password(); hasPassword {
            passwordFile = filepath.Join(testsDir, "config", fmt.Sprintf("test-%s.pwd", dbName))
            if err := os.WriteFile(passwordFile, []byte(password), 0600); err != nil {
                return "", fmt.Errorf("failed to write password file: %w", err)
            }
            config = strings.ReplaceAll(config, "TEST_PASSWORD_FILE", passwordFile)
        } else {
            // No password in connection string, remove the password_file line
            config = strings.ReplaceAll(config, "pg_password_file = TEST_PASSWORD_FILE\n", "")
        }
    } else {
        // No user info, remove the password_file line
        config = strings.ReplaceAll(config, "pg_password_file = TEST_PASSWORD_FILE\n", "")
    }

    // Write config file
    configPath := filepath.Join(testsDir, "config", fmt.Sprintf("test-%s.conf", dbName))
    if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
        return "", fmt.Errorf("failed to write config file: %w", err)
    }

    return configPath, nil
}

// CleanupTestConfig removes a test configuration file and associated password file
func CleanupTestConfig(configPath string) error {
    // Remove config file
    if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("failed to remove config file: %w", err)
    }

    // Remove password file if it exists
    passwordFile := strings.Replace(configPath, ".conf", ".pwd", 1)
    if err := os.Remove(passwordFile); err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("failed to remove password file: %w", err)
    }

    return nil
}
