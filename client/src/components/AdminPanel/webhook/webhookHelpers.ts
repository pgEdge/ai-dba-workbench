/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { HeaderEntry } from './webhookTypes';

/** Supported HTTP methods for webhook channels. */
export const HTTP_METHODS = ['POST', 'GET', 'PUT', 'PATCH'];

/** Authentication type options for the webhook auth select. */
export const AUTH_TYPES = [
    { value: 'none', label: 'None' },
    { value: 'basic', label: 'Basic' },
    { value: 'bearer', label: 'Bearer Token' },
    { value: 'api_key', label: 'API Key' },
];

/** Default template for alert fire notifications. */
export const DEFAULT_ALERT_FIRE_TEMPLATE = `{
  "event": "alert_fire",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "description": "{{.AlertDescription}}",
  "severity": "{{.Severity}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  {{- if .DatabaseName}}
  "database": "{{.DatabaseName}}",
  {{- end}}
  {{- if .MetricName}}
  "metric": {
    "name": "{{.MetricName}}"
    {{- if .MetricValue}}, "value": {{.MetricValue}}{{end}}
    {{- if .ThresholdValue}}, "threshold": {{.ThresholdValue}}{{end}}
    {{- if .Operator}}, "operator": "{{.Operator}}"{{end}}
  },
  {{- end}}
  "triggered_at": "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}"
}`;

/** Default template for alert clear notifications. */
export const DEFAULT_ALERT_CLEAR_TEMPLATE = `{
  "event": "alert_clear",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  "triggered_at": "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}"
  {{- if .ClearedAt}},
  "cleared_at": "{{.ClearedAt.Format "2006-01-02T15:04:05Z07:00"}}"
  {{- end}},
  "duration": "{{.Duration}}"
}`;

/** Default template for reminder notifications. */
export const DEFAULT_REMINDER_TEMPLATE = `{
  "event": "reminder",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "description": "{{.AlertDescription}}",
  "severity": "{{.Severity}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  "triggered_at": "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}",
  "reminder_count": {{.ReminderCount}}
}`;

/**
 * Parse auth_credentials based on auth_type into individual fields.
 *
 * @param authType - The authentication type (basic, bearer, api_key, etc.)
 * @param credentials - The raw credentials string from the API
 * @returns An object with parsed credential fields
 */
export const parseAuthCredentials = (
    authType: string,
    credentials: string,
): Record<string, string> => {
    switch (authType) {
        case 'basic': {
            const separatorIndex = credentials.indexOf(':');
            if (separatorIndex === -1) {
                return { username: credentials, password: '' };
            }
            return {
                username: credentials.substring(0, separatorIndex),
                password: credentials.substring(separatorIndex + 1),
            };
        }
        case 'bearer':
            return { token: credentials };
        case 'api_key': {
            const separatorIndex = credentials.indexOf(':');
            if (separatorIndex === -1) {
                return { headerName: credentials, apiKeyValue: '' };
            }
            return {
                headerName: credentials.substring(0, separatorIndex),
                apiKeyValue: credentials.substring(separatorIndex + 1),
            };
        }
        default:
            return {};
    }
};

/**
 * Build auth_credentials string from individual fields based on auth_type.
 *
 * @param authType - The authentication type (basic, bearer, api_key, etc.)
 * @param fields - An object with credential field values
 * @returns The combined credentials string for the API
 */
export const buildAuthCredentials = (
    authType: string,
    fields: Record<string, string>,
): string => {
    switch (authType) {
        case 'basic':
            return `${fields.username || ''}:${fields.password || ''}`;
        case 'bearer':
            return fields.token || '';
        case 'api_key':
            return `${fields.headerName || ''}:${fields.apiKeyValue || ''}`;
        default:
            return '';
    }
};

/**
 * Convert a headers object from the API into an array of HeaderEntry objects.
 * Each entry receives a unique ID for use as a React key.
 *
 * @param headers - The headers object from the API
 * @returns An array of HeaderEntry objects with generated IDs
 */
export const headersObjectToArray = (
    headers: Record<string, string>,
): HeaderEntry[] => {
    const entries = Object.entries(headers);
    if (entries.length === 0) {
        return [];
    }
    return entries.map(([key, value]) => ({
        id: crypto.randomUUID(),
        key,
        value,
    }));
};

/**
 * Convert an array of HeaderEntry objects into a headers object for the API.
 * Filters out entries with blank keys and trims key names.
 *
 * @param headers - The array of HeaderEntry objects
 * @returns A headers object suitable for the API
 */
export const headersArrayToObject = (
    headers: HeaderEntry[],
): Record<string, string> => {
    return headers.reduce<Record<string, string>>((acc, h) => {
        const trimmedKey = h.key.trim();
        if (trimmedKey) {
            acc[trimmedKey] = h.value;
        }
        return acc;
    }, {});
};
