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
 * Disk information from the server.
 */
export interface DiskInfo {
    mount_point: string;
    filesystem_type: string;
    total_bytes: number;
    used_bytes: number;
    free_bytes: number;
}

/**
 * System and hardware information.
 */
export interface SystemInfo {
    os_name: string | null;
    os_version: string | null;
    architecture: string | null;
    hostname: string | null;
    cpu_model: string | null;
    cpu_cores: number | null;
    cpu_logical: number | null;
    cpu_clock_speed: number | null;
    memory_total_bytes: number | null;
    memory_used_bytes: number | null;
    memory_free_bytes: number | null;
    swap_total_bytes: number | null;
    swap_used_bytes: number | null;
    disks: DiskInfo[] | null;
}

/**
 * PostgreSQL server information.
 */
export interface PostgreSQLInfo {
    version: string | null;
    cluster_name: string | null;
    data_directory: string | null;
    max_connections: number | null;
    max_wal_senders: number | null;
    max_replication_slots: number | null;
}

/**
 * Database information item.
 */
export interface DatabaseInfoItem {
    name: string;
    size_bytes: number | null;
    encoding: string | null;
    connection_limit: number | null;
    extensions: string[] | null;
}

/**
 * Extension information item.
 */
export interface ExtensionInfoItem {
    name: string;
    version: string | null;
    schema: string | null;
    database: string;
}

/**
 * Setting information item.
 */
export interface SettingInfoItem {
    name: string;
    setting: string | null;
    unit?: string | null;
    category: string | null;
}

/**
 * AI analysis information.
 */
export interface AIAnalysisInfo {
    databases: Record<string, string>;
    generated_at: string;
}

/**
 * Complete server information response from the API.
 */
export interface ServerInfoResponse {
    connection_id: number;
    collected_at: string | null;
    system: SystemInfo | null;
    postgresql: PostgreSQLInfo | null;
    databases: DatabaseInfoItem[] | null;
    extensions: ExtensionInfoItem[] | null;
    key_settings: SettingInfoItem[] | null;
    ai_analysis: AIAnalysisInfo | null;
}

/**
 * Props for the ServerInfoDialog component.
 */
export interface ServerInfoDialogProps {
    open: boolean;
    onClose: () => void;
    connectionId: number;
    serverName: string;
}
