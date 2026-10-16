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
    SYSTEM_ALERT_SOURCE_LABEL,
    SYSTEM_ALERT_TYPE,
    isSystemAlert,
    isSystemAlertRecord,
} from '../systemAlerts';

describe('systemAlerts', () => {
    it('exposes the system alert type and source label', () => {
        expect(SYSTEM_ALERT_TYPE).toBe('system');
        expect(SYSTEM_ALERT_SOURCE_LABEL).toBe('AI DBA Workbench');
    });

    it('classifies raw API records', () => {
        expect(isSystemAlertRecord('system', null)).toBe(true);
        expect(isSystemAlertRecord('system', undefined)).toBe(true);
        expect(isSystemAlertRecord('threshold', null)).toBe(true);
        expect(isSystemAlertRecord('threshold', 3)).toBe(false);
        expect(isSystemAlertRecord(undefined, undefined)).toBe(false);
    });

    it('classifies transformed alerts', () => {
        expect(isSystemAlert({ isSystem: true })).toBe(true);
        expect(isSystemAlert({ alertType: 'system' })).toBe(true);
        expect(isSystemAlert({ alertType: 'anomaly', isSystem: false })).toBe(false);
        expect(isSystemAlert({})).toBe(false);
        expect(isSystemAlert(null)).toBe(false);
        expect(isSystemAlert(undefined)).toBe(false);
    });
});
