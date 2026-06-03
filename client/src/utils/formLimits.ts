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
 * Maximum length for short free-text form fields such as names, hosts,
 * database names, and usernames. The backend stores these values in
 * VARCHAR(255) columns, so enforcing the same limit on the client
 * prevents over-length input from reaching the server and producing a
 * confusing generic error.
 */
export const MAX_FIELD_LENGTH = 255;
