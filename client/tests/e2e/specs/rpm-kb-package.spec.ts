/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import * as path from 'path';
import { loadE2EConfig } from '../fixtures/e2e-config';
import { LLM_CONFIG } from '../fixtures/llm-config';

// ---------------------------------------------------------------
// Constants
// ---------------------------------------------------------------

const E2E_DIR = path.join(__dirname, '..');
const RPM_COMPOSE_FILE = path.join(E2E_DIR, 'docker', 'docker-compose.rpm.yml');
const KB_DIR = '/usr/share/pgedge/pgedge-ai-kb';

/**
 * Environment passed to docker compose exec calls.
 * POSTGRES_PASSWORD must be present for compose to parse the file,
 * but the container is already running so the actual value is unused.
 */
const COMPOSE_ENV = {
    ...process.env,
    POSTGRES_PASSWORD: process.env['POSTGRES_PASSWORD'] ?? 'placeholder',
};

/**
 * Maps an embedding model name to the expected kb database filename
 * installed by the corresponding pgedge-ai-kb-* RPM package.
 */
const MODEL_TO_KB_FILE: Record<string, string> = {
    'voyage-3':                 'kb-voyage-voyage-3.db',
    'text-embedding-3-small':   'kb-openai-text-embedding-3-small.db',
    'gemini-embedding-001':     'kb-gemini-gemini-embedding-001.db',
    'nomic-embed-text':         'kb-ollama-nomic-embed-text.db',
};

// ---------------------------------------------------------------
// RPM Knowledge Base Package Validation
// ---------------------------------------------------------------

test.describe('RPM Knowledge Base Package', () => {
    const config = loadE2EConfig();
    const embeddingModel = LLM_CONFIG.embedding.model;

    // Skip the entire suite when not running in RPM mode.
    test.skip(
        config.installMode !== 'rpm',
        'RPM kb package tests only run in RPM install mode',
    );

    // Skip when AI is not enabled — the kb package is only installed
    // when E2E_AI_ENABLED=true, regardless of the default embedding model.
    test.skip(
        !LLM_CONFIG.enabled,
        'AI is not enabled (E2E_AI_ENABLED); skipping kb package validation',
    );

    // Skip when E2E_EMBEDDING_MODEL is not explicitly set. The default
    // value from llm.yaml should not trigger kb validation.
    test.skip(
        !process.env['E2E_EMBEDDING_MODEL'],
        'No E2E_EMBEDDING_MODEL explicitly set; skipping kb package validation',
    );

    test('correct kb file is installed for the configured embedding model', () => {
        const expectedFile = MODEL_TO_KB_FILE[embeddingModel];
        expect(
            expectedFile,
            `No kb file mapping for embedding model "${embeddingModel}". ` +
            `Known models: ${Object.keys(MODEL_TO_KB_FILE).join(', ')}`,
        ).toBeDefined();

        // -------------------------------------------------------
        // Step 1: List all .db files in the kb directory
        // -------------------------------------------------------
        const findOutput = execSync(
            `docker compose -f ${RPM_COMPOSE_FILE} exec -T workbench ` +
            `find ${KB_DIR} -name "*.db"`,
            { cwd: E2E_DIR, encoding: 'utf-8', env: COMPOSE_ENV },
        ).trim();

        const dbFiles = findOutput
            .split('\n')
            .map((line) => line.trim())
            .filter((line) => line.length > 0);

        // -------------------------------------------------------
        // Step 2: Assert exactly one .db file is present
        // -------------------------------------------------------
        expect(
            dbFiles,
            `Expected exactly 1 .db file in ${KB_DIR}, ` +
            `but found ${dbFiles.length}: ${dbFiles.join(', ')}`,
        ).toHaveLength(1);

        // -------------------------------------------------------
        // Step 3: Assert the filename matches the expected file
        // -------------------------------------------------------
        const actualFilename = path.basename(dbFiles[0]);
        expect(
            actualFilename,
            `Expected kb file "${expectedFile}" for model "${embeddingModel}", ` +
            `but found "${actualFilename}"`,
        ).toBe(expectedFile);

        // -------------------------------------------------------
        // Step 4: Assert file permissions are 644
        // -------------------------------------------------------
        const statOutput = execSync(
            `docker compose -f ${RPM_COMPOSE_FILE} exec -T workbench ` +
            `stat -c '%a' ${dbFiles[0]}`,
            { cwd: E2E_DIR, encoding: 'utf-8', env: COMPOSE_ENV },
        ).trim();

        expect(
            statOutput,
            `Expected permissions 644 on ${actualFilename}, ` +
            `but found ${statOutput}`,
        ).toBe('644');
    });
});
