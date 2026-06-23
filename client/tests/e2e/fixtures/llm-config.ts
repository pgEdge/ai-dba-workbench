/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import * as fs from 'fs';
import * as path from 'path';
import * as yaml from 'js-yaml';

// ---------------------------------------------------------------------------
// Provider union types
// ---------------------------------------------------------------------------

export const VALID_REASONING_PROVIDERS = [
    'anthropic', 'openai', 'gemini', 'ollama',
] as const;
export type ReasoningProvider = typeof VALID_REASONING_PROVIDERS[number];

export const VALID_EMBEDDING_PROVIDERS = [
    'voyage', 'openai', 'gemini', 'cohere', 'ollama',
] as const;
export type EmbeddingProvider = typeof VALID_EMBEDDING_PROVIDERS[number];

// ---------------------------------------------------------------------------
// Config interfaces
// ---------------------------------------------------------------------------

export interface ReasoningConfig {
    provider: ReasoningProvider;
    model: string;
    apiKey: string | undefined;
    baseUrl: string | undefined;
}

export interface EmbeddingConfig {
    provider: EmbeddingProvider;
    model: string;
    apiKey: string | undefined;
    baseUrl: string | undefined;
}

export interface LlmConfig {
    enabled: boolean;
    reasoning: ReasoningConfig;
    embedding: EmbeddingConfig;
}

// ---------------------------------------------------------------------------
// YAML shape (mirrors config/llm.yaml)
// ---------------------------------------------------------------------------

