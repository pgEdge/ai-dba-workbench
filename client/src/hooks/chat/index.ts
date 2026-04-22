/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Barrel file re-exporting the public API from the chat hook module.

export { useChat, default } from './useChat';
export type {
    ChatMessage,
    ContentBlock,
    ToolActivity,
    UseChatReturn,
} from './useChat';
