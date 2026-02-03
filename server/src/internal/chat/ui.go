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
// This file re-exports the shared UI types and functions from pkg/chat.
package chat

import (
	"context"

	pkgchat "github.com/pgedge/ai-workbench/pkg/chat"
)

// Re-export constants from pkg/chat
const (
	KeyEscape    = pkgchat.KeyEscape
	ColorReset   = pkgchat.ColorReset
	ColorRed     = pkgchat.ColorRed
	ColorGreen   = pkgchat.ColorGreen
	ColorYellow  = pkgchat.ColorYellow
	ColorBlue    = pkgchat.ColorBlue
	ColorMagenta = pkgchat.ColorMagenta
	ColorCyan    = pkgchat.ColorCyan
	ColorGray    = pkgchat.ColorGray
	ColorBold    = pkgchat.ColorBold
)

// Re-export UI type from pkg/chat
type UI = pkgchat.UI

// NewUI creates a new UI instance
var NewUI = pkgchat.NewUI

// ListenForEscape monitors stdin for the Escape key and calls cancel when detected.
func ListenForEscape(ctx context.Context, done chan struct{}, cancel context.CancelFunc) {
	pkgchat.ListenForEscape(ctx, done, cancel)
}

// elephantActions contains PostgreSQL/Elephant themed action words for animation (for testing)
var elephantActions = pkgchat.ElephantActions
