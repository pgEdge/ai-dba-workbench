/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import {
    SQL_CODE_BLOCK_RULES,
    SQL_PLACEHOLDER_RULES,
} from '../analysisPrompts';

// ---------------------------------------------------------------------------
// SQL_CODE_BLOCK_RULES
// ---------------------------------------------------------------------------

describe('SQL_CODE_BLOCK_RULES', () => {
    it('is a non-empty string', () => {
        expect(typeof SQL_CODE_BLOCK_RULES).toBe('string');
        expect(SQL_CODE_BLOCK_RULES.length).toBeGreaterThan(0);
    });

    it('instructs the LLM to emit one query per sql code block', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain(
            'NEVER combine multiple queries in one block',
        );
    });

    it('lists the correct pg_stat_user_tables columns', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain('pg_stat_user_tables:');
        expect(SQL_CODE_BLOCK_RULES).toContain('n_dead_tup');
    });

    it('directs version-specific validation against the server context', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain(
            'valid for the specific PostgreSQL version in use',
        );
    });

    // -----------------------------------------------------------------------
    // bgwriter / checkpointer split (issue #286)
    // -----------------------------------------------------------------------

    it('mentions the new pg_stat_checkpointer view', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain('pg_stat_checkpointer');
    });

    it('lists the PostgreSQL 17+ pg_stat_checkpointer columns', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain('num_timed');
        expect(SQL_CODE_BLOCK_RULES).toContain('num_requested');
        expect(SQL_CODE_BLOCK_RULES).toContain('buffers_written');
    });

    it('flags pg_stat_checkpointer as PostgreSQL 17+ only', () => {
        expect(SQL_CODE_BLOCK_RULES).toMatch(
            /pg_stat_checkpointer.*PostgreSQL 17\+ ONLY/,
        );
    });

    it('explains the checkpoint columns moved in PostgreSQL 17+', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain(
            'were MOVED out of pg_stat_bgwriter into pg_stat_checkpointer',
        );
    });

    it('describes pg_stat_bgwriter for PostgreSQL 16 and earlier', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain(
            'pg_stat_bgwriter (PostgreSQL 16 and earlier, combined view)',
        );
    });

    it('describes pg_stat_bgwriter for PostgreSQL 17+', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain(
            'pg_stat_bgwriter (PostgreSQL 17+, background-writer stats only)',
        );
    });

    it('does not present the old combined columns as universally correct', () => {
        // The legacy checkpoint columns must never appear on a bare
        // pg_stat_bgwriter line that omits a version qualifier; the only
        // mentions are now version-scoped or in the "moved" explanation.
        const lines = SQL_CODE_BLOCK_RULES.split('\n');
        const legacyBareLine = lines.find(
            (line) =>
                /pg_stat_bgwriter: checkpoints_timed/.test(line),
        );
        expect(legacyBareLine).toBeUndefined();
    });

    it('keeps checkpoint columns out of the PG17+ bgwriter list', () => {
        const lines = SQL_CODE_BLOCK_RULES.split('\n');
        const pg17BgwriterLine = lines.find((line) =>
            line.includes(
                'pg_stat_bgwriter (PostgreSQL 17+, background-writer stats only)',
            ),
        );
        expect(pg17BgwriterLine).toBeDefined();
        expect(pg17BgwriterLine).not.toContain('checkpoints_timed');
        expect(pg17BgwriterLine).not.toContain('buffers_checkpoint');
    });

    // -----------------------------------------------------------------------
    // buffers_backend removal in PG17+ (issue #292)
    // -----------------------------------------------------------------------

    it('explains buffers_backend was removed from pg_stat_bgwriter in PG17+', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain(
            'buffers_backend and buffers_backend_fsync were REMOVED from pg_stat_bgwriter',
        );
    });

    it('directs backend-written-buffer stats to pg_stat_io on PG17+', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain('pg_stat_io');
    });

    it('forbids referencing buffers_backend on pg_stat_bgwriter for PG17+', () => {
        expect(SQL_CODE_BLOCK_RULES).toContain(
            'MUST NOT reference buffers_backend or buffers_backend_fsync against pg_stat_bgwriter on PostgreSQL 17+',
        );
    });

    it('still lists buffers_backend as valid for PostgreSQL 16 and earlier', () => {
        const lines = SQL_CODE_BLOCK_RULES.split('\n');
        const pg16BgwriterLine = lines.find((line) =>
            line.includes(
                'pg_stat_bgwriter (PostgreSQL 16 and earlier, combined view)',
            ),
        );
        expect(pg16BgwriterLine).toBeDefined();
        expect(pg16BgwriterLine).toContain('buffers_backend');
        expect(pg16BgwriterLine).toContain('buffers_backend_fsync');
    });
});

// ---------------------------------------------------------------------------
// SQL_PLACEHOLDER_RULES
// ---------------------------------------------------------------------------

describe('SQL_PLACEHOLDER_RULES', () => {
    it('is a non-empty string', () => {
        expect(typeof SQL_PLACEHOLDER_RULES).toBe('string');
        expect(SQL_PLACEHOLDER_RULES.length).toBeGreaterThan(0);
    });

    it('forbids placeholder identifiers', () => {
        expect(SQL_PLACEHOLDER_RULES).toContain(
            'NEVER use placeholder names',
        );
    });

    it('protects PRIMARY KEY and UNIQUE constraint indexes', () => {
        expect(SQL_PLACEHOLDER_RULES).toContain(
            'NEVER suggest dropping indexes that implement PRIMARY KEY or UNIQUE constraints',
        );
    });
});
