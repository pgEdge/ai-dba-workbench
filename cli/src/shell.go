/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chzyer/readline"
)

// getHistoryPath returns the path to a history file
func getHistoryPath(historyType string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, fmt.Sprintf(".ai-workbench-cli-%s-history", historyType)), nil
}

// runShellMode enters interactive shell mode
func runShellMode(client *MCPClient) error {
	// Set up shell command history
	shellHistoryPath, err := getHistoryPath("shell")
	if err != nil {
		return fmt.Errorf("failed to get shell history path: %w", err)
	}

	// Set up LLM query history
	llmHistoryPath, err := getHistoryPath("llm")
	if err != nil {
		return fmt.Errorf("failed to get LLM history path: %w", err)
	}

	// Create readline instance for shell commands
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          blue("ai-workbench> "),
		HistoryFile:     shellHistoryPath,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return fmt.Errorf("failed to create readline: %w", err)
	}
	defer rl.Close()

	fmt.Println(red("pgEdge AI Workbench CLI - Interactive Shell Mode"))
	fmt.Println(red("Type 'help' for available commands, 'quit' or 'exit' to leave"))
	fmt.Println()

	// Main shell loop
	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				continue
			} else if err == io.EOF {
				break
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse command and args
		parts := strings.Fields(line)
		command := parts[0]
		var args []string
		if len(parts) > 1 {
			args = parts[1:]
		}

		// Handle quit/exit
		if command == "quit" || command == "exit" {
			fmt.Println(red("Goodbye!"))
			break
		}

		// Handle help
		if command == "help" {
			printShellHelp()
			continue
		}

		// Execute command
		if err := executeShellCommand(client, command, args, llmHistoryPath); err != nil {
			fmt.Fprintf(os.Stderr, red("Error: %v\n"), err)
		}
	}

	return nil
}

// printShellHelp displays help for shell mode
func printShellHelp() {
	fmt.Println(red("Available commands:"))
	fmt.Println()
	fmt.Println("  " + blue("help") + "                    Show this help message")
	fmt.Println("  " + blue("quit, exit") + "              Exit the shell")
	fmt.Println()
	fmt.Println(red("MCP Tools and Resources:"))
	fmt.Println("  " + blue("run-tool <name>") + "         Execute an MCP tool (interactive)")
	fmt.Println("  " + blue("read-resource <uri>") + "     Read an MCP resource")
	fmt.Println("  " + blue("list-tools") + "              List all available MCP tools")
	fmt.Println("  " + blue("list-resources") + "          List all available MCP resources")
	fmt.Println("  " + blue("list-prompts") + "            List all available MCP prompts")
	fmt.Println()
	fmt.Println(red("LLM Integration:"))
	fmt.Println("  " + blue("ask-llm <query>") + "         Ask a question to the LLM")
	fmt.Println("  " + blue("ask") + "                    Enter interactive LLM mode")
	fmt.Println()
	fmt.Println(red("Configuration:"))
	fmt.Println("  " + blue("set-llm <provider>") + "      Set LLM provider (anthropic/ollama)")
	fmt.Println("  " + blue("show-llm") + "                Show current LLM provider")
	fmt.Println("  " + blue("set-model <name>") + "        Set model for current provider")
	fmt.Println("  " + blue("show-model") + "              Show current model")
	fmt.Println("  " + blue("set-server <url>") + "        Set MCP server URL")
	fmt.Println("  " + blue("show-config") + "             Show all configuration settings")
	fmt.Println()
	fmt.Println(red("Server Operations:"))
	fmt.Println("  " + blue("ping") + "                    Test server connectivity")
	fmt.Println()
}

// executeShellCommand executes a command in shell mode
func executeShellCommand(client *MCPClient, command string, args []string, llmHistoryPath string) error {
	switch command {
	case "run-tool":
		if len(args) < 1 {
			return fmt.Errorf("usage: run-tool <tool-name>")
		}
		return runToolInteractive(client, args[0])

	case "read-resource":
		return readResource(client, args)

	case "list-tools":
		return listTools(client)

	case "list-resources":
		return listResources(client)

	case "list-prompts":
		return listPrompts(client)

	case "ping":
		return ping(client)

	case "ask-llm", "ask":
		query := strings.Join(args, " ")
		if query == "" {
			// Enter interactive LLM mode with persistent history
			return runInteractiveLLMMode(client, llmHistoryPath)
		}
		return askLLM(client, args)

	case "set-llm":
		return setLLM(args)

	case "show-llm":
		return showLLM()

	case "set-model":
		return setModel(args)

	case "show-model":
		return showModel()

	case "set-server":
		return setServerURL(args)

	case "set-anthropic-key":
		return setAnthropicKey(args)

	case "set-ollama-url":
		return setOllamaURL(args)

	case "show-config":
		return showConfig()

	default:
		return fmt.Errorf("unknown command: %s (type 'help' for available commands)", command)
	}
}

