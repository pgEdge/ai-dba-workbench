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
 * Minimal type declarations for js-yaml.
 *
 * This file provides type coverage until @types/js-yaml is installed
 * via npm install in the E2E package. Once the package is installed
 * from the devDependencies declared in package.json, this file can
 * be removed.
 */
declare module 'js-yaml' {
    /**
     * Parse a YAML string and return the corresponding JavaScript
     * value (object, array, string, number, null, etc.).
     */
    export function load(input: string): unknown;

    /** Options accepted by {@link dump}. */
    export interface DumpOptions {
        /** Set max line width. Use -1 for unlimited width. */
        lineWidth?: number;
        /** Indentation width in spaces. */
        indent?: number;
        /** Do not throw on invalid types and store them as text. */
        skipInvalid?: boolean;
        /** Specifies level of nesting for collections (-1 for unlimited). */
        flowLevel?: number;
        /** If true, sort keys when dumping YAML. */
        sortKeys?: boolean;
        /** Do not convert duplicate objects into references. */
        noRefs?: boolean;
        /** Do not use compatibility mode. */
        noCompatMode?: boolean;
    }

    /**
     * Serialize a JavaScript value to a YAML string.
     */
    export function dump(input: unknown, options?: DumpOptions): string;
}
