/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package usermgmt provides user account and service token management
package usermgmt

import (
    "bufio"
    "context"
    "crypto/rand"
    "crypto/sha256"
    "database/sql"
    "encoding/base64"
    "fmt"
    "os"
    "strings"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "golang.org/x/term"
)

// UserAccount represents a user account
type UserAccount struct {
    ID             int
    Username       string
    Email          string
    FullName       string
    PasswordHash   string
    PasswordExpiry sql.NullTime
    IsSuperuser    bool
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

// ServiceToken represents a service token
type ServiceToken struct {
    ID          int
    Name        string
    TokenHash   string
    IsSuperuser bool
    Note        sql.NullString
    ExpiresAt   sql.NullTime
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// HashPassword creates a SHA256 hash of a password
func HashPassword(password string) string {
    hash := sha256.Sum256([]byte(password))
    return fmt.Sprintf("%x", hash)
}

// GenerateToken generates a random token
func GenerateToken() (string, error) {
    bytes := make([]byte, 32)
    if _, err := rand.Read(bytes); err != nil {
        return "", fmt.Errorf("failed to generate token: %w", err)
    }
    return base64.URLEncoding.EncodeToString(bytes), nil
}

// ReadPassword reads a password from stdin without echo
func ReadPassword(prompt string) (string, error) {
    fmt.Print(prompt)
    password, err := term.ReadPassword(int(os.Stdin.Fd()))
    fmt.Println() // Print newline after password input
    if err != nil {
        return "", fmt.Errorf("failed to read password: %w", err)
    }
    return string(password), nil
}

// ReadInput reads a line of input from stdin
func ReadInput(prompt string) (string, error) {
    fmt.Print(prompt)
    reader := bufio.NewReader(os.Stdin)
    input, err := reader.ReadString('\n')
    if err != nil {
        return "", fmt.Errorf("failed to read input: %w", err)
    }
    return strings.TrimSpace(input), nil
}

// CreateUser creates a new user account
func CreateUser(pool *pgxpool.Pool, username string, interactive bool) error {
    ctx := context.Background()

    // Check if user already exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM user_accounts WHERE username = $1)",
        username).Scan(&exists)
    if err != nil {
        return fmt.Errorf("failed to check if user exists: %w", err)
    }
    if exists {
        return fmt.Errorf("user '%s' already exists", username)
    }

    var email, fullName, password string
    var isSuperuser bool
    var passwordExpiry sql.NullTime

    if interactive {
        // Prompt for email
        email, err = ReadInput("Email: ")
        if err != nil {
            return err
        }
        if email == "" {
            return fmt.Errorf("email is required")
        }

        // Prompt for full name
        fullName, err = ReadInput("Full name: ")
        if err != nil {
            return err
        }
        if fullName == "" {
            return fmt.Errorf("full name is required")
        }

        // Prompt for password
        password, err = ReadPassword("Password: ")
        if err != nil {
            return err
        }
        if password == "" {
            return fmt.Errorf("password is required")
        }

        // Confirm password
        confirmPassword, err := ReadPassword("Confirm password: ")
        if err != nil {
            return err
        }
        if password != confirmPassword {
            return fmt.Errorf("passwords do not match")
        }

        // Prompt for superuser status
        superuserInput, err := ReadInput("Superuser? (y/N): ")
        if err != nil {
            return err
        }
        isSuperuser = strings.ToLower(superuserInput) == "y" ||
            strings.ToLower(superuserInput) == "yes"

        // Prompt for password expiry (optional)
        expiryInput, err := ReadInput(
            "Password expiry (YYYY-MM-DD, leave blank for no expiry): ")
        if err != nil {
            return err
        }
        if expiryInput != "" {
            expiry, err := time.Parse("2006-01-02", expiryInput)
            if err != nil {
                return fmt.Errorf("invalid date format: %w", err)
            }
            passwordExpiry = sql.NullTime{Time: expiry, Valid: true}
        }
    } else {
        return fmt.Errorf("non-interactive mode not yet implemented")
    }

    // Hash the password
    passwordHash := HashPassword(password)

    // Insert the user
    _, err = pool.Exec(ctx, `
        INSERT INTO user_accounts
            (username, email, full_name, password_hash, password_expiry,
             is_superuser)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, username, email, fullName, passwordHash, passwordExpiry, isSuperuser)
    if err != nil {
        return fmt.Errorf("failed to create user: %w", err)
    }

    fmt.Printf("User '%s' created successfully\n", username)
    return nil
}

// ListUsers lists all user accounts
func ListUsers(pool *pgxpool.Pool) error {
    ctx := context.Background()

    rows, err := pool.Query(ctx, `
        SELECT username, email, full_name, password_expiry, is_superuser,
               created_at
        FROM user_accounts
        ORDER BY username
    `)
    if err != nil {
        return fmt.Errorf("failed to list users: %w", err)
    }
    defer rows.Close()

    fmt.Printf("%-20s %-30s %-25s %-15s %-10s %-12s\n",
        "Username", "Email", "Full Name", "Password Expiry", "Superuser",
        "Created")
    fmt.Println(strings.Repeat("-", 120))

    count := 0
    for rows.Next() {
        var username, email, fullName string
        var passwordExpiry sql.NullTime
        var isSuperuser bool
        var createdAt time.Time

        if err := rows.Scan(&username, &email, &fullName, &passwordExpiry,
            &isSuperuser, &createdAt); err != nil {
            return fmt.Errorf("failed to scan user: %w", err)
        }

        superuserStr := "No"
        if isSuperuser {
            superuserStr = "Yes"
        }

        expiryStr := "Never"
        if passwordExpiry.Valid {
            expiryStr = passwordExpiry.Time.Format("2006-01-02")
        }

        fmt.Printf("%-20s %-30s %-25s %-15s %-10s %-12s\n",
            username, email, fullName, expiryStr, superuserStr,
            createdAt.Format("2006-01-02"))
        count++
    }

    if err := rows.Err(); err != nil {
        return fmt.Errorf("error iterating users: %w", err)
    }

    fmt.Printf("\nTotal: %d user(s)\n", count)
    return nil
}

// DeleteUser deletes a user account
func DeleteUser(pool *pgxpool.Pool, username string, confirm bool) error {
    ctx := context.Background()

    // Check if user exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM user_accounts WHERE username = $1)",
        username).Scan(&exists)
    if err != nil {
        return fmt.Errorf("failed to check if user exists: %w", err)
    }
    if !exists {
        return fmt.Errorf("user '%s' does not exist", username)
    }

    if confirm {
        input, err := ReadInput(fmt.Sprintf(
            "Are you sure you want to delete user '%s'? (yes/no): ", username))
        if err != nil {
            return err
        }
        if strings.ToLower(input) != "yes" {
            fmt.Println("Delete canceled")
            return nil
        }
    }

    // Delete the user
    _, err = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1",
        username)
    if err != nil {
        return fmt.Errorf("failed to delete user: %w", err)
    }

    fmt.Printf("User '%s' deleted successfully\n", username)
    return nil
}

// CreateServiceToken creates a new service token
func CreateServiceToken(pool *pgxpool.Pool, name string, interactive bool) error {
    ctx := context.Background()

    // Check if token already exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM service_tokens WHERE name = $1)",
        name).Scan(&exists)
    if err != nil {
        return fmt.Errorf("failed to check if token exists: %w", err)
    }
    if exists {
        return fmt.Errorf("service token '%s' already exists", name)
    }

    var isSuperuser bool
    var note sql.NullString
    var expiresAt sql.NullTime

    if interactive {
        // Prompt for superuser status
        superuserInput, err := ReadInput("Superuser? (y/N): ")
        if err != nil {
            return err
        }
        isSuperuser = strings.ToLower(superuserInput) == "y" ||
            strings.ToLower(superuserInput) == "yes"

        // Prompt for note
        noteInput, err := ReadInput("Note (optional): ")
        if err != nil {
            return err
        }
        if noteInput != "" {
            note = sql.NullString{String: noteInput, Valid: true}
        }

        // Prompt for expiry (optional)
        expiryInput, err := ReadInput(
            "Expiry date (YYYY-MM-DD, leave blank for no expiry): ")
        if err != nil {
            return err
        }
        if expiryInput != "" {
            expiry, err := time.Parse("2006-01-02", expiryInput)
            if err != nil {
                return fmt.Errorf("invalid date format: %w", err)
            }
            expiresAt = sql.NullTime{Time: expiry, Valid: true}
        }
    } else {
        return fmt.Errorf("non-interactive mode not yet implemented")
    }

    // Generate token
    token, err := GenerateToken()
    if err != nil {
        return err
    }

    // Hash the token
    tokenHash := HashPassword(token)

    // Insert the service token
    _, err = pool.Exec(ctx, `
        INSERT INTO service_tokens
            (name, token_hash, is_superuser, note, expires_at)
        VALUES ($1, $2, $3, $4, $5)
    `, name, tokenHash, isSuperuser, note, expiresAt)
    if err != nil {
        return fmt.Errorf("failed to create service token: %w", err)
    }

    fmt.Printf("Service token '%s' created successfully\n", name)
    fmt.Printf("Token: %s\n", token)
    fmt.Println("IMPORTANT: Save this token now. You won't be able to see it again.")
    return nil
}

// ListServiceTokens lists all service tokens
func ListServiceTokens(pool *pgxpool.Pool) error {
    ctx := context.Background()

    rows, err := pool.Query(ctx, `
        SELECT name, is_superuser, note, expires_at, created_at
        FROM service_tokens
        ORDER BY name
    `)
    if err != nil {
        return fmt.Errorf("failed to list service tokens: %w", err)
    }
    defer rows.Close()

    fmt.Printf("%-30s %-10s %-35s %-15s %-12s\n",
        "Name", "Superuser", "Note", "Expires", "Created")
    fmt.Println(strings.Repeat("-", 110))

    count := 0
    for rows.Next() {
        var name string
        var isSuperuser bool
        var note sql.NullString
        var expiresAt sql.NullTime
        var createdAt time.Time

        if err := rows.Scan(&name, &isSuperuser, &note, &expiresAt,
            &createdAt); err != nil {
            return fmt.Errorf("failed to scan service token: %w", err)
        }

        superuserStr := "No"
        if isSuperuser {
            superuserStr = "Yes"
        }

        noteStr := "-"
        if note.Valid {
            noteStr = note.String
            if len(noteStr) > 35 {
                noteStr = noteStr[:32] + "..."
            }
        }

        expiresStr := "Never"
        if expiresAt.Valid {
            expiresStr = expiresAt.Time.Format("2006-01-02")
        }

        fmt.Printf("%-30s %-10s %-35s %-15s %-12s\n",
            name, superuserStr, noteStr, expiresStr,
            createdAt.Format("2006-01-02"))
        count++
    }

    if err := rows.Err(); err != nil {
        return fmt.Errorf("error iterating service tokens: %w", err)
    }

    fmt.Printf("\nTotal: %d service token(s)\n", count)
    return nil
}

// DeleteServiceToken deletes a service token
func DeleteServiceToken(pool *pgxpool.Pool, name string, confirm bool) error {
    ctx := context.Background()

    // Check if token exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM service_tokens WHERE name = $1)",
        name).Scan(&exists)
    if err != nil {
        return fmt.Errorf("failed to check if token exists: %w", err)
    }
    if !exists {
        return fmt.Errorf("service token '%s' does not exist", name)
    }

    if confirm {
        input, err := ReadInput(fmt.Sprintf(
            "Are you sure you want to delete service token '%s'? (yes/no): ",
            name))
        if err != nil {
            return err
        }
        if strings.ToLower(input) != "yes" {
            fmt.Println("Delete canceled")
            return nil
        }
    }

    // Delete the service token
    _, err = pool.Exec(ctx, "DELETE FROM service_tokens WHERE name = $1", name)
    if err != nil {
        return fmt.Errorf("failed to delete service token: %w", err)
    }

    fmt.Printf("Service token '%s' deleted successfully\n", name)
    return nil
}

// CreateUserNonInteractive creates a new user account (non-interactive for MCP)
func CreateUserNonInteractive(pool *pgxpool.Pool, username, email, fullName,
    password string, isSuperuser bool,
    passwordExpiry *time.Time) (string, error) {
    ctx := context.Background()

    // Check if user already exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM user_accounts WHERE username = $1)",
        username).Scan(&exists)
    if err != nil {
        return "", fmt.Errorf("failed to check if user exists: %w", err)
    }
    if exists {
        return "", fmt.Errorf("user '%s' already exists", username)
    }

    // Validate required fields
    if email == "" {
        return "", fmt.Errorf("email is required")
    }
    if fullName == "" {
        return "", fmt.Errorf("full name is required")
    }
    if password == "" {
        return "", fmt.Errorf("password is required")
    }

    // Hash the password
    passwordHash := HashPassword(password)

    // Prepare password expiry
    var pwdExpiry sql.NullTime
    if passwordExpiry != nil {
        pwdExpiry = sql.NullTime{Time: *passwordExpiry, Valid: true}
    }

    // Insert the user
    _, err = pool.Exec(ctx, `
        INSERT INTO user_accounts
            (username, email, full_name, password_hash, password_expiry,
             is_superuser)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, username, email, fullName, passwordHash, pwdExpiry, isSuperuser)
    if err != nil {
        return "", fmt.Errorf("failed to create user: %w", err)
    }

    return fmt.Sprintf("User '%s' created successfully", username), nil
}

// UpdateUserNonInteractive updates a user account (non-interactive for MCP)
func UpdateUserNonInteractive(pool *pgxpool.Pool, username string,
    email, fullName, password *string, isSuperuser *bool,
    passwordExpiry *time.Time, clearPasswordExpiry bool) (string, error) {
    ctx := context.Background()

    // Check if user exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM user_accounts WHERE username = $1)",
        username).Scan(&exists)
    if err != nil {
        return "", fmt.Errorf("failed to check if user exists: %w", err)
    }
    if !exists {
        return "", fmt.Errorf("user '%s' does not exist", username)
    }

    // Build dynamic update query
    updates := []string{}
    args := []interface{}{}
    argCount := 1

    if email != nil {
        updates = append(updates, fmt.Sprintf("email = $%d", argCount))
        args = append(args, *email)
        argCount++
    }

    if fullName != nil {
        updates = append(updates, fmt.Sprintf("full_name = $%d", argCount))
        args = append(args, *fullName)
        argCount++
    }

    if password != nil {
        passwordHash := HashPassword(*password)
        updates = append(updates, fmt.Sprintf("password_hash = $%d", argCount))
        args = append(args, passwordHash)
        argCount++
    }

    if isSuperuser != nil {
        updates = append(updates, fmt.Sprintf("is_superuser = $%d", argCount))
        args = append(args, *isSuperuser)
        argCount++
    }

    if clearPasswordExpiry {
        updates = append(updates, "password_expiry = NULL")
    } else if passwordExpiry != nil {
        updates = append(updates, fmt.Sprintf("password_expiry = $%d",
            argCount))
        args = append(args, *passwordExpiry)
        argCount++
    }

    if len(updates) == 0 {
        return "", fmt.Errorf("no fields to update")
    }

    // Always update the updated_at field
    updates = append(updates, fmt.Sprintf("updated_at = $%d", argCount))
    args = append(args, time.Now())
    argCount++

    // Add username as the last argument for WHERE clause
    args = append(args, username)

    query := fmt.Sprintf("UPDATE user_accounts SET %s WHERE username = $%d",
        strings.Join(updates, ", "), argCount)

    _, err = pool.Exec(ctx, query, args...)
    if err != nil {
        return "", fmt.Errorf("failed to update user: %w", err)
    }

    return fmt.Sprintf("User '%s' updated successfully", username), nil
}

