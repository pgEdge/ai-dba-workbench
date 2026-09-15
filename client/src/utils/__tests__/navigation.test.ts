/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi } from 'vitest';
import { navigateTo } from '../navigation';

describe('navigateTo', () => {
    it('asks the given location to navigate to the URL', () => {
        const assign = vi.fn();
        const target = { assign } as unknown as Location;

        navigateTo('/api/v1/auth/oidc/start', target);

        expect(assign).toHaveBeenCalledWith('/api/v1/auth/oidc/start');
    });

    it('defaults to the window location', () => {
        /*
         * A fragment is the one navigation jsdom performs for real
         * without leaving the document, so it exercises the default
         * parameter without an unimplemented-navigation error.
         */
        navigateTo('#navigation-default');

        expect(window.location.hash).toBe('#navigation-default');

        window.history.replaceState({}, '', '/');
    });
});