interface YamlLlmConfig {
    ai_enabled?: boolean;
    reasoning?: {
        provider?: string;
        model?: string;
    };
    embedding?: {
        provider?: string;
        model?: string;
    };
    alerter?: {
        reasoning?: {
            provider?: string;
            model?: string;
        };
        embedding?: {
            provider?: string;
            model?: string;
        };
    };
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function loadYaml(): YamlLlmConfig {
    const configPath = path.join(__dirname, '..', 'config', 'llm.yaml');
    if (!fs.existsSync(configPath)) {
        return {};
    }
    const raw = yaml.load(fs.readFileSync(configPath, 'utf8'));
    if (raw && typeof raw === 'object') {
        return raw as YamlLlmConfig;
    }
    return {};
}

function coerceBool(value: string | undefined, fallback: boolean): boolean {
    if (value === undefined) {
        return fallback;
    }
    const lower = value.toLowerCase();
    if (lower === 'true' || lower === '1') {
        return true;
    }
    if (lower === 'false' || lower === '0') {
        return false;
    }
    return fallback;
}

function validateReasoningProvider(value: string): ReasoningProvider {
    if (
        !(VALID_REASONING_PROVIDERS as readonly string[]).includes(value)
    ) {
        throw new Error(
            `Invalid reasoning provider "${value}". ` +
            `Must be one of: ${VALID_REASONING_PROVIDERS.join(', ')}.`,
        );
    }
    return value as ReasoningProvider;
}

function validateEmbeddingProvider(value: string): EmbeddingProvider {
    if (
        !(VALID_EMBEDDING_PROVIDERS as readonly string[]).includes(value)
    ) {
        throw new Error(
            `Invalid embedding provider "${value}". ` +
            `Must be one of: ${VALID_EMBEDDING_PROVIDERS.join(', ')}.`,
        );
    }
    return value as EmbeddingProvider;
}

/** Resolve the API key for a reasoning provider from env vars. */
function resolveReasoningApiKey(
    provider: ReasoningProvider,
): string | undefined {
    switch (provider) {
        case 'anthropic':
            return process.env['E2E_ANTHROPIC_API_KEY'];
        case 'openai':
            return process.env['E2E_OPENAI_API_KEY'];
        case 'gemini':
            return process.env['E2E_GEMINI_API_KEY'];
        case 'ollama':
            return undefined;
    }
}

/** Resolve the API key for an embedding provider from env vars. */
function resolveEmbeddingApiKey(
    provider: EmbeddingProvider,
): string | undefined {
    switch (provider) {
        case 'voyage':
            return process.env['E2E_VOYAGE_API_KEY'];
        case 'openai':
            return process.env['E2E_OPENAI_API_KEY'];
        case 'gemini':
            return process.env['E2E_GEMINI_API_KEY'];
        case 'cohere':
            return process.env['E2E_COHERE_API_KEY'];
        case 'ollama':
            return undefined;
    }
}

/** Resolve the base URL (ollama only; undefined for cloud providers). */
function resolveBaseUrl(provider: string): string | undefined {
    if (provider === 'ollama') {
        return process.env['E2E_OLLAMA_BASE_URL'] ?? 'http://localhost:11434';
    }
    return undefined;
}

// ---------------------------------------------------------------------------
// Public loaders
// ---------------------------------------------------------------------------

/** Load and validate the top-level LLM configuration. */
export function loadLlmConfig(): LlmConfig {
    const yml = loadYaml();

    // --- enabled flag ---
    const enabled = coerceBool(
        process.env['E2E_AI_ENABLED'],
        yml.ai_enabled === true,
    );

    // --- reasoning ---
    const reasoningProviderRaw =
        process.env['E2E_REASONING_PROVIDER'] ??
        yml.reasoning?.provider ??
        'anthropic';
    const reasoningProvider = validateReasoningProvider(reasoningProviderRaw);

    const reasoningModel =
        process.env['E2E_REASONING_MODEL'] ??
        yml.reasoning?.model ??
        'claude-sonnet-4-6';

    // --- embedding ---
    const embeddingProviderRaw =
        process.env['E2E_EMBEDDING_PROVIDER'] ??
        yml.embedding?.provider ??
        'voyage';
    const embeddingProvider = validateEmbeddingProvider(embeddingProviderRaw);

    const embeddingModel =
        process.env['E2E_EMBEDDING_MODEL'] ??
        yml.embedding?.model ??
        'voyage-3';

    // --- API key validation when enabled ---
    const reasoningApiKey = resolveReasoningApiKey(reasoningProvider);
    const embeddingApiKey = resolveEmbeddingApiKey(embeddingProvider);

    if (enabled && reasoningProvider !== 'ollama' && !reasoningApiKey) {
        throw new Error(
            `LLM is enabled (E2E_AI_ENABLED=true) but no API key is set ` +
            `for reasoning provider "${reasoningProvider}". ` +
            `Set the appropriate E2E_*_API_KEY environment variable.`,
        );
    }

    if (enabled && embeddingProvider !== 'ollama' && !embeddingApiKey) {
        console.warn(
            `[llm-config] Warning: LLM is enabled but no API key is set ` +
            `for embedding provider "${embeddingProvider}". ` +
            `Embedding-dependent tests may fail.`,
        );
    }

    return {
        enabled,
        reasoning: {
            provider: reasoningProvider,
            model: reasoningModel,
            apiKey: reasoningApiKey,
            baseUrl: resolveBaseUrl(reasoningProvider),
        },
        embedding: {
            provider: embeddingProvider,
            model: embeddingModel,
            apiKey: embeddingApiKey,
            baseUrl: resolveBaseUrl(embeddingProvider),
        },
    };
}

/**
 * Load alerter-specific LLM configuration.
 *
 * Starts from the top-level config, then overlays alerter-specific
 * env vars. When an alerter override is empty or unset, the
 * top-level value is retained.
 */
export function loadAlerterLlmConfig(): LlmConfig {
    const base = loadLlmConfig();
    const yml = loadYaml();

    // --- alerter reasoning overrides ---
    const alerterReasoningProviderRaw =
        process.env['E2E_ALERTER_REASONING_PROVIDER'] ??
        (yml.alerter?.reasoning?.provider || undefined);

    let reasoningProvider = base.reasoning.provider;
    if (alerterReasoningProviderRaw) {
        reasoningProvider = validateReasoningProvider(
            alerterReasoningProviderRaw,
        );
    }

    const alerterReasoningModel =
        process.env['E2E_ALERTER_REASONING_MODEL'] ??
        (yml.alerter?.reasoning?.model || undefined);

    const reasoningModel = alerterReasoningModel ?? base.reasoning.model;

    // --- alerter embedding overrides ---
    const alerterEmbeddingProviderRaw =
        process.env['E2E_ALERTER_EMBEDDING_PROVIDER'] ??
        (yml.alerter?.embedding?.provider || undefined);

    let embeddingProvider = base.embedding.provider;
    if (alerterEmbeddingProviderRaw) {
        embeddingProvider = validateEmbeddingProvider(
            alerterEmbeddingProviderRaw,
        );
    }

    const alerterEmbeddingModel =
        process.env['E2E_ALERTER_EMBEDDING_MODEL'] ??
        (yml.alerter?.embedding?.model || undefined);

    const embeddingModel = alerterEmbeddingModel ?? base.embedding.model;

    // Re-resolve API keys after provider overlay.
    const reasoningApiKey = resolveReasoningApiKey(reasoningProvider);
    const embeddingApiKey = resolveEmbeddingApiKey(embeddingProvider);

    return {
        enabled: base.enabled,
        reasoning: {
            provider: reasoningProvider,
            model: reasoningModel,
            apiKey: reasoningApiKey,
            baseUrl: resolveBaseUrl(reasoningProvider),
        },
        embedding: {
            provider: embeddingProvider,
            model: embeddingModel,
            apiKey: embeddingApiKey,
            baseUrl: resolveBaseUrl(embeddingProvider),
        },
    };
}

// ---------------------------------------------------------------------------
// Ready-to-use singletons
// ---------------------------------------------------------------------------

export const LLM_CONFIG: LlmConfig = loadLlmConfig();
export const ALERTER_LLM_CONFIG: LlmConfig = loadAlerterLlmConfig();