// DeleteUserNonInteractive deletes a user account (non-interactive for MCP)
func DeleteUserNonInteractive(pool *pgxpool.Pool, username string) (string,
    error) {
    ctx := context.Background()

    // Check if user exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM user_accounts WHERE username = $1)",
        username).Scan(&exists)
    if err != nil {
        return "", fmt.Errorf("failed to check if user exists: %w", err)
    }
    if !exists {
        return "", fmt.Errorf("user '%s' does not exist", username)
    }

    // Delete the user
    _, err = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1",
        username)
    if err != nil {
        return "", fmt.Errorf("failed to delete user: %w", err)
    }

    return fmt.Sprintf("User '%s' deleted successfully", username), nil
}

// CreateServiceTokenNonInteractive creates a new service token
// (non-interactive for MCP)
func CreateServiceTokenNonInteractive(pool *pgxpool.Pool, name string,
    isSuperuser bool, note *string, expiresAt *time.Time) (string, string,
    error) {
    ctx := context.Background()

    // Check if token already exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM service_tokens WHERE name = $1)",
        name).Scan(&exists)
    if err != nil {
        return "", "", fmt.Errorf("failed to check if token exists: %w", err)
    }
    if exists {
        return "", "", fmt.Errorf("service token '%s' already exists", name)
    }

    // Prepare optional fields
    var noteVal sql.NullString
    if note != nil {
        noteVal = sql.NullString{String: *note, Valid: true}
    }

    var expiresAtVal sql.NullTime
    if expiresAt != nil {
        expiresAtVal = sql.NullTime{Time: *expiresAt, Valid: true}
    }

    // Generate token
    token, err := GenerateToken()
    if err != nil {
        return "", "", err
    }

    // Hash the token
    tokenHash := HashPassword(token)

    // Insert the service token
    _, err = pool.Exec(ctx, `
        INSERT INTO service_tokens
            (name, token_hash, is_superuser, note, expires_at)
        VALUES ($1, $2, $3, $4, $5)
    `, name, tokenHash, isSuperuser, noteVal, expiresAtVal)
    if err != nil {
        return "", "", fmt.Errorf("failed to create service token: %w", err)
    }

    message := fmt.Sprintf("Service token '%s' created successfully", name)
    return message, token, nil
}

