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
import { dangerColour } from '../actions';

describe('dangerColour', () => {
    it.each<[string | undefined, ReturnType<typeof dangerColour>]>([
        ['low', 'success'],
        ['moderate', 'warning'],
        ['high', 'error'],
        ['critical', 'error'],
        ['unrated', 'default'],
        ['', 'default'],
        [undefined, 'default'],
    ])('maps %j to %s', (danger, colour) => {
        expect(dangerColour(danger)).toBe(colour);
    });
});
