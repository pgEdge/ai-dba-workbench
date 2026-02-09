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
 * Re-export ServerDialog from the modular directory structure.
 * This maintains backward compatibility with existing imports.
 */
export { default } from './ServerDialog/index';
// eslint-disable-next-line react-refresh/only-export-components
export * from './ServerDialog/ServerDialog.types';