// UpdateServiceTokenNonInteractive updates a service token
// (non-interactive for MCP)
func UpdateServiceTokenNonInteractive(pool *pgxpool.Pool, name string,
    isSuperuser *bool, note *string, expiresAt *time.Time,
    clearNote, clearExpiresAt bool) (string, error) {
    ctx := context.Background()

    // Check if token exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM service_tokens WHERE name = $1)",
        name).Scan(&exists)
    if err != nil {
        return "", fmt.Errorf("failed to check if token exists: %w", err)
    }
    if !exists {
        return "", fmt.Errorf("service token '%s' does not exist", name)
    }

    // Build dynamic update query
    updates := []string{}
    args := []interface{}{}
    argCount := 1

    if isSuperuser != nil {
        updates = append(updates, fmt.Sprintf("is_superuser = $%d", argCount))
        args = append(args, *isSuperuser)
        argCount++
    }

    if clearNote {
        updates = append(updates, "note = NULL")
    } else if note != nil {
        updates = append(updates, fmt.Sprintf("note = $%d", argCount))
        args = append(args, *note)
        argCount++
    }

    if clearExpiresAt {
        updates = append(updates, "expires_at = NULL")
    } else if expiresAt != nil {
        updates = append(updates, fmt.Sprintf("expires_at = $%d", argCount))
        args = append(args, *expiresAt)
        argCount++
    }

    if len(updates) == 0 {
        return "", fmt.Errorf("no fields to update")
    }

    // Always update the updated_at field
    updates = append(updates, fmt.Sprintf("updated_at = $%d", argCount))
    args = append(args, time.Now())
    argCount++

    // Add name as the last argument for WHERE clause
    args = append(args, name)

    query := fmt.Sprintf("UPDATE service_tokens SET %s WHERE name = $%d",
        strings.Join(updates, ", "), argCount)

    _, err = pool.Exec(ctx, query, args...)
    if err != nil {
        return "", fmt.Errorf("failed to update service token: %w", err)
    }

    return fmt.Sprintf("Service token '%s' updated successfully", name), nil
}

// DeleteServiceTokenNonInteractive deletes a service token
// (non-interactive for MCP)
func DeleteServiceTokenNonInteractive(pool *pgxpool.Pool, name string) (string,
    error) {
    ctx := context.Background()

    // Check if token exists
    var exists bool
    err := pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM service_tokens WHERE name = $1)",
        name).Scan(&exists)
    if err != nil {
        return "", fmt.Errorf("failed to check if token exists: %w", err)
    }
    if !exists {
        return "", fmt.Errorf("service token '%s' does not exist", name)
    }

    // Delete the service token
    _, err = pool.Exec(ctx, "DELETE FROM service_tokens WHERE name = $1", name)
    if err != nil {
        return "", fmt.Errorf("failed to delete service token: %w", err)
    }

    return fmt.Sprintf("Service token '%s' deleted successfully", name), nil
}
