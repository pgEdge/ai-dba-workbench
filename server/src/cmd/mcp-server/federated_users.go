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

// sessionCaveat is printed by both commands. Sessions live in the running
// server's own memory, keyed by a map this process does not share, so neither
// command can end one: only the server itself, or the enabled flag that
// ValidateSessionToken re-reads from the database on every request, can.
// Saying so is the difference between an operator closing an offboarding
// ticket and finishing the job.
const sessionCaveat = "Browser sessions the running server has already issued are held in that " +
	"server's memory and are not cleared by this command: disable the account with -disable-user, " +
	"or restart the server, to end them."

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
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("\n%s\n\n", sessionCaveat)

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

	key, revoked, err := store.UnlinkFederatedIdentity(username, restorePassword)
	if err != nil {
		return fmt.Errorf("failed to unlink user: %w", err)
	}

	fmt.Printf("Account '%s' unlinked from federated subject %s\n", username, key)
	// Which of these two the operator gets is the whole point of the flag,
	// so each says exactly what the account can still be reached by. Saying
	// only that the sessions are gone would read as "access revoked" on the
	// branch where the password and the tokens both survive.
	if restorePassword {
		fmt.Println("The account now authenticates locally: any password it held before it was " +
			"linked works again, and its API tokens were left in place.")
	} else {
		fmt.Printf("The account now authenticates locally with an unusable password, and %d API "+
			"token(s) have been revoked, so no password login and no token can reach it until you "+
			"set a password with -update-user. Pass -restore-password to keep the password and "+
			"the tokens it had before it was linked.\n", revoked)
	}
	// Said on both branches, because it is the one thing this command
	// cannot do: the session map is per-process and in memory, so the
	// running server still holds whatever sessions it minted. Disabling
	// the account does cut them, since ValidateSessionToken re-reads the
	// enabled flag from the database on every request.
	fmt.Println(sessionCaveat)

	return nil
}
