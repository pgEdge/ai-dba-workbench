/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useEffect, useCallback, useRef } from 'react';
import { Box, Typography, CircularProgress, Alert } from '@mui/material';
import { apiFetch } from '../../utils/apiClient';
import { pageHeadingSx, loadingContainerSx } from './styles';
import { extractErrorMessage } from './_shared';
import type { AuditEvent, AuditFilters } from './AdminAuditLogParts';
import {
    AuditFilterBar,
    AuditTable,
    EMPTY_FILTERS,
    ROWS_PER_PAGE_OPTIONS,
} from './AdminAuditLogParts';

export type { AuditEvent } from './AdminAuditLogParts';

/**
 * Convert a `datetime-local` input value (local wall-clock time, with
 * no zone) into the RFC 3339 string the API expects. Returns an empty
 * string for blank or unparseable input so the caller can drop the
 * parameter entirely.
 */
function toRFC3339(localValue: string): string {
    if (!localValue) {
        return '';
    }
    const parsed = new Date(localValue);
    if (Number.isNaN(parsed.getTime())) {
        return '';
    }
    return parsed.toISOString();
}

/**
 * Build the audit query string from the applied filters and the
 * current page position. Blank filters are omitted rather than sent as
 * empty values.
 */
function buildQuery(filters: AuditFilters, limit: number, offset: number): string {
    const params = new URLSearchParams();
    const optional: [string, string][] = [
        ['actor', filters.actor.trim()],
        ['action', filters.action.trim()],
        ['target_type', filters.targetType],
        ['outcome', filters.outcome],
        ['since', toRFC3339(filters.since)],
        ['until', toRFC3339(filters.until)],
    ];
    optional.forEach(([key, value]) => {
        if (value) {
            params.set(key, value);
        }
    });
    params.set('limit', String(limit));
    params.set('offset', String(offset));
    return params.toString();
}

/**
 * Extract the message from a failed audit response, preferring the
 * `error` field of a JSON body, then the raw body, and finally the
 * HTTP status when the body is empty or unreadable.
 */
async function readErrorMessage(response: Response): Promise<string> {
    const fallback = `Request failed with status ${String(response.status)}`;
    const body = await response.text();
    try {
        const parsed = JSON.parse(body) as { error?: string };
        return truncateMessage(parsed.error ?? body ?? fallback);
    } catch {
        return truncateMessage(body || fallback);
    }
}

/**
 * Longest error message shown in the banner. A proxy or a crashed
 * upstream can answer with a whole HTML page, and the banner is not the
 * place to render it.
 */
export const MAX_ERROR_MESSAGE_LENGTH = 200;

/** Cut a message to MAX_ERROR_MESSAGE_LENGTH, marking the cut. */
function truncateMessage(message: string): string {
    if (message.length <= MAX_ERROR_MESSAGE_LENGTH) {
        return message;
    }
    return `${message.slice(0, MAX_ERROR_MESSAGE_LENGTH)}\u2026`;
}

/**
 * Read the total match count from the `X-Total-Count` header, falling
 * back to the number of rows returned when the header is missing or
 * not a finite number.
 */
function readTotalCount(response: Response, rowCount: number): number {
    const header = response.headers.get('X-Total-Count');
    if (header === null) {
        return rowCount;
    }
    const parsed = Number(header);
    return Number.isFinite(parsed) ? parsed : rowCount;
}

const AdminAuditLog: React.FC = () => {
    const [events, setEvents] = useState<AuditEvent[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedId, setExpandedId] = useState<number | null>(null);

    // `draft` holds what the user is typing; `applied` is what the last
    // Apply click committed, so keystrokes do not fire requests.
    const [draft, setDraft] = useState<AuditFilters>(EMPTY_FILTERS);
    const [applied, setApplied] = useState<AuditFilters>(EMPTY_FILTERS);
    const [page, setPage] = useState(0);
    const [rowsPerPage, setRowsPerPage] = useState(ROWS_PER_PAGE_OPTIONS[0]);
    // Bumped on every Apply so that re-applying unchanged filters still
    // refreshes the list rather than silently doing nothing.
    const [refreshToken, setRefreshToken] = useState(0);

    // Identifies the most recently started request, so that a slower
    // earlier response cannot overwrite a newer one or clear the
    // loading flag whilst the newer request is still in flight.
    const requestIdRef = useRef(0);

    const fetchEvents = useCallback(async () => {
        const requestId = requestIdRef.current + 1;
        requestIdRef.current = requestId;
        const isCurrent = () => requestIdRef.current === requestId;
        setLoading(true);
        setError(null);
        try {
            const query = buildQuery(applied, rowsPerPage, page * rowsPerPage);
            const response = await apiFetch(`/api/v1/rbac/audit?${query}`);
            if (!isCurrent()) {
                return;
            }
            if (!response.ok) {
                const message = await readErrorMessage(response);
                if (!isCurrent()) {
                    return;
                }
                throw new Error(message);
            }
            const data = (await response.json()) as AuditEvent[] | null;
            if (!isCurrent()) {
                return;
            }
            const rows = data ?? [];
            setEvents(rows);
            setTotal(readTotalCount(response, rows.length));
        } catch (err: unknown) {
            if (!isCurrent()) {
                return;
            }
            setEvents([]);
            setTotal(0);
            setError(extractErrorMessage(err, 'Failed to load audit events'));
        } finally {
            if (isCurrent()) {
                setLoading(false);
            }
        }
        // refreshToken is not read here; it exists only to re-run the
        // request when Apply is clicked with unchanged filters.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [applied, page, rowsPerPage, refreshToken]);

    useEffect(() => {
        void fetchEvents();
    }, [fetchEvents]);

    const handleFilterChange = (field: keyof AuditFilters, value: string) => {
        setDraft((prev) => ({ ...prev, [field]: value }));
    };

    const handleApply = () => {
        setExpandedId(null);
        setPage(0);
        setApplied(draft);
        setRefreshToken((token) => token + 1);
    };

    const handleToggleExpanded = (event: AuditEvent) => {
        setExpandedId((current) => (current === event.id ? null : event.id));
    };

    const handleChangePage = (_event: unknown, newPage: number) => {
        setExpandedId(null);
        setPage(newPage);
    };

    const handleChangeRowsPerPage = (event: React.ChangeEvent<HTMLInputElement>) => {
        setExpandedId(null);
        setRowsPerPage(Number(event.target.value));
        setPage(0);
    };

    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    Audit Log
                </Typography>
            </Box>

            <AuditFilterBar draft={draft} onChange={handleFilterChange} onApply={handleApply} />

            {error && (
                <Alert severity="error" sx={{ mb: 2 }}>
                    {error}
                </Alert>
            )}

            {loading ? (
                <Box sx={loadingContainerSx}>
                    <CircularProgress />
                </Box>
            ) : (
                <AuditTable
                    events={events}
                    expandedId={expandedId}
                    onToggle={handleToggleExpanded}
                    total={total}
                    page={page}
                    rowsPerPage={rowsPerPage}
                    onPageChange={handleChangePage}
                    onRowsPerPageChange={handleChangeRowsPerPage}
                />
            )}
        </Box>
    );
};

export default AdminAuditLog;
