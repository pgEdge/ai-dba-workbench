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
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// addTokenCommand handles the add-token command
func addTokenCommand(dataDir, annotation string, expiresIn time.Duration, isSuperuser bool) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	reader := bufio.NewReader(os.Stdin)

	// Prompt for annotation if not provided
	if annotation == "" {
		fmt.Print("Enter notes for this token (optional): ")
		if input, err := reader.ReadString('\n'); err == nil {
			annotation = strings.TrimSpace(input)
		}
	}

	// Calculate expiry
	var expiresAt *time.Time
	if expiresIn > 0 {
		expiry := time.Now().Add(expiresIn)
		expiresAt = &expiry
	} else if expiresIn == 0 {
		// Prompt for expiry
		fmt.Print("Enter expiry duration (e.g., '30d', '1y', or 'never'): ")
		input := ""
		if userInput, err := reader.ReadString('\n'); err == nil {
			input = strings.TrimSpace(userInput)
		}

		if input != "" && input != "never" {
			duration, err := parseDuration(input)
			if err != nil {
				return fmt.Errorf("invalid duration: %w", err)
			}
			expiry := time.Now().Add(duration)
			expiresAt = &expiry
		}
	}

	// Create service token (empty database = uses configured single database)
	rawToken, storedToken, err := store.CreateServiceToken(annotation, expiresAt, "", isSuperuser)
	if err != nil {
		return fmt.Errorf("failed to create token: %w", err)
	}

	// Display results
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("Token created successfully!")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("\nToken: %s\n", rawToken)
	fmt.Printf("Hash:  %s...\n", storedToken.TokenHash[:16])
	fmt.Printf("ID:    %d\n", storedToken.ID)
	if annotation != "" {
		fmt.Printf("Note:  %s\n", annotation)
	}
	if isSuperuser {
		fmt.Println("Superuser: Yes (bypasses all privilege checks)")
	}
	if expiresAt != nil {
		fmt.Printf("Expires: %s\n", expiresAt.Format(time.RFC3339))
	} else {
		fmt.Println("Expires: Never")
	}
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("\nIMPORTANT: Save this token securely - it will not be shown again!")
	fmt.Println("Use it in API requests with: Authorization: Bearer <token>")
	fmt.Println(strings.Repeat("=", 70) + "\n")

	return nil
}

// removeTokenCommand handles the remove-token command
func removeTokenCommand(dataDir, identifier string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Remove token
	if err := store.DeleteServiceToken(identifier); err != nil {
		return fmt.Errorf("failed to remove token: %w", err)
	}

	fmt.Printf("Token removed successfully: %s\n", identifier)
	return nil
}

// listTokensCommand handles the list-tokens command
func listTokensCommand(dataDir string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	tokens, err := store.ListServiceTokens()
	if err != nil {
		return fmt.Errorf("failed to list tokens: %w", err)
	}

	if len(tokens) == 0 {
		fmt.Println("No service tokens found.")
		return nil
	}

	fmt.Println("\nService Tokens:")
	fmt.Println(strings.Repeat("=", 100))
	fmt.Printf("%-6s %-18s %-20s %-10s %-10s %s\n", "ID", "Hash Prefix", "Expires", "Status", "Superuser", "Notes")
	fmt.Println(strings.Repeat("-", 100))

	now := time.Now()
	for _, token := range tokens {
		status := "Active"
		if token.ExpiresAt != nil && token.ExpiresAt.Before(now) {
			status = "EXPIRED"
		}

		expiryStr := "Never"
		if token.ExpiresAt != nil {
			expiryStr = token.ExpiresAt.Format("2006-01-02 15:04")
		}

		superuserStr := "No"
		if token.IsSuperuser {
			superuserStr = "Yes"
		}

		annotation := token.Annotation
		if len(annotation) > 20 {
			annotation = annotation[:17] + "..."
		}

		hashPrefix := token.TokenHash
		if len(hashPrefix) > 16 {
			hashPrefix = hashPrefix[:16]
		}

		fmt.Printf("%-6d %-18s %-20s %-10s %-10s %s\n",
			token.ID,
			hashPrefix,
			expiryStr,
			status,
			superuserStr,
			annotation)
	}
	fmt.Println(strings.Repeat("=", 100) + "\n")

	return nil
}

// parseDuration parses durations like "30d", "1y", "2w", "12h"
func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration format")
	}

	// Get the numeric part and unit
	numStr := s[:len(s)-1]
	unit := s[len(s)-1]

	var num int
	if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
		return 0, fmt.Errorf("invalid number in duration: %w", err)
	}

	switch unit {
	case 'h':
		return time.Duration(num) * time.Hour, nil
	case 'd':
		return time.Duration(num) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	case 'm':
		return time.Duration(num) * 30 * 24 * time.Hour, nil
	case 'y':
		return time.Duration(num) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration unit: %c (use h, d, w, m, or y)", unit)
	}
}
