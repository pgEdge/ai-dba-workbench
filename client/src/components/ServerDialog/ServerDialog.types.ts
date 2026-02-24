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
    onSave: (data: Record<string, unknown>) => Promise<unknown>;
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

/**
 * Summary of a cluster returned by the cluster list API.
 */
export interface ClusterSummary {
    id: number;
    name: string;
    replication_type: string | null;
    auto_cluster_key: string | null;
}

/**
 * Cluster information associated with a connection.
 */
export interface ConnectionClusterInfo {
    cluster_id: number | null;
    role: string | null;
    cluster_override: boolean;
    cluster_name: string | null;
    replication_type: string | null;
    auto_cluster_key: string | null;
}

/**
 * Form data for creating a new cluster inline.
 */
export interface NewClusterFormData {
    name: string;
    replication_type: string;
}

/**
 * Value managed by the ClusterFields component.
 */
export interface ClusterFieldsValue {
    clusterId: number | null;
    role: string | null;
    clusterOverride: boolean;
    newCluster?: NewClusterFormData;
}

/**
 * A node-to-node relationship within a cluster.
 */
export interface NodeRelationship {
    id: number;
    cluster_id: number;
    source_connection_id: number;
    target_connection_id: number;
    source_name: string;
    target_name: string;
    relationship_type: string;
    is_auto_detected: boolean;
}

/**
 * Input payload for creating a relationship.
 */
export interface RelationshipInput {
    target_connection_id: number;
    relationship_type: string;
}

/**
 * Summary of a server within a cluster.
 */
export interface ClusterServerInfo {
    id: number;
    name: string;
    host: string;
    port: number;
    status: string;
    role?: string;
    database_name?: string;
}
