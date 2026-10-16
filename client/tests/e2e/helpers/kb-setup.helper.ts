/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Knowledgebase (KB) setup for DOCKER install mode.
 *
 * Docker mode builds the workbench from source via
 * docker/docker-compose.yml + server/Dockerfile and, unlike RPM mode, ships
 * no KB. RPM mode installs a `pgedge-ai-kb-<provider>-<model>` package inside
 * docker/Dockerfile.rpm-installer, selected from E2E_EMBEDDING_MODEL. This
 * helper gives docker mode the equivalent, driven entirely by the same env
 * selections:
 *
 *   - E2E_EMBEDDING_MODEL  selects which KB database to obtain.
 *   - REPO_CHANNEL         picks the pgEdge dnf channel (staging | release).
 *
 * It pulls the matching `kb-<provider>-<model>.db` onto the host (via a
 * throwaway rockylinux:9 container that reuses the exact package resolution
 * RPM mode uses), where docker-compose.yml bind-mounts it into the server
 * container. generateE2EConfigs() then points the server's `knowledgebase:`
 * block at it. When no model is selected the step is skipped and the
 * search_knowledgebase tool stays inactive, exactly as in RPM mode.
 */

import { spawnSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import type { InstallMode, RepoChannel } from '../fixtures/e2e-config';

/**
 * Container directory the host KB database directory is mounted at (see the
 * `server` service in docker/docker-compose.yml). The selected `.db` lands
 * directly under this path and the generated `knowledgebase.database_path`
 * points at it.
 */
export const KB_CONTAINER_DIR = '/opt/kb';

/**
 * Host KB directory, relative to the e2e package root. It lives under
 * config/generated/ (git-ignored) alongside the other generated artifacts
 * and is bind-mounted into the server container. The directory is always
 * created — even when empty — so the compose bind mount never fails.
 */
export const KB_HOST_SUBDIR = path.join('config', 'generated', 'kb');

/**
 * Model -> KB package / database / provider mapping.
 *
 * SSOT: this MUST mirror the `case "${E2E_EMBEDDING_MODEL}"` block in
 * docker/Dockerfile.rpm-installer (currently ~lines 82-89). Model names are
 * globally unique across providers, so the model alone resolves the package;
 * the provider is recorded here because the generated KB embedding block and
 * the `kb-<provider>-<model>.db` filename both need it. Keep this table in
 * lockstep with that Dockerfile — when one changes, change the other.
 */
export interface KbPackage {
    /** Embedding provider baked into the KB database (voyage|openai|gemini|ollama). */
    provider: string;
    /** dnf/apt package name, e.g. pgedge-ai-kb-voyage-voyage-3. */
    pkg: string;
    /** Database filename inside the package, e.g. kb-voyage-voyage-3.db. */
    dbFile: string;
}

export const KB_MODEL_MAP: Record<string, KbPackage> = {
    'voyage-3': {
        provider: 'voyage',
        pkg: 'pgedge-ai-kb-voyage-voyage-3',
        dbFile: 'kb-voyage-voyage-3.db',
    },
    'text-embedding-3-small': {
        provider: 'openai',
        pkg: 'pgedge-ai-kb-openai-text-embedding-3-small',
        dbFile: 'kb-openai-text-embedding-3-small.db',
    },
    'gemini-embedding-001': {
        provider: 'gemini',
        pkg: 'pgedge-ai-kb-gemini-gemini-embedding-001',
        dbFile: 'kb-gemini-gemini-embedding-001.db',
    },
    'nomic-embed-text': {
        provider: 'ollama',
        pkg: 'pgedge-ai-kb-ollama-nomic-embed-text',
        dbFile: 'kb-ollama-nomic-embed-text.db',
    },
};

/** Result of obtaining a KB database, consumed by generateE2EConfigs(). */
export interface KbResult {
    /** Embedding provider baked into the database. */
    provider: string;
    /** Embedding model the database was built with. */
    model: string;
    /** Database filename, e.g. kb-gemini-gemini-embedding-001.db. */
    dbFile: string;
    /** Absolute container path of the mounted database. */
    containerDbPath: string;
}

/** Options controlling whether and how the KB database is obtained. */
export interface KbSetupOptions {
    installMode: InstallMode;
    /** Whether AI features are enabled; the KB tool is useless without them. */
    aiEnabled: boolean;
    /** Raw E2E_EMBEDDING_MODEL selection (undefined/empty => skip KB). */
    model: string | undefined;
    repoChannel: RepoChannel;
}

/**
 * Resolve the selected embedding model to its KB package, or null when no
 * model is selected (skip KB) or the model is unknown (warn and skip),
 * mirroring the default case in Dockerfile.rpm-installer.
 */
export function resolveKbPackage(model: string | undefined): KbPackage | null {
    const trimmed = (model ?? '').trim();
    if (trimmed === '') {
        return null;
    }
    const entry = KB_MODEL_MAP[trimmed];
    if (!entry) {
        console.warn(
            `[E2E setup] Unknown embedding model "${trimmed}" — skipping KB ` +
            '(mirrors Dockerfile.rpm-installer default case).',
        );
        return null;
    }
    return entry;
}

/** Container path of a KB database file mounted under KB_CONTAINER_DIR. */
export function kbContainerDbPath(dbFile: string): string {
    return `${KB_CONTAINER_DIR}/${dbFile}`;
}

/**
 * Build the server-format `knowledgebase:` YAML block for a selected KB.
 *
 * The keys map 1:1 onto config.KnowledgebaseConfig
 * (server/src/internal/config/config.go): `enabled`, `database_path`,
 * `embedding_provider`, `embedding_model`, and the per-provider API-key file
 * (`embedding_<provider>_api_key_file`) or `embedding_ollama_url`.
 *
 * The server re-embeds every KB query at runtime with this provider/model,
 * so they MUST match the provider/model the database was built with — which
 * is exactly the KB_MODEL_MAP provider and the selected model. The query
 * re-embedding reuses the SAME key already mounted for the top-level
 * `embedding:` block (/etc/pgedge/embedding_api_key), because in a valid
 * selection the KB provider and the E2E embedding provider are identical.
 *
 * The hard-coded server default database_path
 * (/usr/share/pgedge/postgres-mcp-kb/kb.db) is stale, so database_path is
 * always set explicitly to the mounted container path.
 */
export function buildServerKnowledgebaseBlock(
    kb: KbResult,
    ollamaUrl = 'http://localhost:11434',
): Record<string, unknown> {
    const block: Record<string, unknown> = {
        enabled: true,
        database_path: kb.containerDbPath,
        embedding_provider: kb.provider,
        embedding_model: kb.model,
    };

    const keyFileMap: Record<string, string> = {
        voyage: 'embedding_voyage_api_key_file',
        openai: 'embedding_openai_api_key_file',
        gemini: 'embedding_gemini_api_key_file',
    };

    if (kb.provider === 'ollama') {
        block['embedding_ollama_url'] = ollamaUrl;
    } else if (keyFileMap[kb.provider]) {
        block[keyFileMap[kb.provider]] = '/etc/pgedge/embedding_api_key';
    }

    return block;
}

/**
 * Obtain the KB database selected by the env for a docker-mode run.
 *
 * Returns the resolved KbResult when a database is available for the
 * generated config to enable, or null when KB should stay inactive:
 *   - any non-docker install mode (RPM mode ships its own KB),
 *   - AI disabled (the KB query re-embedding has no provider/key configured),
 *   - no / unknown E2E_EMBEDDING_MODEL.
 *
 * The host KB directory is always created so the compose bind mount never
 * fails, even when this returns null. Obtaining the database is idempotent:
 * an existing host copy is reused rather than re-downloaded.
 */
export async function setupKnowledgebase(
    e2eDir: string,
    options: KbSetupOptions,
): Promise<KbResult | null> {
    const hostKbDir = path.join(e2eDir, KB_HOST_SUBDIR);
    // Always create the mount source so docker-compose never fails on a
    // missing bind-mount path, regardless of whether a KB is selected.
    fs.mkdirSync(hostKbDir, { recursive: true });

    if (options.installMode !== 'docker') {
        return null;
    }
    if (!options.aiEnabled) {
        return null;
    }

    const kbPackage = resolveKbPackage(options.model);
    if (!kbPackage) {
        return null;
    }

    const { provider, pkg, dbFile } = kbPackage;
    const hostDbPath = path.join(hostKbDir, dbFile);
    const result: KbResult = {
        provider,
        model: (options.model ?? '').trim(),
        dbFile,
        containerDbPath: kbContainerDbPath(dbFile),
    };

    // Idempotent: reuse an already-downloaded database.
    if (fs.existsSync(hostDbPath)) {
        console.log(
            `[E2E setup] KB database already present (${dbFile}) — reusing ` +
            `host copy, skipping download.`,
        );
        return result;
    }

    const ok = downloadKbDatabase(hostKbDir, pkg, dbFile, options.repoChannel);
    if (!ok || !fs.existsSync(hostDbPath)) {
        console.warn(
            `[E2E setup] Could not obtain KB database for model ` +
            `"${result.model}" (package ${pkg}); search_knowledgebase will ` +
            'stay inactive. This is expected where dnf.pgedge.com is ' +
            'unreachable; it works on a host with repository access.',
        );
        return null;
    }

    console.log(
        `[E2E setup] KB database obtained: ${dbFile} ` +
        `(provider=${provider}, model=${result.model}, ` +
        `channel=${options.repoChannel}) → ${KB_HOST_SUBDIR}/`,
    );
    return result;
}

/**
 * Pull a single KB database onto the host using a throwaway rockylinux:9
 * container. This reuses RPM mode's exact package resolution
 * (pgedge-release repo + optional staging rewrite + dnf install), so no host
 * rpm/dnf tooling is required and the result matches what RPM mode installs.
 *
 * `pkg` and `dbFile` always come from the internal KB_MODEL_MAP (never user
 * input), and `repoChannel` is validated to staging|release upstream, so the
 * inlined shell is not exposed to injection.
 */
function downloadKbDatabase(
    hostKbDir: string,
    pkg: string,
    dbFile: string,
    repoChannel: RepoChannel,
): boolean {
    console.log(
        `[E2E setup] Obtaining KB database ${dbFile} via ${pkg} ` +
        `(channel=${repoChannel})...`,
    );

    // The KB .db ships under /usr/share/pgedge/pgedge-ai-kb/ (see
    // pgedge-ai-kb.spec). Copy it out to the bind-mounted /out directory.
    const script = [
        'set -e',
        'dnf install -y https://dnf.pgedge.com/reporpm/pgedge-release-latest.noarch.rpm',
        'if [ "$REPO_CHANNEL" = "staging" ]; then ' +
            'sed -i "s/release/staging/g" /etc/yum.repos.d/pgedge*.repo; fi',
        `dnf install -y --nogpgcheck ${pkg}`,
        `cp /usr/share/pgedge/pgedge-ai-kb/${dbFile} /out/${dbFile}`,
    ].join('\n');

    const res = spawnSync('docker', [
        'run', '--rm',
        '-e', `REPO_CHANNEL=${repoChannel}`,
        '-v', `${hostKbDir}:/out`,
        'rockylinux:9',
        'bash', '-c', script,
    ], { stdio: 'inherit' });

    if (res.error) {
        console.warn(
            `[E2E setup] KB download container failed to run: ` +
            `${res.error.message}`,
        );
        return false;
    }
    return res.status === 0;
}
