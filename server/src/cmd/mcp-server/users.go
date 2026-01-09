/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/pgedge/ai-workbench/server/internal/auth"

	"golang.org/x/term"
)

// addUserCommand handles the add-user command
func addUserCommand(dataDir, username, password, annotation string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Prompt for username if not provided
	if username == "" {
		fmt.Print("Enter username: ")
		reader := bufio.NewReader(os.Stdin)
		if input, err := reader.ReadString('\n'); err == nil {
			username = strings.TrimSpace(input)
		}
		if username == "" {
			return fmt.Errorf("username is required")
		}
	}

	// Prompt for password if not provided (securely without echo)
	if password == "" {
		fmt.Print("Enter password: ")
		passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println() // New line after password input
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		password = string(passwordBytes)

		if password == "" {
			return fmt.Errorf("password is required")
		}

		// Confirm password
		fmt.Print("Confirm password: ")
		confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println() // New line after password input
		if err != nil {
			return fmt.Errorf("failed to read password confirmation: %w", err)
		}

		if password != string(confirmBytes) {
			return fmt.Errorf("passwords do not match")
		}
	}

	// Prompt for annotation if not provided
	if annotation == "" {
		fmt.Print("Enter annotation/note for this user (optional): ")
		reader := bufio.NewReader(os.Stdin)
		if input, err := reader.ReadString('\n'); err == nil {
			annotation = strings.TrimSpace(input)
		}
	}

	// Add user to store
	if err := store.CreateUser(username, password, annotation); err != nil {
		return fmt.Errorf("failed to add user: %w", err)
	}

	// Display results
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("User created successfully!")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("\nUsername: %s\n", username)
	if annotation != "" {
		fmt.Printf("Note:     %s\n", annotation)
	}
	fmt.Printf("Status:   Enabled\n")
	fmt.Println(strings.Repeat("=", 70) + "\n")

	return nil
}

// updateUserCommand handles the update-user command
func updateUserCommand(dataDir, username, newPassword, newAnnotation string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Prompt for username if not provided
	if username == "" {
		fmt.Print("Enter username: ")
		reader := bufio.NewReader(os.Stdin)
		if input, err := reader.ReadString('\n'); err == nil {
			username = strings.TrimSpace(input)
		}
		if username == "" {
			return fmt.Errorf("username is required")
		}
	}

	// Check user exists
	user, err := store.GetUser(username)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user '%s' not found", username)
	}

	// If neither password nor annotation provided, prompt for what to update
	if newPassword == "" && newAnnotation == "" {
		fmt.Println("What would you like to update?")
		fmt.Print("Update password? (y/N): ")
		reader := bufio.NewReader(os.Stdin)
		if input, err := reader.ReadString('\n'); err == nil {
			response := strings.TrimSpace(strings.ToLower(input))
			if response == "y" || response == "yes" {
				fmt.Print("Enter new password: ")
				passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
				fmt.Println() // New line after password input
				if err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}
				newPassword = string(passwordBytes)

				if newPassword != "" {
					// Confirm password
					fmt.Print("Confirm new password: ")
					confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
					fmt.Println() // New line after password input
					if err != nil {
						return fmt.Errorf("failed to read password confirmation: %w", err)
					}

					if newPassword != string(confirmBytes) {
						return fmt.Errorf("passwords do not match")
					}
				}
			}
		}

		fmt.Print("Update annotation? (y/N): ")
		if input, err := reader.ReadString('\n'); err == nil {
			response := strings.TrimSpace(strings.ToLower(input))
			if response == "y" || response == "yes" {
				fmt.Print("Enter new annotation (leave empty to clear): ")
				if input, err := reader.ReadString('\n'); err == nil {
					newAnnotation = strings.TrimSpace(input)
				}
			}
		}

		if newPassword == "" && newAnnotation == "" {
			return fmt.Errorf("no updates specified")
		}
	}

	// Update user
	if err := store.UpdateUser(username, newPassword, newAnnotation); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	fmt.Printf("User '%s' updated successfully\n", username)
	return nil
}

