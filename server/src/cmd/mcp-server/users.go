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
	"syscall"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// addUserCommand handles the add-user command
func addUserCommand(dataDir, username, password, annotation, fullName, email string) error {
	// Open auth store
	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	reader := bufio.NewReader(os.Stdin)

	// Prompt for username if not provided
	if username == "" {
		fmt.Print("Enter username: ")
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

	// Prompt for full name if not provided
	if fullName == "" {
		fmt.Print("Enter full name (optional): ")
		if input, err := reader.ReadString('\n'); err == nil {
			fullName = strings.TrimSpace(input)
		}
	}

	// Prompt for email if not provided
	if email == "" {
		fmt.Print("Enter email address (optional): ")
		if input, err := reader.ReadString('\n'); err == nil {
			email = strings.TrimSpace(input)
		}
	}

	// Prompt for notes if not provided
	if annotation == "" {
		fmt.Print("Enter notes for this user (optional): ")
		if input, err := reader.ReadString('\n'); err == nil {
			annotation = strings.TrimSpace(input)
		}
	}

	// Add user to store
	if err := cliStore(store).CreateUser(username, password, annotation, fullName, email); err != nil {
		return fmt.Errorf("failed to add user: %w", err)
	}

	// Display results
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("User created successfully!")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("\nUsername:  %s\n", username)
	if fullName != "" {
		fmt.Printf("Full Name: %s\n", fullName)
	}
	if email != "" {
		fmt.Printf("Email:    %s\n", email)
	}
	if annotation != "" {
		fmt.Printf("Notes:    %s\n", annotation)
	}
	fmt.Printf("Status:   Enabled\n")
	fmt.Println(strings.Repeat("=", 70) + "\n")

	return nil
}

// updateUserCommand handles the update-user command
func updateUserCommand(dataDir, username, newPassword, newAnnotation, newFullName, newEmail string) error {
	// Open auth store
	store, err := openAuthStoreCLI(dataDir)
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

	// Track whether any flags were provided
	hasUpdates := newPassword != "" || newAnnotation != "" || newFullName != "" || newEmail != ""

	// Use existing values as defaults
	annotation := user.Annotation
	fullName := user.DisplayName
	email := user.Email

	if newAnnotation != "" {
		annotation = newAnnotation
	}
	if newFullName != "" {
		fullName = newFullName
	}
	if newEmail != "" {
		email = newEmail
	}

	// If no flags were provided, prompt for what to update
	if !hasUpdates {
		reader := bufio.NewReader(os.Stdin)
		fmt.Println("What would you like to update?")
		fmt.Print("Update password? (y/N): ")
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
					hasUpdates = true
				}
			}
		}

		fmt.Print("Update full name? (y/N): ")
		if input, err := reader.ReadString('\n'); err == nil {
			response := strings.TrimSpace(strings.ToLower(input))
			if response == "y" || response == "yes" {
				fmt.Print("Enter new full name (leave empty to clear): ")
				if input, err := reader.ReadString('\n'); err == nil {
					fullName = strings.TrimSpace(input)
					hasUpdates = true
				}
			}
		}

		fmt.Print("Update email? (y/N): ")
		if input, err := reader.ReadString('\n'); err == nil {
			response := strings.TrimSpace(strings.ToLower(input))
			if response == "y" || response == "yes" {
				fmt.Print("Enter new email (leave empty to clear): ")
				if input, err := reader.ReadString('\n'); err == nil {
					email = strings.TrimSpace(input)
					hasUpdates = true
				}
			}
		}

		fmt.Print("Update notes? (y/N): ")
		if input, err := reader.ReadString('\n'); err == nil {
			response := strings.TrimSpace(strings.ToLower(input))
			if response == "y" || response == "yes" {
				fmt.Print("Enter new notes (leave empty to clear): ")
				if input, err := reader.ReadString('\n'); err == nil {
					annotation = strings.TrimSpace(input)
					hasUpdates = true
				}
			}
		}

		if !hasUpdates {
			return fmt.Errorf("no updates specified")
		}
	}

	// Update user
	if err := cliStore(store).UpdateUser(username, newPassword, annotation, fullName, email); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	fmt.Printf("User '%s' updated successfully\n", username)
	return nil
}