// runToolInteractive prompts user for tool arguments and executes the tool
func runToolInteractive(client *MCPClient, toolName string) error {
	// First, get the tool schema by listing all tools
	tools, err := client.ListTools()
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// Find the requested tool
	var toolSchema map[string]interface{}
	toolsMap, ok := tools.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected tools format")
	}

	if toolsList, ok := toolsMap["tools"].([]interface{}); ok {
		for _, t := range toolsList {
			if tool, ok := t.(map[string]interface{}); ok {
				if name, ok := tool["name"].(string); ok && name == toolName {
					toolSchema = tool
					break
				}
			}
		}
	}

	if toolSchema == nil {
		return fmt.Errorf("tool not found: %s", toolName)
	}

	// Extract input schema
	var inputSchema map[string]interface{}
	if schema, ok := toolSchema["inputSchema"].(map[string]interface{}); ok {
		inputSchema = schema
	}

	// Prompt for arguments
	argsMap := make(map[string]interface{})

	if inputSchema != nil {
		if properties, ok := inputSchema["properties"].(map[string]interface{}); ok {
			// Get required fields
			requiredFields := make(map[string]bool)
			if required, ok := inputSchema["required"].([]interface{}); ok {
				for _, r := range required {
					if fieldName, ok := r.(string); ok {
						requiredFields[fieldName] = true
					}
				}
			}

			// Use bufio.Reader for better input handling
			reader := bufio.NewReader(os.Stdin)

			// Prompt for each property
			for propName, propSchema := range properties {
				propMap, ok := propSchema.(map[string]interface{})
				if !ok {
					continue
				}

				description := ""
				if desc, ok := propMap["description"].(string); ok {
					description = desc
				}

				propType := "string"
				if t, ok := propMap["type"].(string); ok {
					propType = t
				}

				isRequired := requiredFields[propName]
				requiredTag := ""
				if isRequired {
					requiredTag = red(" (required)")
				}

				fmt.Printf("\n%s%s:\n", blue(propName), requiredTag)
				if description != "" {
					fmt.Printf("  %s\n", description)
				}
				fmt.Printf("  Type: %s\n", propType)
				fmt.Print("  Value: ")

				input, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read input: %w", err)
				}
				input = strings.TrimSpace(input)

				if input == "" && isRequired {
					return fmt.Errorf("required field %s cannot be empty", propName)
				}

				if input != "" {
					// Convert to appropriate type
					switch propType {
					case "integer", "number":
						num, err := strconv.ParseFloat(input, 64)
						if err != nil {
							return fmt.Errorf("invalid %s value for %s: %s", propType, propName, input)
						}
						if propType == "integer" {
							argsMap[propName] = int(num)
						} else {
							argsMap[propName] = num
						}
					case "boolean":
						lowerInput := strings.ToLower(input)
						argsMap[propName] = lowerInput == "true" || lowerInput == "yes" || lowerInput == "1"
					default:
						argsMap[propName] = input
					}
				}
			}
		}
	}

	fmt.Println()

	// Execute the tool
	result, err := client.CallTool(toolName, argsMap)
	if err != nil {
		return err
	}

	// Print the result
	if err := printJSON(result); err != nil {
		return err
	}

	return nil
}

// runInteractiveLLMMode enters interactive LLM conversation mode with persistent history
func runInteractiveLLMMode(client *MCPClient, historyPath string) error {
	// Create readline instance for LLM queries
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          blue("You: "),
		HistoryFile:     historyPath,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return fmt.Errorf("failed to create readline: %w", err)
	}
	defer rl.Close()

	fmt.Println(red("Entering interactive LLM mode. Type your questions, Ctrl+C to return to shell."))
	fmt.Println()

	// Call the existing interactive LLM function with readline
	return askLLMInteractiveWithReadline(client, rl)
}
