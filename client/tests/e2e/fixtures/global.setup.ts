/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { chromium, type FullConfig } from '@playwright/test';
import { execSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import * as yaml from 'js-yaml';
import { ApiHelper } from '../helpers/api.helper';
import { ADMIN_USER, API_URL, BASE_URL } from './test-data';
import { type E2EConfig, loadE2EConfig } from './e2e-config';
import { LLM_CONFIG, ALERTER_LLM_CONFIG, type LlmConfig } from './llm-config';
import { setupWorkbenchDocker, createAdminUserDocker } from './setup/workbench-docker';
import { setupWorkbenchRPM, createAdminUserRPM } from './setup/workbench-rpm';

/**
 * Playwright global setup function.
 *
 * This runs once before all test files. It starts the workbench stack
 * in the configured install mode (Docker or RPM), verifies the server
 * is reachable, authenticates as the admin user, and saves a browser
 * storage state file so that browser-based tests can skip the UI
 * login flow.
 */
async function globalSetup(_config: FullConfig): Promise<void> {
    const e2eConfig = loadE2EConfig();
    const api = new ApiHelper(API_URL);

    // -------------------------------------------------------
    // 0a. Generate secrets on first run
    // -------------------------------------------------------
    const e2eDir = path.join(__dirname, '..');
    const secretFile = path.join(e2eDir, 'secret', 'ai-dba.secret');
    if (!fs.existsSync(secretFile)) {
        console.log('[E2E setup] Generating secrets...');
        execSync('bash scripts/setup-secrets.sh', {
            cwd: e2eDir,
            stdio: 'inherit',
        });
    }

    // -------------------------------------------------------
    // 0b. Generate configs (LLM injection + config/generated/)
    // -------------------------------------------------------
    generateE2EConfigs(e2eDir);

    // -------------------------------------------------------
    // 1. Start workbench stack (mode-specific)
    // -------------------------------------------------------
    await setupWorkbench(e2eConfig, LLM_CONFIG.enabled);

    // -------------------------------------------------------
    // 2. Health check
    // -------------------------------------------------------
    const maxAttempts = 30;
    const delayMs = 2_000;
    let healthy = false;
    for (let i = 0; i < maxAttempts; i++) {
        if (await api.healthCheck()) {
            healthy = true;
            break;
        }
        await sleep(delayMs);
    }
    if (!healthy) {
        throw new Error(
            `Server at ${API_URL}/health did not become reachable ` +
            `after ${maxAttempts} attempts. Is the E2E stack running?`,
        );
    }

    // -------------------------------------------------------
    // 3. Create admin user (mode-specific)
    // -------------------------------------------------------
    if (e2eConfig.installMode === 'docker') {
        createAdminUserDocker();
    } else {
        createAdminUserRPM();
    }

    // -------------------------------------------------------
    // 4. Authenticate as admin via API
    // -------------------------------------------------------
    // The health endpoint can return 200 before the server has
    // finished running migrations and creating the default admin
    // user. Retry login for up to 30 seconds to handle this race.
    let cookie: string | undefined;
    const loginDeadline = Date.now() + 30_000;
    let lastLoginError = '';
    while (Date.now() < loginDeadline) {
        try {
            const result = await api.login(
                ADMIN_USER.username,
                ADMIN_USER.password,
            );
            cookie = result.cookie;
            break;
        } catch (err) {
            lastLoginError = err instanceof Error ? err.message : String(err);
            await sleep(2_000);
        }
    }
    if (!cookie) {
        throw new Error(
            `[E2E setup] Admin login failed for user ` +
            `"${ADMIN_USER.username}" at ${API_URL}: ${lastLoginError}`,
        );
    }

    // Expose the raw cookie to helpers that run outside of a
    // Playwright browser context (e.g. ApiHelper in test hooks).
    process.env.E2E_ADMIN_COOKIE = cookie;

    // -------------------------------------------------------
    // 5. Save browser storage state for Playwright contexts
    // -------------------------------------------------------
    const authDir = path.resolve(__dirname, '..', '.auth');
    if (!fs.existsSync(authDir)) {
        fs.mkdirSync(authDir, { recursive: true });
    }

    const browser = await chromium.launch();
    const context = await browser.newContext();

    // Parse the cookie value from "session_token=<value>".
    const cookieValue = cookie.split('=').slice(1).join('=');
    const baseUrlObj = new URL(BASE_URL);

    await context.addCookies([
        {
            name: 'session_token',
            value: cookieValue,
            domain: baseUrlObj.hostname,
            path: '/',
            httpOnly: true,
            sameSite: 'Lax',
        },
    ]);

    // Wait for the React client (nginx) to be reachable on BASE_URL
    // before navigating. The client container may still be starting
    // even after the API health check passes.
    for (let i = 0; i < maxAttempts; i++) {
        if (await isHttpServiceReady(BASE_URL)) { break; }
        if (i === maxAttempts - 1) {
            throw new Error(
                `Client at ${BASE_URL} did not become reachable ` +
                `after ${maxAttempts} attempts.`,
            );
        }
        await sleep(delayMs);
    }

    // Navigate to the app so cookies are persisted.
    const page = await context.newPage();
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' });

    const statePath = path.join(authDir, 'admin.json');
    await context.storageState({ path: statePath });

    await browser.close();

    // -------------------------------------------------------
    // 6. Start notification mock services
    // -------------------------------------------------------
    // In CI the main docker-compose.yml already starts mailpit and
    // wiremock. Attempting to start them again via the notifications
    // compose would cause a port conflict and abort setup. Only start
    // them when they are not already reachable.
    const mailpitReady = await isHttpServiceReady('http://localhost:8025/api/v1/messages');
    const wiremockReady = await isHttpServiceReady('http://localhost:9090/__admin/requests');
    if (!mailpitReady || !wiremockReady) {
        const NOTIFICATIONS_COMPOSE = path.join(
            __dirname, '..', 'docker', 'docker-compose.notifications.yml',
        );
        execSync(`docker compose -f ${NOTIFICATIONS_COMPOSE} pull`, { stdio: 'pipe' });
        execSync(`docker compose -f ${NOTIFICATIONS_COMPOSE} up -d`, { stdio: 'pipe' });
    }
    await waitForHttpService('http://localhost:8025/api/v1/messages');  // Mailpit
    await waitForHttpService('http://localhost:9090/__admin/requests'); // WireMock
    const wireMockUrl = 'http://localhost:9090';
    for (const p of ['/slack', '/mattermost', '/webhook']) {
        await registerWireMockStub(wireMockUrl, p);
    }
}

// -----------------------------------------------------------------------
// LLM config generation
// -----------------------------------------------------------------------

/**
 * Build the server-format LLM YAML block from the resolved LlmConfig.
 *
 * The server's LLMConfig struct reads API keys from files, not inline
 * values. The key file path inside the container is always
 * /etc/pgedge/llm_api_key (mounted from secret/llm_api_key).
 */
function buildServerLlmBlock(config: LlmConfig): Record<string, unknown> {
    const block: Record<string, unknown> = {
        provider: config.reasoning.provider,
        model: config.reasoning.model,
    };

    const keyFileMap: Record<string, string> = {
        anthropic: 'anthropic_api_key_file',
        openai: 'openai_api_key_file',
        gemini: 'gemini_api_key_file',
    };

    const provider = config.reasoning.provider;
    if (provider === 'ollama') {
        block['ollama_url'] = config.reasoning.baseUrl
            ?? 'http://localhost:11434';
    } else if (keyFileMap[provider]) {
        block[keyFileMap[provider]] = '/etc/pgedge/llm_api_key';
    }

    return block;
}

/**
 * Build the alerter-format LLM YAML block from the resolved LlmConfig.
 *
 * The alerter's LLMConfig struct uses a nested per-provider layout:
 *   llm:
 *     reasoning_provider: <provider>
 *     embedding_provider: <provider>
 *     <provider>:
 *       api_key_file: /etc/pgedge/llm_api_key
 *       reasoning_model: <model>
 */
function buildAlerterLlmBlock(config: LlmConfig): Record<string, unknown> {
    const block: Record<string, unknown> = {
        reasoning_provider: config.reasoning.provider,
        embedding_provider: config.embedding.provider,
    };

    const providerBlock = (
        provider: string,
        model: string,
        isReasoning: boolean,
    ): Record<string, unknown> => {
        const entry: Record<string, unknown> = {};
        if (provider === 'ollama') {
            entry['base_url'] = config.reasoning.baseUrl
                ?? 'http://localhost:11434';
        } else {
            entry['api_key_file'] = '/etc/pgedge/llm_api_key';
        }
        if (isReasoning) {
            entry['reasoning_model'] = model;
        } else {
            entry['embedding_model'] = model;
        }
        return entry;
    };

    // Reasoning provider sub-block
    block[config.reasoning.provider] = providerBlock(
        config.reasoning.provider,
        config.reasoning.model,
        true,
    );

    // Embedding provider sub-block (merge if same provider)
    if (config.embedding.provider === config.reasoning.provider) {
        const existing = block[config.reasoning.provider] as Record<string, unknown>;
        existing['embedding_model'] = config.embedding.model;
    } else {
        block[config.embedding.provider] = providerBlock(
            config.embedding.provider,
            config.embedding.model,
            false,
        );
    }

    return block;
}

/**
 * Generate E2E configs with LLM settings injected.
 *
 * Creates config/generated/ with copies of all base configs. When
 * LLM is enabled, the server and alerter configs receive an `llm:`
 * block and the API key is written to secret/llm_api_key. When
 * disabled, configs are copied unchanged and the key file is empty.
 *
 * Sets process.env.E2E_CONFIG_DIR so docker-compose picks up the
 * generated configs.
 */
function generateE2EConfigs(e2eDir: string): void {
    const configDir = path.join(e2eDir, 'config');
    const generatedDir = path.join(configDir, 'generated');
    const secretDir = path.join(e2eDir, 'secret');

    // Ensure directories exist.
    fs.mkdirSync(generatedDir, { recursive: true });
    fs.mkdirSync(secretDir, { recursive: true });

    // (a) Write the LLM API key file.
    const keyFilePath = path.join(secretDir, 'llm_api_key');
    if (LLM_CONFIG.enabled && LLM_CONFIG.reasoning.apiKey) {
        fs.writeFileSync(keyFilePath, LLM_CONFIG.reasoning.apiKey, {
            mode: 0o600,
        });
    } else {
        // Empty file so the Docker volume mount never fails.
        fs.writeFileSync(keyFilePath, '', { mode: 0o600 });
    }

    // (b) Copy base configs to generated/.
    const configFiles = [
        'ai-dba-server.yaml',
        'ai-dba-alerter.yaml',
        'ai-dba-collector.yaml',
    ];
    for (const file of configFiles) {
        const src = path.join(configDir, file);
        const dest = path.join(generatedDir, file);
        if (fs.existsSync(src)) {
            fs.copyFileSync(src, dest);
        }
    }

    if (LLM_CONFIG.enabled) {
        console.log(
            `[E2E setup] LLM config: enabled=true, ` +
            `provider=${LLM_CONFIG.reasoning.provider}, ` +
            `model=${LLM_CONFIG.reasoning.model}`,
        );

        // (c) Merge LLM block into generated server config.
        const serverConfigPath = path.join(
            generatedDir, 'ai-dba-server.yaml',
        );
        const serverYaml = yaml.load(
            fs.readFileSync(serverConfigPath, 'utf8'),
        ) as Record<string, unknown> | null ?? {};
        serverYaml['llm'] = buildServerLlmBlock(LLM_CONFIG);
        fs.writeFileSync(
            serverConfigPath,
            yaml.dump(serverYaml, { lineWidth: -1 }),
        );
        console.log(
            '[E2E setup] Generated server config with LLM block ' +
            `\u2192 config/generated/ai-dba-server.yaml`,
        );

        // (d) Merge LLM block into generated alerter config.
        const alerterConfigPath = path.join(
            generatedDir, 'ai-dba-alerter.yaml',
        );
        const alerterYaml = yaml.load(
            fs.readFileSync(alerterConfigPath, 'utf8'),
        ) as Record<string, unknown> | null ?? {};
        alerterYaml['llm'] = buildAlerterLlmBlock(ALERTER_LLM_CONFIG);
        fs.writeFileSync(
            alerterConfigPath,
            yaml.dump(alerterYaml, { lineWidth: -1 }),
        );
        console.log(
            '[E2E setup] Generated alerter config with LLM block ' +
            `\u2192 config/generated/ai-dba-alerter.yaml`,
        );
    } else {
        console.log(
            '[E2E setup] LLM config: enabled=false ' +
            '\u2014 using base configs unchanged',
        );
    }

    // (e) Point docker-compose at the generated config directory.
    process.env.E2E_CONFIG_DIR = '../config/generated';
}

/** Route to the correct workbench setup based on install mode. */
async function setupWorkbench(config: E2EConfig, forceRecreateServer: boolean): Promise<void> {
    if (config.installMode === 'docker') {
        await setupWorkbenchDocker(forceRecreateServer);
    } else {
        await setupWorkbenchRPM(config.repoChannel, config.platformImage);
    }
}

function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

async function isHttpServiceReady(url: string): Promise<boolean> {
    try {
        const res = await fetch(url);
        return res.status < 500;
    } catch {
        return false;
    }
}

async function waitForHttpService(url: string, timeoutMs = 60_000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        try {
            const res = await fetch(url);
            if (res.status < 500) return;
        } catch {
            // not yet reachable
        }
        await sleep(1_000);
    }
    throw new Error(`Service at ${url} did not become ready within ${timeoutMs}ms`);
}

async function registerWireMockStub(baseUrl: string, stubPath: string): Promise<void> {
    await fetch(`${baseUrl}/__admin/mappings`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            request: { method: 'ANY', url: stubPath },
            response: { status: 200, body: 'ok' },
        }),
    });
}

export default globalSetup;
