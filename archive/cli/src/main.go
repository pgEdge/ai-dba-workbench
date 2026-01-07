/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package main is the entry point for the AI DBA Workbench CLI
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

	// ANSI color codes
	colorReset = "\033[0m"
	colorRed   = "\033[31m"
	colorBlue  = "\033[34m"
)

// colorize wraps text in ANSI color codes
func colorize(color, text string) string {
	return color + text + colorReset
}

// red returns text in red (for notices/warnings)
func red(text string) string {
	return colorize(colorRed, text)
}

// blue returns text in blue (for user input)
func blue(text string) string {
	return colorize(colorBlue, text)
}

// getEffectiveServerURL determines the effective server URL based on priority
// Priority: --server flag > AI_CLI_SERVER_URL env > config file > default
func getEffectiveServerURL(flagValue string) string {
	// Command-line flag has highest priority
	if flagValue != "" {
		return flagValue
	}

	// Check AI_CLI_SERVER_URL environment variable
	if envURL := os.Getenv("AI_CLI_SERVER_URL"); envURL != "" {
		return envURL
	}

	// Check config file
	config, err := loadCLIConfig()
	if err == nil && config.ServerURL != "" {
		return config.ServerURL
	}

	// Default
	return "http://localhost:8080"
}

func main() {
	// Define command-line flags
	serverURLFlag := flag.String("server", "", "MCP server URL")
	token := flag.String("token", "", "Bearer token for authentication")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("AI DBA Workbench CLI v%s\n", version)
		os.Exit(0)
	}

	// Get command and arguments
	args := flag.Args()
	if len(args) < 1 {
		// Enter shell mode when no arguments provided
		serverURL := getEffectiveServerURL(*serverURLFlag)
		client := NewMCPClient(serverURL)

		// Try to handle authentication, but allow shell mode to continue if it fails
		// (some shell commands don't need auth)
		if *token != "" || needsAuthForShellMode() {
			if err := handleAuthentication(client, *token); err != nil {
				fmt.Fprint(os.Stderr, red(fmt.Sprintf("Warning: Authentication failed: %v\n", err)))
				fmt.Fprint(os.Stderr, red("Some commands may not work without authentication.\n\n"))
			}
		}

		if err := runShellMode(client); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	command := args[0]
	var commandArgs []string
	if len(args) > 1 {
		commandArgs = args[1:]
	}

	// Determine effective server URL
	// Priority: --server flag > AI_CLI_SERVER_URL env > config file > default
	serverURL := getEffectiveServerURL(*serverURLFlag)

	// Create MCP client
	client := NewMCPClient(serverURL)

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
	case "set-llm":
		err = setLLM(commandArgs)
	case "show-llm":
		err = showLLM()
	case "set-model":
		err = setModel(commandArgs)
	case "show-model":
		err = showModel()
	case "set-server":
		err = setServerURL(commandArgs)
	case "set-anthropic-key":
		err = setAnthropicKey(commandArgs)
	case "set-ollama-url":
		err = setOllamaURL(commandArgs)
	case "show-config":
		err = showConfig()
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
	fmt.Fprintf(os.Stderr, `AI DBA Workbench CLI - MCP Server Interaction Tool

Usage:
    ai-cli [options] <command> [arguments]
    ai-cli [options]                        (Enter interactive shell mode)

Options:
    -server <url>    MCP server URL (default: http://localhost:8080)
    -token <token>   Bearer token for authentication
    -version         Show version information

Environment Variables (AI_CLI_* override config file):
    AI_CLI_SERVER_URL           MCP server URL
    AI_CLI_ANTHROPIC_API_KEY    Anthropic API key
    AI_CLI_ANTHROPIC_MODEL      Anthropic model name
    AI_CLI_OLLAMA_URL           Ollama server URL
    AI_CLI_OLLAMA_MODEL         Ollama model name

Configuration File:
    ~/.ai-workbench-cli.json    Stores server URL, API keys, and preferences

Commands:
    run-tool <tool-name>         Run an MCP tool (with optional JSON input)
    read-resource <resource-uri> Read an MCP resource
    ping                         Ping the server
    list-resources               List available resources
    list-tools                   List available tools
    list-prompts                 List available prompts
    ask-llm [query]              Ask an LLM using MCP tools and resources
                                 (Interactive mode if no query provided)

Configuration Commands:
    set-server <url>             Set AI DBA Workbench MCP server URL
    set-llm <provider>           Set LLM provider (anthropic or ollama)
    show-llm                     Show current LLM provider
    set-model <model-name>       Set model name for current LLM provider
    show-model                   Show model name for current LLM provider
    set-anthropic-key <key>      Set Anthropic API key
    set-ollama-url <url>         Set Ollama server URL
    show-config                  Show all configuration settings

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

    # Ask an LLM (requires AI_CLI_ANTHROPIC_API_KEY or Ollama)
    ai-cli ask-llm "List all users in the system"

    # Interactive LLM conversation mode
    ai-cli ask-llm

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

		uri, _ := resource["uri"].(string)                 //nolint:errcheck // Optional field, empty string is acceptable default
		name, _ := resource["name"].(string)               //nolint:errcheck // Optional field, empty string is acceptable default
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
	words      []string
	frames     []string
	active     bool
	mu         sync.Mutex
	done       chan bool
	maxWordLen int
}

// newSpinner creates a new spinner with fun PostgreSQL-themed words
func newSpinner() *spinner {
	words := []string{
		"Postgressing",
		"Sloniking",
		"Herding",
		"Mahouting",
		"SELECTing",
		"Aggregating",
		"Elephanting",
		"Querying",
		"Schemaing",
	}

	// Find the longest word for clearing
	maxLen := 0
	for _, word := range words {
		if len(word) > maxLen {
			maxLen = len(word)
		}
	}

	return &spinner{
		words:      words,
		frames:     []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		done:       make(chan bool),
		maxWordLen: maxLen,
	}
}

// start begins the spinner animation
func (s *spinner) start() {
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()

	go func() {
		frameIndex := 0
		wordIndex := 0
		frameCount := 0
		framesPerWord := 25 // Change word every ~2 seconds (25 frames * 80ms)

		for {
			select {
			case <-s.done:
				// Clear the spinner line with enough space for longest word
				fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", s.maxWordLen+10))
				return
			default:
				s.mu.Lock()
				if s.active {
					currentWord := s.words[wordIndex]
					fmt.Fprintf(os.Stderr, "\r\033[31m%s %s...\033[0m", s.frames[frameIndex], currentWord)
					frameIndex = (frameIndex + 1) % len(s.frames)
					frameCount++

					// Change word every ~2 seconds
					if frameCount >= framesPerWord {
						frameCount = 0
						wordIndex = (wordIndex + 1) % len(s.words)
						// Clear the line before changing word
						fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", s.maxWordLen+10))
					}
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
		"ping":              true,
		"set-llm":           true,
		"show-llm":          true,
		"set-model":         true,
		"show-model":        true,
		"set-server":        true,
		"set-anthropic-key": true,
		"set-ollama-url":    true,
		"show-config":       true,
	}
	return !exemptCommands[command]
}

// needsAuthForShellMode checks if we should attempt authentication for shell mode
func needsAuthForShellMode() bool {
	// Check if there's a saved token
	homeDir, err := os.UserHomeDir()
	if err == nil {
		tokenFile := filepath.Join(homeDir, ".pgedge-ai-workbench-token")
		if _, err := os.Stat(tokenFile); err == nil {
			return true
		}
	}
	return false
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
	// Load CLI configuration
	cliConfig, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load CLI config: %w", err)
	}

	// Create LLM configuration
	llmConfig := NewLLMConfig()

	// Apply CLI config preferences
	applyConfigToLLMConfig(cliConfig, llmConfig)

	// Create LLM client
	llmClient, err := NewLLMClient(llmConfig)
	if err != nil {
		return fmt.Errorf("failed to create LLM client: %w\n\nMake sure you have either:\n- AI_CLI_ANTHROPIC_API_KEY environment variable set, or\n- Ollama running at %s", err, llmConfig.OllamaURL)
	}

	fmt.Fprintf(os.Stderr, red("Using %s LLM with model %s...\n\n"), llmConfig.Provider, getLLMModelName(llmConfig))

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
					name, _ := toolMap["name"].(string)                               //nolint:errcheck // Type assertion, optional field
					description, _ := toolMap["description"].(string)                 //nolint:errcheck // Type assertion, optional field
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
					uri, _ := resourceMap["uri"].(string)                 //nolint:errcheck // Type assertion, optional field
					name, _ := resourceMap["name"].(string)               //nolint:errcheck // Type assertion, optional field
					description, _ := resourceMap["description"].(string) //nolint:errcheck // Type assertion, optional field
					mimeType, _ := resourceMap["mimeType"].(string)       //nolint:errcheck // Type assertion, optional field

					if uri != "" {
						// Only send resource metadata - don't fetch data upfront
						// The LLM can use read_resource tool to fetch data when needed
						resource := Resource{
							URI:         uri,
							Name:        name,
							Description: description,
							MimeType:    mimeType,
						}
						resources = append(resources, resource)
					}
				}
			}
		}
	}

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
		fmt.Fprint(os.Stderr, red("Entering interactive mode. Press Ctrl+C to exit.\n\n"))
	}

	// Interactive conversation loop
	for {
		var query string

		// Get the query (from args or prompt)
		if initialQuery != "" {
			query = initialQuery
			initialQuery = "" // Clear after first use
		} else {
			fmt.Fprint(os.Stderr, blue("You: "))
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
		spin := newSpinner()
		spin.start()

		// Send to LLM (with MCP client for tool execution)
		response, err := llmClient.Chat(ctx, messages, tools, resources, client)

		// Stop spinner
		spin.stop()

		if err != nil {
			return fmt.Errorf("failed to chat with LLM: %w", err)
		}

		// Check if response is empty
		if response == "" {
			fmt.Fprint(os.Stderr, red("Warning: LLM returned empty response\n"))
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
			fmt.Fprint(os.Stderr, red("(Press Ctrl+C to exit)\n\n"))
		}
	}
}

// getLLMModelName returns the model name for the current LLM provider
func getLLMModelName(config *LLMConfig) string {
	if config.Provider == "anthropic" {
		return config.AnthropicModel
	}
	return config.OllamaModel
}

// askLLMInteractiveWithReadline is the interactive LLM conversation mode with readline support
func askLLMInteractiveWithReadline(client *MCPClient, rl interface{}) error {
	// Load CLI configuration
	cliConfig, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load CLI config: %w", err)
	}

	// Create LLM configuration
	llmConfig := NewLLMConfig()

	// Apply CLI config preferences
	applyConfigToLLMConfig(cliConfig, llmConfig)

	// Create LLM client
	llmClient, err := NewLLMClient(llmConfig)
	if err != nil {
		return fmt.Errorf("failed to create LLM client: %w\n\nMake sure you have either:\n- AI_CLI_ANTHROPIC_API_KEY environment variable set, or\n- Ollama running at %s", err, llmConfig.OllamaURL)
	}

	fmt.Fprintf(os.Stderr, red("Using %s LLM with model %s...\n\n"), llmConfig.Provider, getLLMModelName(llmConfig))

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
					name, _ := toolMap["name"].(string)                               //nolint:errcheck // Type assertion, optional field
					description, _ := toolMap["description"].(string)                 //nolint:errcheck // Type assertion, optional field
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
					uri, _ := resourceMap["uri"].(string)                 //nolint:errcheck // Type assertion, optional field
					name, _ := resourceMap["name"].(string)               //nolint:errcheck // Type assertion, optional field
					description, _ := resourceMap["description"].(string) //nolint:errcheck // Type assertion, optional field
					mimeType, _ := resourceMap["mimeType"].(string)       //nolint:errcheck // Type assertion, optional field

					if uri != "" {
						// Only send resource metadata - don't fetch data upfront
						// The LLM can use read_resource tool to fetch data when needed
						resource := Resource{
							URI:         uri,
							Name:        name,
							Description: description,
							MimeType:    mimeType,
						}
						resources = append(resources, resource)
					}
				}
			}
		}
	}

	// Initialize conversation history
	var messages []Message
	ctx := context.Background()

	// Type assert rl to readline interface
	type readlineInterface interface {
		Readline() (string, error)
	}
	readline, ok := rl.(readlineInterface)
	if !ok {
		return fmt.Errorf("invalid readline instance")
	}

	// Interactive conversation loop
	for {
		// Get query from readline
		query, err := readline.Readline()
		if err != nil {
			// Ctrl+C or EOF
			fmt.Fprintln(os.Stderr)
			return nil
		}

		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}

		// Add user message to history
		messages = append(messages, Message{
			Role:    "user",
			Content: query,
		})

		// Start spinner while waiting for LLM
		spin := newSpinner()
		spin.start()

		// Send to LLM (with MCP client for tool execution)
		response, err := llmClient.Chat(ctx, messages, tools, resources, client)

		// Stop spinner
		spin.stop()

		if err != nil {
			fmt.Fprintf(os.Stderr, red("Error: %v\n"), err)
			continue
		}

		// Check if response is empty
		if response == "" {
			fmt.Fprint(os.Stderr, red("Warning: LLM returned empty response\n"))
			continue
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
	}
}

// setLLM sets the preferred LLM provider
func setLLM(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: set-llm <provider>\n\nAvailable providers: anthropic, ollama")
	}

	provider := strings.ToLower(args[0])

	// Validate provider
	if provider != "anthropic" && provider != "ollama" {
		return fmt.Errorf("invalid provider: %s\n\nAvailable providers: anthropic, ollama", args[0])
	}

	// Check if the provider is configured
	llmConfig := NewLLMConfig()

	// Load config to check what's available
	config, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	applyConfigToLLMConfig(config, llmConfig)

	if provider == "anthropic" {
		if llmConfig.AnthropicKey == "" {
			return fmt.Errorf("Anthropic API key is required. Set it using:\n  - AI_CLI_ANTHROPIC_API_KEY environment variable\n  - Config file: ./ai-cli set-anthropic-key <key>")
		}
	}
	// Ollama doesn't require configuration, it just needs to be running

	// Update provider
	config.PreferredLLM = provider

	// Save config
	if err := saveCLIConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("LLM provider set to: %s\n", provider)
	if provider == "anthropic" {
		fmt.Printf("Model: %s (use 'set-model' to change)\n", llmConfig.AnthropicModel)
	} else {
		fmt.Printf("Model: %s (use 'set-model' to change)\n", llmConfig.OllamaModel)
		fmt.Printf("URL: %s\n", llmConfig.OllamaURL)
		fmt.Printf("\nNote: For agentic tool execution, use models that support function calling\n")
		fmt.Printf("(e.g., llama3.1, llama3.2, mistral, mixtral, qwen2.5)\n")
	}

	return nil
}

// showLLM displays the current LLM provider
func showLLM() error {
	// Load current config
	config, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get effective LLM config
	llmConfig := NewLLMConfig()
	applyConfigToLLMConfig(config, llmConfig)

	if config.PreferredLLM == "" {
		fmt.Printf("LLM Provider: %s (auto-detected)\n", llmConfig.Provider)
	} else {
		fmt.Printf("LLM Provider: %s\n", llmConfig.Provider)
	}

	if llmConfig.Provider == "anthropic" {
		fmt.Printf("Model: %s\n", llmConfig.AnthropicModel)
		if llmConfig.AnthropicKey != "" {
			fmt.Printf("API Key: configured\n")
		} else {
			fmt.Printf("API Key: not configured\n")
		}
	} else {
		fmt.Printf("Model: %s\n", llmConfig.OllamaModel)
		fmt.Printf("URL: %s\n", llmConfig.OllamaURL)
	}

	return nil
}

// setModel sets the model name for the current LLM provider
func setModel(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: set-model <model-name>")
	}

	modelName := args[0]

	// Load current config
	config, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get effective LLM config to determine current provider
	llmConfig := NewLLMConfig()
	applyConfigToLLMConfig(config, llmConfig)

	// Update the appropriate model
	if llmConfig.Provider == "anthropic" {
		config.AnthropicModel = modelName
		fmt.Printf("Anthropic model set to: %s\n", modelName)
	} else {
		config.OllamaModel = modelName
		fmt.Printf("Ollama model set to: %s\n", modelName)
		fmt.Printf("\nNote: For agentic tool execution, use models that support function calling\n")
		fmt.Printf("(e.g., llama3.1, llama3.2, mistral, mixtral, qwen2.5)\n")
	}

	// Save config
	if err := saveCLIConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// showModel displays the model name for the current LLM provider
func showModel() error {
	// Load current config
	config, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get effective LLM config
	llmConfig := NewLLMConfig()
	applyConfigToLLMConfig(config, llmConfig)

	fmt.Printf("Current LLM Provider: %s\n", llmConfig.Provider)

	if llmConfig.Provider == "anthropic" {
		if config.AnthropicModel != "" {
			fmt.Printf("Anthropic Model: %s (configured)\n", llmConfig.AnthropicModel)
		} else {
			fmt.Printf("Anthropic Model: %s (default)\n", llmConfig.AnthropicModel)
		}
	} else {
		if config.OllamaModel != "" {
			fmt.Printf("Ollama Model: %s (configured)\n", llmConfig.OllamaModel)
		} else {
			fmt.Printf("Ollama Model: %s (default)\n", llmConfig.OllamaModel)
		}
	}

	return nil
}

// setServerURL sets the AI DBA Workbench MCP server URL in the config file
func setServerURL(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: set-server <url>")
	}

	serverURL := args[0]

	// Load current config
	config, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Update server URL
	config.ServerURL = serverURL

	// Save config
	if err := saveCLIConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("AI DBA Workbench MCP server URL set to: %s\n", serverURL)
	fmt.Printf("\nNote: This can be overridden with --server flag or AI_CLI_SERVER_URL environment variable\n")

	return nil
}

// setAnthropicKey sets the Anthropic API key in the config file
func setAnthropicKey(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: set-anthropic-key <api-key>")
	}

	apiKey := args[0]

	// Load current config
	config, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Update API key
	config.AnthropicAPIKey = apiKey

	// Save config
	if err := saveCLIConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Anthropic API key saved to config file\n")
	fmt.Printf("\nNote: This can be overridden with AI_CLI_ANTHROPIC_API_KEY environment variable\n")
	fmt.Printf("Warning: The API key is stored in plain text in ~/.ai-workbench-cli.json (0600 permissions)\n")

	return nil
}

// setOllamaURL sets the Ollama server URL in the config file
func setOllamaURL(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: set-ollama-url <url>")
	}

	ollamaURL := args[0]

	// Load current config
	config, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Update Ollama URL
	config.OllamaURL = ollamaURL

	// Save config
	if err := saveCLIConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Ollama server URL set to: %s\n", ollamaURL)
	fmt.Printf("\nNote: This can be overridden with AI_CLI_OLLAMA_URL environment variable\n")

	return nil
}

// showConfig displays all configuration settings
func showConfig() error {
	// Load current config
	config, err := loadCLIConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get effective LLM config
	llmConfig := NewLLMConfig()
	applyConfigToLLMConfig(config, llmConfig)

	fmt.Printf("Configuration File: ~/.ai-workbench-cli.json\n\n")

	// Server settings
	fmt.Printf("Server Settings:\n")
	effectiveServerURL := getEffectiveServerURL("")
	if config.ServerURL != "" {
		fmt.Printf("  Server URL: %s (configured)\n", config.ServerURL)
	} else {
		fmt.Printf("  Server URL: %s (default)\n", effectiveServerURL)
	}
	if os.Getenv("AI_CLI_SERVER_URL") != "" {
		fmt.Printf("  Server URL Override: %s (AI_CLI_SERVER_URL env var)\n", os.Getenv("AI_CLI_SERVER_URL"))
	}
	fmt.Println()

	// LLM provider
	fmt.Printf("LLM Provider:\n")
	if config.PreferredLLM != "" {
		fmt.Printf("  Provider: %s (configured)\n", config.PreferredLLM)
	} else {
		fmt.Printf("  Provider: %s (auto-detected)\n", llmConfig.Provider)
	}
	fmt.Println()

	// Anthropic settings
	fmt.Printf("Anthropic Settings:\n")
	if os.Getenv("AI_CLI_ANTHROPIC_API_KEY") != "" {
		fmt.Printf("  API Key: configured (AI_CLI_ANTHROPIC_API_KEY env var)\n")
	} else if config.AnthropicAPIKey != "" {
		fmt.Printf("  API Key: configured (config file)\n")
	} else {
		fmt.Printf("  API Key: not configured\n")
	}

	if os.Getenv("AI_CLI_ANTHROPIC_MODEL") != "" {
		fmt.Printf("  Model: %s (AI_CLI_ANTHROPIC_MODEL env var)\n", llmConfig.AnthropicModel)
	} else if config.AnthropicModel != "" {
		fmt.Printf("  Model: %s (configured)\n", llmConfig.AnthropicModel)
	} else {
		fmt.Printf("  Model: %s (default)\n", llmConfig.AnthropicModel)
	}
	fmt.Println()

	// Ollama settings
	fmt.Printf("Ollama Settings:\n")
	if os.Getenv("AI_CLI_OLLAMA_URL") != "" {
		fmt.Printf("  URL: %s (AI_CLI_OLLAMA_URL env var)\n", llmConfig.OllamaURL)
	} else if config.OllamaURL != "" {
		fmt.Printf("  URL: %s (configured)\n", llmConfig.OllamaURL)
	} else {
		fmt.Printf("  URL: %s (default)\n", llmConfig.OllamaURL)
	}

	if os.Getenv("AI_CLI_OLLAMA_MODEL") != "" {
		fmt.Printf("  Model: %s (AI_CLI_OLLAMA_MODEL env var)\n", llmConfig.OllamaModel)
	} else if config.OllamaModel != "" {
		fmt.Printf("  Model: %s (configured)\n", llmConfig.OllamaModel)
	} else {
		fmt.Printf("  Model: %s (default)\n", llmConfig.OllamaModel)
	}
	fmt.Println()

	fmt.Printf("Priority Order:\n")
	fmt.Printf("  1. Command-line flags (highest)\n")
	fmt.Printf("  2. AI_CLI_* environment variables\n")
	fmt.Printf("  3. Configuration file settings\n")
	fmt.Printf("  4. Built-in defaults (lowest)\n")

	return nil
}
