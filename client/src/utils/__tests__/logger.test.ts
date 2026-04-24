/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Logger Utility Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, afterEach } from 'vitest';
import { logger } from '../logger';

describe('logger', () => {
    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('delegates error() to console.error', () => {
        const spy = vi.spyOn(console, 'error')
            .mockImplementation(() => {});
        logger.error('test error', { detail: 1 });
        expect(spy).toHaveBeenCalledWith(
            'test error',
            { detail: 1 },
        );
    });

    it('delegates warn() to console.warn', () => {
        const spy = vi.spyOn(console, 'warn')
            .mockImplementation(() => {});
        logger.warn('test warning', 42);
        expect(spy).toHaveBeenCalledWith('test warning', 42);
    });

    it('delegates info() to console.info', () => {
        const spy = vi.spyOn(console, 'info')
            .mockImplementation(() => {});
        logger.info('test info');
        expect(spy).toHaveBeenCalledWith('test info');
    });

    it('delegates debug() to console.debug', () => {
        const spy = vi.spyOn(console, 'debug')
            .mockImplementation(() => {});
        logger.debug('test debug', 'extra');
        expect(spy).toHaveBeenCalledWith(
            'test debug',
            'extra',
        );
    });

    it('passes zero arguments through', () => {
        const spy = vi.spyOn(console, 'error')
            .mockImplementation(() => {});
        logger.error();
        expect(spy).toHaveBeenCalledWith();
    });
});