// deleteUserCommand handles the delete-user command
func deleteUserCommand(dataDir, username string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Prompt for username if not provided
	if username == "" {
		fmt.Print("Enter username to delete: ")
		reader := bufio.NewReader(os.Stdin)
		if input, err := reader.ReadString('\n'); err == nil {
			username = strings.TrimSpace(input)
		}
		if username == "" {
			return fmt.Errorf("username is required")
		}
	}

	// Confirm deletion
	fmt.Printf("Are you sure you want to delete user '%s'? (y/N): ", username)
	reader := bufio.NewReader(os.Stdin)
	if input, err := reader.ReadString('\n'); err == nil {
		response := strings.TrimSpace(strings.ToLower(input))
		if response != "y" && response != "yes" {
			fmt.Println("Deletion canceled")
			return nil
		}
	}

	// Remove user
	if err := store.DeleteUser(username); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	fmt.Printf("User '%s' deleted successfully\n", username)
	return nil
}

// listUsersCommand handles the list-users command
func listUsersCommand(dataDir string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	users, err := store.ListUsers()
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	if len(users) == 0 {
		fmt.Println("No users found.")
		return nil
	}

	fmt.Println("\nUsers:")
	fmt.Println(strings.Repeat("=", 90))
	fmt.Printf("%-20s %-25s %-20s %-10s %s\n", "Username", "Created", "Last Login", "Status", "Annotation")
	fmt.Println(strings.Repeat("-", 90))

	for _, user := range users {
		status := "Enabled"
		if !user.Enabled {
			status := "DISABLED"
			if user.FailedAttempts > 0 {
				status = fmt.Sprintf("DISABLED (%d fails)", user.FailedAttempts)
			}
			_ = status
		}

		lastLogin := "Never"
		if user.LastLogin != nil {
			lastLogin = user.LastLogin.Format("2006-01-02 15:04")
		}

		created := user.CreatedAt.Format("2006-01-02 15:04")

		annotation := user.Annotation
		if len(annotation) > 20 {
			annotation = annotation[:17] + "..."
		}

		statusDisplay := status
		if !user.Enabled {
			statusDisplay = "DISABLED"
		}

		fmt.Printf("%-20s %-25s %-20s %-10s %s\n",
			user.Username,
			created,
			lastLogin,
			statusDisplay,
			annotation)
	}
	fmt.Println(strings.Repeat("=", 90) + "\n")

	return nil
}

// enableUserCommand handles the enable-user command
func enableUserCommand(dataDir, username string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Prompt for username if not provided
	if username == "" {
		fmt.Print("Enter username to enable: ")
		reader := bufio.NewReader(os.Stdin)
		if input, err := reader.ReadString('\n'); err == nil {
			username = strings.TrimSpace(input)
		}
		if username == "" {
			return fmt.Errorf("username is required")
		}
	}

	// Enable user (also resets failed attempts)
	if err := store.EnableUser(username); err != nil {
		return fmt.Errorf("failed to enable user: %w", err)
	}

	// Reset failed attempts
	if err := store.ResetFailedAttempts(username); err != nil {
		return fmt.Errorf("failed to reset failed attempts: %w", err)
	}

	fmt.Printf("User '%s' enabled successfully (failed attempts reset)\n", username)
	return nil
}

// disableUserCommand handles the disable-user command
func disableUserCommand(dataDir, username string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Prompt for username if not provided
	if username == "" {
		fmt.Print("Enter username to disable: ")
		reader := bufio.NewReader(os.Stdin)
		if input, err := reader.ReadString('\n'); err == nil {
			username = strings.TrimSpace(input)
		}
		if username == "" {
			return fmt.Errorf("username is required")
		}
	}

	// Disable user
	if err := store.DisableUser(username); err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	fmt.Printf("User '%s' disabled successfully\n", username)
	return nil
}
