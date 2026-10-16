/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test } from '@playwright/test';
import type { AnalysisDialog } from '../pages/AnalysisDialog';
import type { ChatPage } from '../pages/ChatPage';

/**
 * Matches an AI-provider usage/rate-limit rejection surfaced in an analysis
 * dialog's error message or a chat error bubble. These are ENVIRONMENTAL
 * (the account hit its provider quota, e.g.
 * `anthropic (400): You have reached your specified API usage limits`), not a
 * product or test defect, so a matching failure skips the step instead of
 * going red. It is deliberately narrow: a genuine analysis error (bad tool
 * call, malformed response, hallucinated SQL) does NOT match and still fails.
 */
export const AI_PROVIDER_LIMIT_PATTERN =
    /(usage limit|rate limit|too many requests|reached your .*limit)|\b(429)\b|(anthropic|openai|voyage|gemini)\s*\(\s*(400|429)\s*\)/i;

const SKIP_REASON =
    'AI provider usage/rate limit reached — environmental, not a defect';

/**
 * Skip the current test/step when an analysis dialog failed specifically
 * because an AI provider hit a usage/rate limit. Call this BEFORE asserting
 * that the dialog has no error: a provider limit skips here, while any other
 * error falls through and still fails the caller's `hasError` assertion.
 */
export async function skipIfAiProviderLimited(
    dialog: AnalysisDialog,
): Promise<void> {
    if ((await dialog.hasError()) && (await dialog.isProviderLimited())) {
        test.skip(true, SKIP_REASON);
    }
}

/**
 * Wait for an Ask Ellie chat response and return the panel transcript,
 * tolerating an AI provider usage/rate limit. When the response never
 * completes (the input never re-enables) AND the panel shows a provider
 * usage/rate-limit error, the current test is skipped (environmental). Any
 * other hang preserves the normal hard failure via the standard reader, and a
 * completed-but-wrong response is returned unchanged so genuine chat
 * assertions still run.
 */
export async function getChatResponseOrSkip(
    chat: ChatPage,
    timeout: number,
): Promise<string> {
    const settled = await chat.didResponseSettle(timeout);
    if (!settled) {
        const errText = await chat.responseErrorText();
        if (AI_PROVIDER_LIMIT_PATTERN.test(errText)) {
            test.skip(true, SKIP_REASON);
        }
    }
    // Settled: cheap re-read (input already enabled). Unsettled and NOT a
    // provider limit: a short reader re-asserts the input re-enabled and
    // fails fast, preserving the original behaviour for genuine chat hangs.
    return chat.getLastAssistantMessage(settled ? timeout : 1_000);
}
