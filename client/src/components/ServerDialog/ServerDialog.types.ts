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
 * SSL mode options for PostgreSQL connections.
 */
export const SSL_MODES = [
    { value: 'disable', label: 'Disable' },
    { value: 'allow', label: 'Allow' },
    { value: 'prefer', label: 'Prefer' },
    { value: 'require', label: 'Require' },
    { value: 'verify-ca', label: 'Verify CA' },
    { value: 'verify-full', label: 'Verify Full' },
] as const;

/**
 * Form data structure for creating or editing a server connection.
 */
export interface ServerFormData {
    name: string;
    description: string;
    host: string;
    port: number | string;
    database: string;
    username: string;
    password: string;
    ssl_mode: string;
    ssl_cert_path: string;
    ssl_key_path: string;
    ssl_root_cert_path: string;
    is_monitored: boolean;
    is_shared: boolean;
}

/**
 * Server data structure received when editing an existing server.
 */
export interface ServerEditData {
    id?: number;
    name?: string;
    description?: string;
    host?: string;
    port?: number;
    database_name?: string;
    username?: string;
    ssl_mode?: string;
    ssl_cert_path?: string;
    ssl_key_path?: string;
    ssl_root_cert_path?: string;
    is_monitored?: boolean;
    is_shared?: boolean;
    [key: string]: unknown;
}

/**
 * Props for the ServerDialog component.
 */
export interface ServerDialogProps {
    open: boolean;
    onClose: () => void;
    onSave: (data: Record<string, unknown>) => Promise<void>;
    mode?: 'create' | 'edit';
    server?: ServerEditData | null;
    isSuperuser?: boolean;
}

/**
 * Returns default form values for a new server connection.
 */
export const getDefaultFormData = (): ServerFormData => ({
    name: '',
    description: '',
    host: '',
    port: 5432,
    database: 'postgres',
    username: 'postgres',
    password: '',
    ssl_mode: 'prefer',
    ssl_cert_path: '',
    ssl_key_path: '',
    ssl_root_cert_path: '',
    is_monitored: true,
    is_shared: false,
});

/**
 * Form validation error state type.
 */
export type FormErrors = Record<string, string>;

/**
 * Handler function type for form field changes.
 */
export type FieldChangeHandler = (
    field: keyof ServerFormData,
    value: string | number | boolean
) => void;
