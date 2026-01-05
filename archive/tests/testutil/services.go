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
    "context"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "syscall"
    "time"
)

// Service represents a running service process
type Service struct {
    Name    string
    Cmd     *exec.Cmd
    LogFile *os.File
    cancel  context.CancelFunc
}

// StartCollector starts the collector service
func StartCollector(configPath string) (*Service, error) {
    // Get the tests directory
    testsDir, err := getTestsDir()
    if err != nil {
        return nil, fmt.Errorf("failed to find tests directory: %w", err)
    }

    // Find collector binary
    collectorPath := filepath.Join(testsDir, "..", "collector", "collector")
    if _, err := os.Stat(collectorPath); os.IsNotExist(err) {
        return nil, fmt.Errorf("collector binary not found at %s", collectorPath)
    }

    // Create log file
    logFile, err := os.Create(filepath.Join(testsDir, "logs", fmt.Sprintf("collector-%d.log", time.Now().Unix())))
    if err != nil {
        return nil, fmt.Errorf("failed to create log file: %w", err)
    }

    // Create context for cancellation
    ctx, cancel := context.WithCancel(context.Background())

    // Start collector
    cmd := exec.CommandContext(ctx, collectorPath, "-config", configPath, "-v")
    cmd.Stdout = io.MultiWriter(logFile, os.Stdout)
    cmd.Stderr = io.MultiWriter(logFile, os.Stderr)
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    if err := cmd.Start(); err != nil {
        cancel()
        logFile.Close()
        return nil, fmt.Errorf("failed to start collector: %w", err)
    }

    // Wait a bit for startup
    time.Sleep(2 * time.Second)

    return &Service{
        Name:    "collector",
        Cmd:     cmd,
        LogFile: logFile,
        cancel:  cancel,
    }, nil
}

// StartMCPServer starts the MCP server
func StartMCPServer(configPath string, port int) (*Service, error) {
    // Get the tests directory
    testsDir, err := getTestsDir()
    if err != nil {
        return nil, fmt.Errorf("failed to find tests directory: %w", err)
    }

    // Find server binary
    serverPath := filepath.Join(testsDir, "..", "server", "mcp-server")
    if _, err := os.Stat(serverPath); os.IsNotExist(err) {
        return nil, fmt.Errorf("mcp-server binary not found at %s", serverPath)
    }

    // Create log file
    logFile, err := os.Create(filepath.Join(testsDir, "logs", fmt.Sprintf("mcp-server-%d.log", time.Now().Unix())))
    if err != nil {
        return nil, fmt.Errorf("failed to create log file: %w", err)
    }

    // Create context for cancellation
    ctx, cancel := context.WithCancel(context.Background())

    // Start server
    cmd := exec.CommandContext(ctx, serverPath,
        "-config", configPath,
        "-port", fmt.Sprintf("%d", port),
        "-v")
    cmd.Stdout = io.MultiWriter(logFile, os.Stdout)
    cmd.Stderr = io.MultiWriter(logFile, os.Stderr)
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    if err := cmd.Start(); err != nil {
        cancel()
        logFile.Close()
        return nil, fmt.Errorf("failed to start MCP server: %w", err)
    }

    // Wait a bit for startup
    time.Sleep(2 * time.Second)

    return &Service{
        Name:    "mcp-server",
        Cmd:     cmd,
        LogFile: logFile,
        cancel:  cancel,
    }, nil
}

// Stop stops the service gracefully
func (s *Service) Stop() error {
    if s.Cmd == nil || s.Cmd.Process == nil {
        return nil
    }

    // Send SIGTERM for graceful shutdown
    if err := s.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
        return fmt.Errorf("failed to send SIGTERM to %s: %w", s.Name, err)
    }

    // Wait for process to exit with timeout
    done := make(chan error, 1)
    go func() {
        done <- s.Cmd.Wait()
    }()

    select {
    case <-time.After(10 * time.Second):
        // Force kill if graceful shutdown fails
        if err := s.Cmd.Process.Kill(); err != nil {
            return fmt.Errorf("failed to kill %s: %w", s.Name, err)
        }
        <-done // Wait for process to be killed
    case err := <-done:
        if err != nil && err.Error() != "signal: terminated" {
            return fmt.Errorf("%s exited with error: %w", s.Name, err)
        }
    }

    // Cancel context
    if s.cancel != nil {
        s.cancel()
    }

    // Close log file
    if s.LogFile != nil {
        s.LogFile.Close()
    }

    return nil
}

// IsRunning checks if the service is still running
func (s *Service) IsRunning() bool {
    if s.Cmd == nil || s.Cmd.Process == nil {
        return false
    }

    // Send signal 0 to check if process exists
    err := s.Cmd.Process.Signal(syscall.Signal(0))
    return err == nil
}
