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
// This file re-exports the shared conversations types and client from pkg/chat.
package chat

import (
	pkgchat "github.com/pgedge/ai-workbench/pkg/chat"
)

// Re-export types from pkg/chat for backward compatibility
type ConversationSummary = pkgchat.ConversationSummary
type Conversation = pkgchat.Conversation
type ConversationsClient = pkgchat.ConversationsClient
type ListResponse = pkgchat.ListResponse
type CreateConversationRequest = pkgchat.CreateConversationRequest
type RenameConversationRequest = pkgchat.RenameConversationRequest

// NewConversationsClient creates a new conversations client
var NewConversationsClient = pkgchat.NewConversationsClient
