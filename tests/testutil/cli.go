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
    "bytes"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

// CLIClient wraps the AI CLI for testing
type CLIClient struct {
    ServerURL string
    Token     string
    cliPath   string
}

// NewCLIClient creates a new CLI client for testing
func NewCLIClient(serverURL string) (*CLIClient, error) {
    // Get the tests directory
    testsDir, err := getTestsDir()
    if err != nil {
        return nil, fmt.Errorf("failed to find tests directory: %w", err)
    }

    // Find CLI binary
    cliPath := filepath.Join(testsDir, "..", "cli", "ai-cli")
    if _, err := os.Stat(cliPath); os.IsNotExist(err) {
        return nil, fmt.Errorf("ai-cli binary not found at %s", cliPath)
    }

    return &CLIClient{
        ServerURL: serverURL,
        cliPath:   cliPath,
    }, nil
}

// SetToken sets the bearer token for authentication
func (c *CLIClient) SetToken(token string) {
    c.Token = token
}

// RunTool executes a tool via the CLI
func (c *CLIClient) RunTool(toolName string, input map[string]interface{}) (map[string]interface{}, error) {
    // Marshal input to JSON
    inputJSON, err := json.Marshal(input)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal input: %w", err)
    }

    // Build command
    args := []string{"--server", c.ServerURL}
    if c.Token != "" {
        args = append(args, "--token", c.Token)
    }
    args = append(args, "run-tool", toolName)

    cmd := exec.Command(c.cliPath, args...)
    cmd.Stdin = bytes.NewReader(inputJSON)

    // Capture output
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    // Run command
    err = cmd.Run()
    if err != nil {
        return nil, fmt.Errorf("CLI command failed: %w\nStderr: %s", err, stderr.String())
    }

    // Parse output
    var result map[string]interface{}
    if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
        return nil, fmt.Errorf("failed to parse CLI output: %w\nOutput: %s", err, stdout.String())
    }

    return result, nil
}

// ReadResource reads a resource via the CLI
func (c *CLIClient) ReadResource(uri string) (map[string]interface{}, error) {
    // Build command
    args := []string{"--server", c.ServerURL}
    if c.Token != "" {
        args = append(args, "--token", c.Token)
    }
    args = append(args, "read-resource", uri)

    cmd := exec.Command(c.cliPath, args...)

    // Capture output
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    // Run command
    err := cmd.Run()
    if err != nil {
        return nil, fmt.Errorf("CLI command failed: %w\nStderr: %s", err, stderr.String())
    }

    // Parse output
    var result map[string]interface{}
    if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
        return nil, fmt.Errorf("failed to parse CLI output: %w\nOutput: %s", err, stdout.String())
    }

    return result, nil
}

// Ping pings the server via the CLI
func (c *CLIClient) Ping() error {
    args := []string{"--server", c.ServerURL, "ping"}
    cmd := exec.Command(c.cliPath, args...)

    var stderr bytes.Buffer
    cmd.Stderr = &stderr

    err := cmd.Run()
    if err != nil {
        return fmt.Errorf("ping failed: %w\nStderr: %s", err, stderr.String())
    }

    return nil
}

// ListTools lists available tools via the CLI
func (c *CLIClient) ListTools() ([]string, error) {
    args := []string{"--server", c.ServerURL}
    if c.Token != "" {
        args = append(args, "--token", c.Token)
    }
    args = append(args, "list-tools")

    cmd := exec.Command(c.cliPath, args...)

    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    err := cmd.Run()
    if err != nil {
        return nil, fmt.Errorf("list-tools failed: %w\nStderr: %s", err, stderr.String())
    }

    // Parse output - just get tool names
    lines := bytes.Split(stdout.Bytes(), []byte("\n"))
    var tools []string
    for _, line := range lines {
        if len(line) > 0 {
            // Extract tool name (before " - ")
            parts := bytes.SplitN(line, []byte(" - "), 2)
            tools = append(tools, string(parts[0]))
        }
    }

    return tools, nil
}

// Authenticate authenticates a user and returns session token
func (c *CLIClient) Authenticate(username, password string) (string, error) {
    input := map[string]interface{}{
        "username": username,
        "password": password,
    }

    result, err := c.RunTool("authenticate_user", input)
    if err != nil {
        return "", err
    }

    // Extract token from result
    content, ok := result["content"].([]interface{})
    if !ok || len(content) == 0 {
        return "", fmt.Errorf("no content in authentication response")
    }

    firstContent, ok := content[0].(map[string]interface{})
    if !ok {
        return "", fmt.Errorf("unexpected content format")
    }

    text, ok := firstContent["text"].(string)
    if !ok {
        return "", fmt.Errorf("no text in response")
    }

    // Extract token from text
    // Format: "Authentication successful. Session token: <token>\nExpires at: <timestamp>"
    lines := strings.Split(text, "\n")
    for _, line := range lines {
        if strings.HasPrefix(line, "Authentication successful. Session token: ") {
            token := strings.TrimPrefix(line, "Authentication successful. Session token: ")
            token = strings.TrimSpace(token)
            if token == "" {
                return "", fmt.Errorf("empty token in response")
            }
            return token, nil
        }
    }

    return "", fmt.Errorf("failed to extract token from response: %s", text)
}