// deleteUserCommand handles the delete-user command
func deleteUserCommand(dataDir, username string) error {
	// Open auth store
	store, err := openAuthStoreCLI(dataDir)
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
	if err := cliStore(store).DeleteUser(username); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	fmt.Printf("User '%s' deleted successfully\n", username)
	return nil
}

// listUsersCommand handles the list-users command
func listUsersCommand(dataDir string) error {
	// Open auth store
	store, err := openAuthStoreCLI(dataDir)
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
	fmt.Println(strings.Repeat("=", listUsersRuleWidth))
	fmt.Printf(listUsersRowFormat,
		"Username", "Created", "Last Login", "Status", "Notes", listUsersAuthHeader)
	fmt.Println(strings.Repeat("-", listUsersRuleWidth))

	for _, user := range users {
		status := "Enabled"
		if !user.Enabled {
			status = "DISABLED"
			if user.FailedAttempts > 0 {
				status = fmt.Sprintf("DISABLED (%d fails)", user.FailedAttempts)
			}
		}

		lastLogin := "Never"
		if user.LastLogin != nil {
			lastLogin = user.LastLogin.Format("2006-01-02 15:04")
		}

		created := user.CreatedAt.Format("2006-01-02 15:04")

		// The annotation is free text and the issuer came from whoever ran
		// -link-oidc-user, so both are stripped of control characters
		// before they reach the terminal. The username needs no such
		// treatment, since auth.ValidateUsername admits none.
		fmt.Printf(listUsersRowFormat,
			user.Username,
			created,
			lastLogin,
			status,
			truncateColumn(stripControlCharacters(user.Annotation), listUsersNotesWidth),
			stripControlCharacters(describeUserAuth(user)))
	}
	fmt.Println(strings.Repeat("=", listUsersRuleWidth) + "\n")

	return nil
}

// Column widths for the -list-users table. The authentication column is not
// among them: it is the last column, printed in full, because it usually holds
// an issuer URL, and issuers from the same provider (Entra tenants, Keycloak
// realms) differ only near the end, so any truncation would make them look
// identical. The rule width is the sum of the fixed columns, the single space
// after each of them and the authentication heading, so the rules span the
// header row exactly; a row with a long issuer runs on past them.
const (
	listUsersUsernameWidth  = 20
	listUsersCreatedWidth   = 17
	listUsersLastLoginWidth = 17
	listUsersStatusWidth    = 20
	listUsersNotesWidth     = 20

	listUsersAuthHeader = "Authentication"

	listUsersRuleWidth = listUsersUsernameWidth + listUsersCreatedWidth +
		listUsersLastLoginWidth + listUsersStatusWidth + listUsersNotesWidth + 5 +
		len(listUsersAuthHeader)
)

// listUsersRowFormat lays out one row of the -list-users table. It is built
// from the width constants above rather than written out, so that the header,
// the rules and the rows cannot drift apart when a column is resized. The last
// column, authentication, is unpadded because nothing follows it.
var listUsersRowFormat = fmt.Sprintf("%%-%ds %%-%ds %%-%ds %%-%ds %%-%ds %%s\n",
	listUsersUsernameWidth, listUsersCreatedWidth, listUsersLastLoginWidth,
	listUsersStatusWidth, listUsersNotesWidth)

// describeUserAuth renders how an account signs in, for the authentication
// column of the -list-users table.
//
// Only an auth_source of exactly local is shown as local, matching
// AuthenticateUser, which refuses a password to every other value, empty
// included. A federated account is shown by its issuer, which is what an
// operator needs in order to answer "which provider owns this account?"; the
// subject is deliberately never printed. An oidc account whose stored external
// subject cannot be parsed still reports as federated, under the generic
// "OIDC" label, since the important fact is that it does not sign in locally.
// Any other source is shown as stored, or as "Unknown" when it is empty, which
// the NOT NULL DEFAULT 'local' column should make impossible.
func describeUserAuth(user *auth.StoredUser) string {
	if user.AuthSource == auth.AuthSourceLocal {
		return "Local"
	}
	if issuer := auth.IssuerFromExternalSubject(user.ExternalSubject); issuer != "" {
		return issuer
	}
	switch user.AuthSource {
	case auth.AuthSourceOIDC:
		return "OIDC"
	case "":
		return "Unknown"
	default:
		return user.AuthSource
	}
}

