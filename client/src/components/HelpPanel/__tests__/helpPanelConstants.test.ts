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
import { HELP_PAGES, contextToPage } from '../helpPanelConstants';

describe('helpPanelConstants', () => {
    describe('HELP_PAGES', () => {
        it('has all expected page keys', () => {
            expect(HELP_PAGES.overview).toBe('overview');
            expect(HELP_PAGES.navigator).toBe('navigator');
            expect(HELP_PAGES.statusPanel).toBe('statusPanel');
            expect(HELP_PAGES.alerts).toBe('alerts');
            expect(HELP_PAGES.serverManagement).toBe('serverManagement');
            expect(HELP_PAGES.settings).toBe('settings');
            expect(HELP_PAGES.administration).toBe('administration');
            expect(HELP_PAGES.blackouts).toBe('blackouts');
            expect(HELP_PAGES.askEllie).toBe('askEllie');
            expect(HELP_PAGES.monitoring).toBe('monitoring');
        });

        it('has exactly 10 pages', () => {
            expect(Object.keys(HELP_PAGES)).toHaveLength(10);
        });
    });

    describe('contextToPage', () => {
        it('returns overview for null context', () => {
            expect(contextToPage(null)).toBe(HELP_PAGES.overview);
        });

        it('returns overview for empty string context', () => {
            expect(contextToPage('')).toBe(HELP_PAGES.overview);
        });

        it('returns overview for unknown context', () => {
            expect(contextToPage('unknown')).toBe(HELP_PAGES.overview);
            expect(contextToPage('foobar')).toBe(HELP_PAGES.overview);
        });

        describe('navigator contexts', () => {
            it('maps "navigator" to navigator page', () => {
                expect(contextToPage('navigator')).toBe(HELP_PAGES.navigator);
            });

            it('maps "cluster" to navigator page', () => {
                expect(contextToPage('cluster')).toBe(HELP_PAGES.navigator);
            });

            it('maps "group" to navigator page', () => {
                expect(contextToPage('group')).toBe(HELP_PAGES.navigator);
            });
        });

        describe('statusPanel contexts', () => {
            it('maps "server" to statusPanel page', () => {
                expect(contextToPage('server')).toBe(HELP_PAGES.statusPanel);
            });

            it('maps "status" to statusPanel page', () => {
                expect(contextToPage('status')).toBe(HELP_PAGES.statusPanel);
            });
        });

        describe('alerts contexts', () => {
            it('maps "alerts" to alerts page', () => {
                expect(contextToPage('alerts')).toBe(HELP_PAGES.alerts);
            });

            it('maps "alert" to alerts page', () => {
                expect(contextToPage('alert')).toBe(HELP_PAGES.alerts);
            });
        });

        describe('serverManagement contexts', () => {
            it('maps "serverDialog" to serverManagement page', () => {
                expect(contextToPage('serverDialog')).toBe(HELP_PAGES.serverManagement);
            });

            it('maps "addServer" to serverManagement page', () => {
                expect(contextToPage('addServer')).toBe(HELP_PAGES.serverManagement);
            });

            it('maps "editServer" to serverManagement page', () => {
                expect(contextToPage('editServer')).toBe(HELP_PAGES.serverManagement);
            });
        });

        describe('settings contexts', () => {
            it('maps "settings" to settings page', () => {
                expect(contextToPage('settings')).toBe(HELP_PAGES.settings);
            });

            it('maps "theme" to settings page', () => {
                expect(contextToPage('theme')).toBe(HELP_PAGES.settings);
            });
        });

        describe('administration contexts', () => {
            it('maps "administration" to administration page', () => {
                expect(contextToPage('administration')).toBe(HELP_PAGES.administration);
            });

            it('maps "admin" to administration page', () => {
                expect(contextToPage('admin')).toBe(HELP_PAGES.administration);
            });
        });

        describe('blackouts contexts', () => {
            it('maps "blackouts" to blackouts page', () => {
                expect(contextToPage('blackouts')).toBe(HELP_PAGES.blackouts);
            });

            it('maps "blackout" to blackouts page', () => {
                expect(contextToPage('blackout')).toBe(HELP_PAGES.blackouts);
            });

            it('maps "maintenance" to blackouts page', () => {
                expect(contextToPage('maintenance')).toBe(HELP_PAGES.blackouts);
            });
        });

        describe('askEllie contexts', () => {
            it('maps "chat" to askEllie page', () => {
                expect(contextToPage('chat')).toBe(HELP_PAGES.askEllie);
            });

            it('maps "ellie" to askEllie page', () => {
                expect(contextToPage('ellie')).toBe(HELP_PAGES.askEllie);
            });

            it('maps "askEllie" to askEllie page', () => {
                expect(contextToPage('askEllie')).toBe(HELP_PAGES.askEllie);
            });
        });

        describe('monitoring contexts', () => {
            it('maps "monitoring" to monitoring page', () => {
                expect(contextToPage('monitoring')).toBe(HELP_PAGES.monitoring);
            });

            it('maps "dashboard" to monitoring page', () => {
                expect(contextToPage('dashboard')).toBe(HELP_PAGES.monitoring);
            });

            it('maps "dashboards" to monitoring page', () => {
                expect(contextToPage('dashboards')).toBe(HELP_PAGES.monitoring);
            });
        });
    });
});
