/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package main is the entry point for the AI Workbench CLI
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

const (
	version = "0.1.0"
)

func main() {
	// Define command-line flags
	serverURL := flag.String("server", "http://localhost:8080", "MCP server URL")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("AI Workbench CLI v%s\n", version)
		os.Exit(0)
	}

	// Get command and arguments
	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	command := args[0]
	var commandArgs []string
	if len(args) > 1 {
		commandArgs = args[1:]
	}

	// Create MCP client
	client := NewMCPClient(*serverURL)

	// Execute command
	var err error
	switch command {
	case "run-tool":
		err = runTool(client, commandArgs)
	case "read-resource":
		err = readResource(client, commandArgs)
	case "ping":
		err = ping(client)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `AI Workbench CLI - MCP Server Interaction Tool

Usage:
    ai-cli [options] <command> [arguments]

Options:
    -server <url>    MCP server URL (default: http://localhost:8080)
    -version         Show version information

Commands:
    run-tool <tool-name>         Run an MCP tool
    read-resource <resource-uri> Read an MCP resource
    ping                         Ping the server

Examples:
    # Ping the server
    ai-cli ping

    # Run a tool with JSON input from stdin
    echo '{"key": "value"}' | ai-cli run-tool set_config

    # Read a resource
    ai-cli read-resource system://stats

    # Use a different server
    ai-cli -server http://example.com:9000 ping

For more information, see the documentation at docs/index.md
`)
}

func runTool(client *MCPClient, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("tool name required\n\nUsage: ai-cli run-tool <tool-name>\n\nExample JSON input:\n%s",
			getToolExample())
	}

	toolName := args[0]

	// Read JSON input from stdin
	var inputData map[string]interface{}
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Data is being piped
		decoder := json.NewDecoder(os.Stdin)
		if err := decoder.Decode(&inputData); err != nil {
			return fmt.Errorf("failed to parse JSON input: %w", err)
		}
	} else {
		// No piped data, show example
		fmt.Fprintf(os.Stderr, "No input provided. Example JSON input:\n%s\n\n", getToolExample())
		fmt.Fprintf(os.Stderr, "Usage: echo '{\"key\": \"value\"}' | ai-cli run-tool %s\n", toolName)
		return fmt.Errorf("no input provided")
	}

	// Call the tool
	result, err := client.CallTool(toolName, inputData)
	if err != nil {
		return fmt.Errorf("failed to call tool: %w", err)
	}

	// Pretty-print the result
	return printJSON(result)
}

func readResource(client *MCPClient, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("resource URI required\n\nUsage: ai-cli read-resource <resource-uri>\n\nExample: ai-cli read-resource system://stats")
	}

	resourceURI := args[0]

	// Read the resource
	result, err := client.ReadResource(resourceURI)
	if err != nil {
		return fmt.Errorf("failed to read resource: %w", err)
	}

	// Pretty-print the result
	return printJSON(result)
}

func ping(client *MCPClient) error {
	result, err := client.Ping()
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	// Pretty-print the result
	return printJSON(result)
}

func printJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return nil
}

func getToolExample() string {
	return `{
    "config_key": "setting_name",
    "config_value": "new_value"
}`
}

func readAllStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin)
}
