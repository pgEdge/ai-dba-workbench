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

export type InstallMode = 'docker' | 'rpm';
export type RepoChannel = 'staging' | 'release';

export const VALID_PLATFORM_IMAGES = [
    'rocky9', 'rocky10', 'rhel9', 'rhel10', 'debian12', 'ubuntu22',
] as const;
export type PlatformImage = typeof VALID_PLATFORM_IMAGES[number];

/** Resolved and validated E2E configuration. */
export interface E2EConfig {
    installMode: InstallMode;
    repoChannel: RepoChannel;
    platformImage: PlatformImage;
}

/** Docker base images for each platform option. */
export const PLATFORM_BASE_IMAGES: Record<PlatformImage, string> = {
    rocky9:   'rockylinux:9',
    rocky10:  'rockylinux:10',
    rhel9:    'redhat/ubi9',
    rhel10:   'redhat/ubi10',
    debian12: 'debian:12',
    ubuntu22: 'ubuntu:22.04',
};

/** Load, merge, and validate E2E configuration. */
export function loadE2EConfig(): E2EConfig {
    // 1. Load YAML defaults
    const configPath = path.join(__dirname, '..', 'config', 'e2e-test.yaml');
    let yamlDefaults: Record<string, string> = {};
    if (fs.existsSync(configPath)) {
        const raw = yaml.load(fs.readFileSync(configPath, 'utf8'));
        if (raw && typeof raw === 'object') {
            yamlDefaults = raw as Record<string, string>;
        }
    }

    // Resolve each setting (env > YAML > hardcoded default).
    // Playwright intercepts unknown CLI flags, so runtime overrides must
    // be passed as environment variables, not --flag=value arguments.
    const installMode = (
        process.env['INSTALL_MODE'] ??
        yamlDefaults['install_mode'] ??
        'docker'
    ) as InstallMode;

    const repoChannel = (
        process.env['REPO_CHANNEL'] ??
        yamlDefaults['repo_channel'] ??
        'release'
    ) as RepoChannel;

    const platformImage = (
        process.env['PLATFORM_IMAGE'] ??
        yamlDefaults['platform_image'] ??
        'rocky9'
    ) as PlatformImage;

    // 3. Validate
    if (!['docker', 'rpm'].includes(installMode)) {
        throw new Error(
            `Invalid install_mode "${installMode}". Must be "docker" or "rpm".`,
        );
    }
    if (!['staging', 'release'].includes(repoChannel)) {
        throw new Error(
            `Invalid repo_channel "${repoChannel}". Must be "staging" or "release".`,
        );
    }
    if (!(VALID_PLATFORM_IMAGES as readonly string[]).includes(platformImage)) {
        throw new Error(
            `Invalid platform_image "${platformImage}". ` +
            `Must be one of: ${VALID_PLATFORM_IMAGES.join(', ')}.`,
        );
    }

    return { installMode, repoChannel, platformImage };
}
