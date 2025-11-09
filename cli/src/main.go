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
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	version = "0.1.0"
)

func main() {
	// Define command-line flags
	serverURL := flag.String("server", "http://localhost:8080", "MCP server URL")
	token := flag.String("token", "", "Bearer token for authentication")
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

	// Handle authentication for commands that need it
	// Special case: skip authentication for authenticate_user tool
	skipAuth := false
	if command == "run-tool" && len(commandArgs) > 0 && commandArgs[0] == "authenticate_user" {
		skipAuth = true
	}

	if needsAuth(command) && !skipAuth {
		if err := handleAuthentication(client, *token); err != nil {
			fmt.Fprintf(os.Stderr, "Authentication error: %v\n", err)
			os.Exit(1)
		}
	}

	// Execute command
	var err error
	switch command {
	case "run-tool":
		err = runTool(client, commandArgs)
	case "read-resource":
		err = readResource(client, commandArgs)
	case "ping":
		err = ping(client)
	case "list-resources":
		err = listResources(client)
	case "list-tools":
		err = listTools(client)
	case "list-prompts":
		err = listPrompts(client)
	case "ask-llm":
		err = askLLM(client, commandArgs)
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
    -token <token>   Bearer token for authentication
    -version         Show version information

Commands:
    run-tool <tool-name>         Run an MCP tool (with optional JSON input)
    read-resource <resource-uri> Read an MCP resource
    ping                         Ping the server
    list-resources               List available resources
    list-tools                   List available tools
    list-prompts                 List available prompts
    ask-llm [query]              Ask an LLM using MCP tools and resources
                                 (Interactive mode if no query provided)

Examples:
    # Ping the server
    ai-cli ping

    # Run a tool with JSON input from stdin
    echo '{"key": "value"}' | ai-cli run-tool set_config

    # Run a tool without input (uses empty JSON object)
    ai-cli run-tool some_tool

    # Read a resource (for listing/viewing data)
    ai-cli read-resource ai-workbench://users

    # List available tools
    ai-cli list-tools

    # Ask an LLM (requires ANTHROPIC_API_KEY or Ollama)
    ai-cli ask-llm "List all users in the system"

    # Interactive LLM conversation mode
    ai-cli ask-llm

    # Use a different server
    ai-cli -server http://example.com:9000 ping

Environment Variables:
    ANTHROPIC_API_KEY    API key for Anthropic Claude (preferred if set)
    ANTHROPIC_MODEL      Model to use (default: claude-sonnet-4-5)
    OLLAMA_URL           Ollama server URL (default: http://localhost:11434)
    OLLAMA_MODEL         Ollama model to use (default: llama2)

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
	stat, _ := os.Stdin.Stat() //nolint:errcheck // Stat failure treated as character device
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Data is being piped
		decoder := json.NewDecoder(os.Stdin)
		if err := decoder.Decode(&inputData); err != nil {
			return fmt.Errorf("failed to parse JSON input: %w", err)
		}
	} else {
		// No piped data, use empty JSON object
		inputData = make(map[string]interface{})
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

func listResources(client *MCPClient) error {
	result, err := client.ListResources()
	if err != nil {
		return fmt.Errorf("failed to list resources: %w", err)
	}

	// Parse the result
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected response format")
	}

	resources, ok := resultMap["resources"].([]interface{})
	if !ok {
		return fmt.Errorf("no resources found in response")
	}

	// Print resources in plain text
	for _, r := range resources {
		resource, ok := r.(map[string]interface{})
		if !ok {
			continue
		}

		uri, _ := resource["uri"].(string)               //nolint:errcheck // Optional field, empty string is acceptable default
		name, _ := resource["name"].(string)             //nolint:errcheck // Optional field, empty string is acceptable default
		description, _ := resource["description"].(string) //nolint:errcheck // Optional field, empty string is acceptable default

		if uri != "" {
			fmt.Printf("%s", uri)
			if name != "" {
				fmt.Printf(" (%s)", name)
			}
			if description != "" {
				fmt.Printf(" - %s", description)
			}
			fmt.Println()
		}
	}

	return nil
}

func listTools(client *MCPClient) error {
	result, err := client.ListTools()
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// Parse the result
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected response format")
	}

	tools, ok := resultMap["tools"].([]interface{})
	if !ok {
		return fmt.Errorf("no tools found in response")
	}

	// Print tools in plain text
	for _, t := range tools {
		tool, ok := t.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := tool["name"].(string)               //nolint:errcheck // Optional field, empty string is acceptable default
		description, _ := tool["description"].(string) //nolint:errcheck // Optional field, empty string is acceptable default

		if name != "" {
			fmt.Printf("%s", name)
			if description != "" {
				fmt.Printf(" - %s", description)
			}
			fmt.Println()
		}
	}

	return nil
}

func listPrompts(client *MCPClient) error {
	result, err := client.ListPrompts()
	if err != nil {
		return fmt.Errorf("failed to list prompts: %w", err)
	}

	// Parse the result
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected response format")
	}

	prompts, ok := resultMap["prompts"].([]interface{})
	if !ok {
		return fmt.Errorf("no prompts found in response")
	}

	// Print prompts in plain text
	for _, p := range prompts {
		prompt, ok := p.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := prompt["name"].(string)               //nolint:errcheck // Optional field, empty string is acceptable default
		description, _ := prompt["description"].(string) //nolint:errcheck // Optional field, empty string is acceptable default

		if name != "" {
			fmt.Printf("%s", name)
			if description != "" {
				fmt.Printf(" - %s", description)
			}
			fmt.Println()
		}
	}

	return nil
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

// spinner displays a rotating cursor animation while waiting
type spinner struct {
	message string
	frames  []string
	active  bool
	mu      sync.Mutex
	done    chan bool
}

// newSpinner creates a new spinner with the given message
func newSpinner(message string) *spinner {
	return &spinner{
		message: message,
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		done:    make(chan bool),
	}
}

// start begins the spinner animation
func (s *spinner) start() {
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()

	go func() {
		frameIndex := 0
		for {
			select {
			case <-s.done:
				// Clear the spinner line
				fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", len(s.message)+10))
				return
			default:
				s.mu.Lock()
				if s.active {
					fmt.Fprintf(os.Stderr, "\r%s %s", s.frames[frameIndex], s.message)
					frameIndex = (frameIndex + 1) % len(s.frames)
				}
				s.mu.Unlock()
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

// stop stops the spinner animation
func (s *spinner) stop() {
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
	s.done <- true
}

// needsAuth determines if a command requires authentication
func needsAuth(command string) bool {
	// These commands do not require authentication
	exemptCommands := map[string]bool{
		"ping": true,
	}
	return !exemptCommands[command]
}

// handleAuthentication handles the authentication flow
func handleAuthentication(client *MCPClient, token string) error {
	// If token was provided via flag, use it
	if token != "" {
		client.SetBearerToken(token)
		return nil
	}

	// Try to read token from file
	homeDir, err := os.UserHomeDir()
	if err == nil {
		tokenFile := filepath.Join(homeDir, ".pgedge-ai-workbench-token")
		fileToken, err := readTokenFromFile(tokenFile)
		if err == nil && fileToken != "" {
			client.SetBearerToken(fileToken)
			return nil
		}
	}

	// No token found, prompt for username and password
	username, password, err := promptForCredentials()
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}

	// Authenticate and get session token
	sessionToken, err := authenticateUser(client, username, password)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Set the session token for subsequent requests
	client.SetBearerToken(sessionToken)
	return nil
}

// readTokenFromFile reads a token from a file
func readTokenFromFile(filename string) (string, error) {
	// #nosec G304 -- CLI intentionally reads user-specified file paths
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// promptForCredentials prompts the user for username and password
func promptForCredentials() (string, string, error) {
	reader := bufio.NewReader(os.Stdin)

	// Prompt for username
	fmt.Fprint(os.Stderr, "Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("failed to read username: %w", err)
	}
	username = strings.TrimSpace(username)

	// Prompt for password (hidden)
	fmt.Fprint(os.Stderr, "Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", "", fmt.Errorf("failed to read password: %w", err)
	}
	fmt.Fprintln(os.Stderr) // Print newline after password input
	password := string(passwordBytes)

	return username, password, nil
}

// authenticateUser calls the authenticate_user tool to get a session token
func authenticateUser(client *MCPClient, username, password string) (string, error) {
	arguments := map[string]interface{}{
		"username": username,
		"password": password,
	}

	result, err := client.CallTool("authenticate_user", arguments)
	if err != nil {
		return "", err
	}

	// Parse the result to extract the session token
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response format")
	}

	content, ok := resultMap["content"].([]interface{})
	if !ok || len(content) == 0 {
		return "", fmt.Errorf("no content in response")
	}

	firstContent, ok := content[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected content format")
	}

	text, ok := firstContent["text"].(string)
	if !ok {
		return "", fmt.Errorf("no text in response")
	}

	// Extract session token from text
	// Text format: "Authentication successful. Session token: <token>\nExpires at: <timestamp>"
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Authentication successful. Session token: ") {
			token := strings.TrimPrefix(line, "Authentication successful. Session token: ")
			return token, nil
		}
	}

	return "", fmt.Errorf("session token not found in response")
}

// askLLM sends a query to an LLM with access to MCP tools and resources
func askLLM(client *MCPClient, args []string) error {
	// Create LLM configuration
	llmConfig := NewLLMConfig()

	// Create LLM client
	llmClient, err := NewLLMClient(llmConfig)
	if err != nil {
		return fmt.Errorf("failed to create LLM client: %w\n\nMake sure you have either:\n- ANTHROPIC_API_KEY environment variable set, or\n- Ollama running at %s", err, llmConfig.OllamaURL)
	}

	fmt.Fprintf(os.Stderr, "Using %s LLM...\n\n", llmConfig.Provider)

	// Get available tools from MCP server
	toolsResult, err := client.ListTools()
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// Parse tools
	var tools []Tool
	if toolsMap, ok := toolsResult.(map[string]interface{}); ok {
		if toolsList, ok := toolsMap["tools"].([]interface{}); ok {
			for _, t := range toolsList {
				if toolMap, ok := t.(map[string]interface{}); ok {
					name, _ := toolMap["name"].(string)                           //nolint:errcheck // Type assertion, optional field
					description, _ := toolMap["description"].(string)             //nolint:errcheck // Type assertion, optional field
					inputSchema, _ := toolMap["inputSchema"].(map[string]interface{}) //nolint:errcheck // Type assertion, optional field

					if name != "" {
						tools = append(tools, Tool{
							Name:        name,
							Description: description,
							InputSchema: inputSchema,
						})
					}
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Loaded %d MCP tools\n\n", len(tools))

	// Get available resources from MCP server
	resourcesResult, err := client.ListResources()
	if err != nil {
		return fmt.Errorf("failed to list resources: %w", err)
	}

	// Parse resources
	var resources []Resource
	if resourcesMap, ok := resourcesResult.(map[string]interface{}); ok {
		if resourcesList, ok := resourcesMap["resources"].([]interface{}); ok {
			for _, r := range resourcesList {
				if resourceMap, ok := r.(map[string]interface{}); ok {
					uri, _ := resourceMap["uri"].(string)               //nolint:errcheck // Type assertion, optional field
					name, _ := resourceMap["name"].(string)             //nolint:errcheck // Type assertion, optional field
					description, _ := resourceMap["description"].(string) //nolint:errcheck // Type assertion, optional field
					mimeType, _ := resourceMap["mimeType"].(string)     //nolint:errcheck // Type assertion, optional field

					if uri != "" {
						resource := Resource{
							URI:         uri,
							Name:        name,
							Description: description,
							MimeType:    mimeType,
						}

						// Fetch data for static resources (no parameters in URI)
						if !strings.Contains(uri, "{") {
							resourceData, err := client.ReadResource(uri)
							if err == nil {
								resource.Data = resourceData
							}
						}

						resources = append(resources, resource)
					}
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Loaded %d MCP resources\n\n", len(resources))

	// Initialize conversation history
	var messages []Message
	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)

	// Check if initial query was provided
	var initialQuery string
	if len(args) > 0 {
		initialQuery = strings.Join(args, " ")
	}

	// If no initial query, enter interactive mode immediately
	if initialQuery == "" {
		fmt.Fprintf(os.Stderr, "Entering interactive mode. Press Ctrl+C to exit.\n\n")
	}

	// Interactive conversation loop
	for {
		var query string

		// Get the query (from args or prompt)
		if initialQuery != "" {
			query = initialQuery
			initialQuery = "" // Clear after first use
		} else {
			fmt.Fprint(os.Stderr, "You: ")
			input, err := reader.ReadString('\n')
			if err != nil {
				// Ctrl+C or EOF
				fmt.Fprintln(os.Stderr)
				return nil
			}
			query = strings.TrimSpace(input)
			if query == "" {
				continue
			}
		}

		// Add user message to history
		messages = append(messages, Message{
			Role:    "user",
			Content: query,
		})

		// Start spinner while waiting for LLM
		spin := newSpinner("Thinking...")
		spin.start()

		// Send to LLM
		response, err := llmClient.Chat(ctx, messages, tools, resources)

		// Stop spinner
		spin.stop()

		if err != nil {
			return fmt.Errorf("failed to chat with LLM: %w", err)
		}

		// Add assistant response to history
		messages = append(messages, Message{
			Role:    "assistant",
			Content: response,
		})

		// Print response
		fmt.Println()
		fmt.Println(response)
		fmt.Println()

		// Show prompt hint for interactive mode
		if len(args) == 0 || len(messages) > 2 {
			fmt.Fprintf(os.Stderr, "(Press Ctrl+C to exit)\n\n")
		}
	}
}
