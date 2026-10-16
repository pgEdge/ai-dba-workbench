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

/**
 * Read an LLM API key from the environment. The `E2E_`-prefixed variable
 * wins (it isolates the test's key from whatever is in the shell), but we
 * fall back to the bare provider variable so that simply exporting
 * `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GEMINI_API_KEY` / `VOYAGE_API_KEY`
 * / `COHERE_API_KEY` also works.
 */
function readApiKey(name: string): string | undefined {
    return process.env[`E2E_${name}`] ?? process.env[name];
}

/** Resolve the API key for a reasoning provider from env vars. */
function resolveReasoningApiKey(
    provider: ReasoningProvider,
): string | undefined {
    switch (provider) {
        case 'anthropic':
            return readApiKey('ANTHROPIC_API_KEY');
        case 'openai':
            return readApiKey('OPENAI_API_KEY');
        case 'gemini':
            return readApiKey('GEMINI_API_KEY');
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
            return readApiKey('VOYAGE_API_KEY');
        case 'openai':
            return readApiKey('OPENAI_API_KEY');
        case 'gemini':
            return readApiKey('GEMINI_API_KEY');
        case 'cohere':
            return readApiKey('COHERE_API_KEY');
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

    // --- reasoning provider ---
    // Provider comes from env → config/llm.yaml. Use || so an empty string
    // (e.g. a workflow_dispatch input that resolves to "") counts as absent.
    // The hardcoded fallback applies ONLY when AI is disabled (the value is
    // then irrelevant); when AI is enabled a missing provider is a hard error
    // (see the fail-fast block below).
    const reasoningProviderRaw =
        (process.env['E2E_REASONING_PROVIDER'] || undefined) ??
        yml.reasoning?.provider ??
        undefined;
    const reasoningProvider = validateReasoningProvider(
        reasoningProviderRaw ?? 'anthropic',
    );

    const reasoningModelRaw =
        (process.env['E2E_REASONING_MODEL'] || undefined) ??
        yml.reasoning?.model ??
        undefined;
    const reasoningModel = reasoningModelRaw ?? 'claude-sonnet-4-6';

    // --- embedding provider ---
    const embeddingProviderRaw =
        (process.env['E2E_EMBEDDING_PROVIDER'] || undefined) ??
        yml.embedding?.provider ??
        undefined;
    const embeddingProvider = validateEmbeddingProvider(
        embeddingProviderRaw ?? 'voyage',
    );

    const embeddingModelRaw =
        (process.env['E2E_EMBEDDING_MODEL'] || undefined) ??
        yml.embedding?.model ??
        undefined;
    const embeddingModel = embeddingModelRaw ?? 'voyage-3';

    // --- Fail-fast AI configuration validation --------------------------
    // AI-dependent specs must not run against a half-configured LLM setup,
    // or they fail later with opaque errors. When AI is enabled, BOTH the
    // reasoning (LLM) and embedding providers must be configured, and each
    // (except keyless ollama) must have an API key. This runs at module load
    // (LLM_CONFIG below) → in global.setup → before any test executes.
    const reasoningApiKey = resolveReasoningApiKey(reasoningProvider);
    const embeddingApiKey = resolveEmbeddingApiKey(embeddingProvider);

    if (enabled) {
        if (!reasoningProviderRaw) {
            throw new Error(
                'AI is enabled (E2E_AI_ENABLED=true) but no LLM (reasoning) ' +
                'provider is configured. Set E2E_REASONING_PROVIDER (or ' +
                'reasoning.provider in config/llm.yaml) to one of: ' +
                `${VALID_REASONING_PROVIDERS.join(', ')}.`,
            );
        }
        if (!embeddingProviderRaw) {
            throw new Error(
                'AI is enabled (E2E_AI_ENABLED=true) but no embedding ' +
                'provider is configured. Set E2E_EMBEDDING_PROVIDER (or ' +
                'embedding.provider in config/llm.yaml) to one of: ' +
                `${VALID_EMBEDDING_PROVIDERS.join(', ')}.`,
            );
        }
        if (reasoningProvider !== 'ollama' && !reasoningApiKey) {
            throw new Error(
                'AI is enabled (E2E_AI_ENABLED=true) but no API key is set ' +
                `for the LLM (reasoning) provider "${reasoningProvider}". Set ` +
                `E2E_${reasoningProvider.toUpperCase()}_API_KEY (or the bare ` +
                `${reasoningProvider.toUpperCase()}_API_KEY).`,
            );
        }
        // Guard the common footgun: switching the PROVIDER but leaving the
        // model at its provider-specific default. The default reasoning model
        // (claude-sonnet-4-6) is anthropic-only and the default embedding
        // model (voyage-3) is voyage-only; using them with another provider
        // 404s at runtime (every AI test fails late with an opaque error).
        // Require an explicit model whenever the provider isn't the default's.
        if (reasoningProvider !== 'anthropic' && !reasoningModelRaw) {
            throw new Error(
                `AI is enabled with LLM (reasoning) provider ` +
                `"${reasoningProvider}", but no model is configured — the ` +
                `default "claude-sonnet-4-6" is anthropic-only and will 404 on ` +
                `${reasoningProvider}. Set E2E_REASONING_MODEL (or ` +
                `reasoning.model in config/llm.yaml) to a ${reasoningProvider} ` +
                'model.',
            );
        }
        if (embeddingProvider !== 'voyage' && !embeddingModelRaw) {
            throw new Error(
                `AI is enabled with embedding provider "${embeddingProvider}", ` +
                `but no model is configured — the default "voyage-3" is ` +
                `voyage-only and will fail on ${embeddingProvider}. Set ` +
                `E2E_EMBEDDING_MODEL (or embedding.model in config/llm.yaml) to ` +
                `a ${embeddingProvider} model.`,
            );
        }
        if (embeddingProvider !== 'ollama' && !embeddingApiKey) {
            throw new Error(
                'AI is enabled (E2E_AI_ENABLED=true) but no API key is set ' +
                `for the embedding provider "${embeddingProvider}". Set ` +
                `E2E_${embeddingProvider.toUpperCase()}_API_KEY (or the bare ` +
                `${embeddingProvider.toUpperCase()}_API_KEY).`,
            );
        }
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
