/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package chat provides the interactive chat client functionality.
// This file re-exports the shared preferences types and functions from pkg/chat.
package chat

import (
	pkgchat "github.com/pgedge/ai-workbench/pkg/chat"
)

// Re-export types from pkg/chat for backward compatibility
type Preferences = pkgchat.Preferences
type UIPreferences = pkgchat.UIPreferences

// CurrentPreferencesVersion is the current preferences file format version
const CurrentPreferencesVersion = pkgchat.CurrentPreferencesVersion

// GetPreferencesPath returns the path to the user preferences file
var GetPreferencesPath = pkgchat.GetPreferencesPath

// LoadPreferences loads user preferences from the preferences file
var LoadPreferences = pkgchat.LoadPreferences

// SavePreferences saves user preferences to the preferences file
var SavePreferences = pkgchat.SavePreferences

// getDefaultPreferences returns default preferences (package-internal name)
func getDefaultPreferences() *Preferences {
	return pkgchat.GetDefaultPreferences()
}

// sanitizePreferences validates and fixes corrupted preference data (for testing)
var sanitizePreferences = pkgchat.SanitizePreferences
