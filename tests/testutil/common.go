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
    "os"
    "path/filepath"
)

// getTestsDir finds the tests directory
func getTestsDir() (string, error) {
    // Get current working directory
    cwd, err := os.Getwd()
    if err != nil {
        return "", err
    }

    // Check if we're in integration/ subdirectory
    if filepath.Base(cwd) == "integration" {
        return filepath.Dir(cwd), nil
    }

    // Otherwise assume we're already in tests/
    return cwd, nil
}
