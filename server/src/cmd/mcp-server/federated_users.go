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
	"fmt"
	"strings"
)

// linkOIDCUserCommand handles the link-oidc-user command, attaching an
// existing account to an identity provider subject. With
// http.auth.oidc.provision_users disabled, which is the default, this is the
// only way an account can be reached by a federated login at all.
//
// The issuer and subject come from the server log: a refused federated login
// names both, quoted, in its "[OIDC] Refusing federated login" line.
func linkOIDCUserCommand(dataDir, username, issuer, subject string, relink bool) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}
	if issuer == "" {
		return fmt.Errorf("issuer is required")
	}
	if subject == "" {
		return fmt.Errorf("subject is required")
	}

	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	key, err := store.LinkFederatedIdentity(username, issuer, subject, relink)
	if err != nil {
		return fmt.Errorf("failed to link user: %w", err)
	}

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("Account linked to identity provider")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("\nUsername:        %s\n", username)
	fmt.Printf("Issuer:          %s\n", issuer)
	fmt.Printf("Subject:         %s\n", subject)
	fmt.Printf("External subject: %s\n", key)
	fmt.Println("Auth source:     oidc (password login is now refused for this account)")
	fmt.Println(strings.Repeat("=", 70) + "\n")

	return nil
}

// unlinkOIDCUserCommand handles the unlink-oidc-user command, detaching an
// account from its identity provider subject and returning it to local
// authentication.
//
// By default the account's password hash is replaced with an unusable one, so
// that unlinking cannot quietly revive a password nobody has rotated since
// before the account was federated. restorePassword keeps the old hash, for
// the case where the operator means to hand the account back to its holder.
func unlinkOIDCUserCommand(dataDir, username string, restorePassword bool) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}

	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	key, err := store.UnlinkFederatedIdentity(username, restorePassword)
	if err != nil {
		return fmt.Errorf("failed to unlink user: %w", err)
	}

	fmt.Printf("Account '%s' unlinked from federated subject %s\n", username, key)
	// Which of these two the operator gets is the whole point of the flag,
	// so both say exactly what the account can now be reached by.
	if restorePassword {
		fmt.Println("The account now authenticates locally. Any password it held before it was " +
			"linked works again: set a new password with -update-user, or disable the account " +
			"with -disable-user.")
	} else {
		fmt.Println("The account now authenticates locally and its password has been made " +
			"unusable, so nothing can log into it until you set a password with -update-user. " +
			"Pass -restore-password to keep the password it had before it was linked.")
	}
	fmt.Println("Any sessions the account held have been invalidated.")

	return nil
}