// stripControlCharacters replaces every control character in value with a
// question mark, so that a stored value cannot move the cursor, recolour the
// terminal or forge extra rows when it is printed. logging.SanitizeForLog is
// not used because it escapes only a fixed list of characters, leaving the
// rest of the C0 range and the C1 range (which includes the single-byte CSI,
// U+009B) untouched; unicode.IsControl covers both.
func stripControlCharacters(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, value)
}

// truncateColumn fits a value into a fixed-width column, marking a value that
// did not fit with a trailing ellipsis.
//
// Both the fit and the cut are measured in runes, because that is what the
// %-Ns padding in listUsersRowFormat counts: measuring bytes would cut a
// multi-byte value that fits well before the column is full. Cutting on a rune
// boundary also means an annotation, which is free text an administrator typed,
// is never split mid-character into a replacement character. Runes are still
// not display width: a full-width character occupies two terminal cells, so a
// row holding one prints wider than the rule, and correcting that would mean
// taking on a width-measuring dependency for a cosmetic gain in an
// administrative table.
func truncateColumn(value string, width int) string {
	if utf8.RuneCountInString(value) <= width {
		return value
	}

	const ellipsis = "..."

	runes := []rune(value)
	if width <= len(ellipsis) {
		return string(runes[:max(width, 0)])
	}
	return string(runes[:width-len(ellipsis)]) + ellipsis
}

// enableUserCommand handles the enable-user command
func enableUserCommand(dataDir, username string) error {
	// Open auth store
	store, err := openAuthStoreCLI(dataDir)
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

	// Enable the user. The store clears the failed-attempt counter in
	// the same transaction, so a locked-out account is usable again as
	// soon as this returns.
	if err := cliStore(store).EnableUser(username); err != nil {
		return fmt.Errorf("failed to enable user: %w", err)
	}

	fmt.Printf("User '%s' enabled successfully (failed attempts reset)\n", username)
	return nil
}

// addServiceAccountCommand handles the add-service-account command
func addServiceAccountCommand(dataDir, username, annotation, fullName, email string) error {
	// Open auth store
	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	reader := bufio.NewReader(os.Stdin)

	// Prompt for username if not provided
	if username == "" {
		fmt.Print("Enter service account username: ")
		if input, err := reader.ReadString('\n'); err == nil {
			username = strings.TrimSpace(input)
		}
		if username == "" {
			return fmt.Errorf("username is required")
		}
	}

	// Prompt for full name if not provided
	if fullName == "" {
		fmt.Print("Enter full name (optional): ")
		if input, err := reader.ReadString('\n'); err == nil {
			fullName = strings.TrimSpace(input)
		}
	}

	// Prompt for email if not provided
	if email == "" {
		fmt.Print("Enter email address (optional): ")
		if input, err := reader.ReadString('\n'); err == nil {
			email = strings.TrimSpace(input)
		}
	}

	// Prompt for notes if not provided
	if annotation == "" {
		fmt.Print("Enter notes for this service account (optional): ")
		if input, err := reader.ReadString('\n'); err == nil {
			annotation = strings.TrimSpace(input)
		}
	}

	// Create service account
	if err := cliStore(store).CreateServiceAccount(username, annotation, fullName, email); err != nil {
		return fmt.Errorf("failed to create service account: %w", err)
	}

	// Display results
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("Service account created successfully!")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("\nUsername:  %s\n", username)
	fmt.Println("Type:     Service Account (no password login)")
	if fullName != "" {
		fmt.Printf("Full Name: %s\n", fullName)
	}
	if email != "" {
		fmt.Printf("Email:    %s\n", email)
	}
	if annotation != "" {
		fmt.Printf("Notes:    %s\n", annotation)
	}
	fmt.Printf("Status:   Enabled\n")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("\nUse -add-token -user " + username + " to create a token for this service account.")
	fmt.Println(strings.Repeat("=", 70) + "\n")

	return nil
}

// disableUserCommand handles the disable-user command
func disableUserCommand(dataDir, username string) error {
	// Open auth store
	store, err := openAuthStoreCLI(dataDir)
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
	if err := cliStore(store).DisableUser(username); err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	fmt.Printf("User '%s' disabled successfully\n", username)
	return nil
}
