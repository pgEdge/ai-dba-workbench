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
import { Fragment, useState, useEffect, useCallback } from 'react';
import {
    Box,
    Typography,
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    TablePagination,
    Paper,
    IconButton,
    CircularProgress,
    Alert,
    Chip,
    Tooltip,
    Collapse,
    TextField,
    MenuItem,
    Button,
} from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import type { Theme } from '@mui/material/styles';
import {
    KeyboardArrowDown as ExpandIcon,
    KeyboardArrowUp as CollapseIcon,
} from '@mui/icons-material';
import { apiFetch } from '../../utils/apiClient';
import {
    tableHeaderCellSx,
    pageHeadingSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    getTableContainerSx,
    getContainedButtonSx,
} from './styles';
import { extractErrorMessage } from './_shared';

/**
 * A single immutable RBAC audit event as returned by
 * `GET /api/v1/rbac/audit`. Optional fields are omitted by the server
 * when empty, so every one of them must be treated as absent rather
 * than blank.
 */
export interface AuditEvent {
    id: number;
    /** RFC 3339 timestamp of when the event was recorded. */
    occurred_at: string;
    actor_type: 'user' | 'token' | 'cli' | 'system';
    actor_id: number | null;
    actor_name: string;
    /** Source address of the request; omitted when not known. */
    actor_ip?: string;
    /** Dotted noun.verb action name, for example `group.delete`. */
    action: string;
    /** Omitted for denials, which have no resolved target. */
    target_type?: 'user' | 'token' | 'group';
    target_id: number | null;
    target_name?: string;
    outcome: 'success' | 'failure' | 'denied';
    /** Failure or denial reason; omitted on success. */
    error?: string;
    /** Arbitrary structured context; omitted when empty. */
    details?: Record<string, unknown>;
    prev_hash: string;
    hash: string;
}

/** Filter values bound to the filter bar controls. */
interface AuditFilters {
    actor: string;
    action: string;
    targetType: string;
    outcome: string;
    since: string;
    until: string;
}

const EMPTY_FILTERS: AuditFilters = {
    actor: '',
    action: '',
    targetType: '',
    outcome: '',
    since: '',
    until: '',
};

/** Target types the server recognises for the `target_type` filter. */
const TARGET_TYPE_OPTIONS = ['user', 'token', 'group'] as const;

/** Outcomes the server records for every audited operation. */
const OUTCOME_OPTIONS = ['success', 'failure', 'denied'] as const;

/** Page sizes offered by the pagination control. */
const ROWS_PER_PAGE_OPTIONS = [25, 50, 100];

/** Number of columns in the table, used for full-width rows. */
const COLUMN_COUNT = 6;

/**
 * Format an ISO timestamp for display, falling back to the raw value
 * when it cannot be parsed.
 */
function formatTimestamp(isoDate: string): string {
    const parsed = new Date(isoDate);
    if (Number.isNaN(parsed.getTime())) {
        return isoDate;
    }
    return parsed.toLocaleString(undefined, {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
}

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
function buildQuery(
    filters: AuditFilters,
    limit: number,
    offset: number,
): string {
    const params = new URLSearchParams();
    const since = toRFC3339(filters.since);
    const until = toRFC3339(filters.until);
    const optional: [string, string][] = [
        ['actor', filters.actor.trim()],
        ['action', filters.action.trim()],
        ['target_type', filters.targetType],
        ['outcome', filters.outcome],
        ['since', since],
        ['until', until],
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
 * Chip colours for each outcome: green for success, red for failure
 * and amber for a denial, all drawn from the theme so the contrast
 * holds in both light and dark modes.
 */
function outcomePalette(theme: Theme, outcome: AuditEvent['outcome']): string {
    if (outcome === 'success') {
        return theme.palette.success.main;
    }
    if (outcome === 'failure') {
        return theme.palette.error.main;
    }
    return theme.palette.warning.main;
}

/**
 * The name shown for an event's target, falling back to the numeric
 * id when the server recorded no name.
 */
function targetLabel(event: AuditEvent): string {
    if (event.target_name) {
        return event.target_name;
    }
    return event.target_id === null ? '' : `#${String(event.target_id)}`;
}

const AdminAuditLog: React.FC = () => {
    const theme = useTheme();
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

    const fetchEvents = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const query = buildQuery(applied, rowsPerPage, page * rowsPerPage);
            const response = await apiFetch(`/api/v1/rbac/audit?${query}`);
            if (!response.ok) {
                const body = await response.text();
                let message = `Request failed with status ${String(response.status)}`;
                try {
                    const parsed = JSON.parse(body) as { error?: string };
                    message = parsed.error ?? body ?? message;
                } catch {
                    message = body || message;
                }
                throw new Error(message);
            }
            const data = (await response.json()) as AuditEvent[] | null;
            const rows = data ?? [];
            setEvents(rows);
            const header = response.headers.get('X-Total-Count');
            const parsedTotal = header === null ? NaN : Number(header);
            setTotal(
                Number.isFinite(parsedTotal) && header !== null
                    ? parsedTotal
                    : rows.length,
            );
        } catch (err: unknown) {
            setEvents([]);
            setTotal(0);
            setError(extractErrorMessage(err, 'Failed to load audit events'));
        } finally {
            setLoading(false);
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

    const handleChangePage = (_event: unknown, newPage: number) => {
        setExpandedId(null);
        setPage(newPage);
    };

    const handleChangeRowsPerPage = (
        event: React.ChangeEvent<HTMLInputElement>,
    ) => {
        setExpandedId(null);
        setRowsPerPage(Number(event.target.value));
        setPage(0);
    };

    const tableContainerSx = getTableContainerSx(theme);
    const filterFieldSx = { minWidth: 160, flex: '1 1 160px' };

    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    Audit Log
                </Typography>
            </Box>

            <Box
                component="form"
                aria-label="Audit log filters"
                onSubmit={(event: React.FormEvent) => {
                    event.preventDefault();
                    handleApply();
                }}
                sx={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 2,
                    alignItems: 'center',
                    mb: 2,
                }}
            >
                <TextField
                    label="Actor"
                    size="small"
                    value={draft.actor}
                    onChange={(e) => { handleFilterChange('actor', e.target.value); }}
                    sx={filterFieldSx}
                />
                <TextField
                    label="Action"
                    size="small"
                    value={draft.action}
                    onChange={(e) => { handleFilterChange('action', e.target.value); }}
                    sx={filterFieldSx}
                />
                <TextField
                    select
                    label="Target type"
                    size="small"
                    value={draft.targetType}
                    onChange={(e) => { handleFilterChange('targetType', e.target.value); }}
                    sx={filterFieldSx}
                >
                    <MenuItem value="">Any</MenuItem>
                    {TARGET_TYPE_OPTIONS.map((value) => (
                        <MenuItem key={value} value={value}>
                            {value}
                        </MenuItem>
                    ))}
                </TextField>
                <TextField
                    select
                    label="Outcome"
                    size="small"
                    value={draft.outcome}
                    onChange={(e) => { handleFilterChange('outcome', e.target.value); }}
                    sx={filterFieldSx}
                >
                    <MenuItem value="">Any</MenuItem>
                    {OUTCOME_OPTIONS.map((value) => (
                        <MenuItem key={value} value={value}>
                            {value}
                        </MenuItem>
                    ))}
                </TextField>
                <TextField
                    label="Since"
                    type="datetime-local"
                    size="small"
                    value={draft.since}
                    onChange={(e) => { handleFilterChange('since', e.target.value); }}
                    InputLabelProps={{ shrink: true }}
                    sx={filterFieldSx}
                />
                <TextField
                    label="Until"
                    type="datetime-local"
                    size="small"
                    value={draft.until}
                    onChange={(e) => { handleFilterChange('until', e.target.value); }}
                    InputLabelProps={{ shrink: true }}
                    sx={filterFieldSx}
                />
                <Button
                    type="submit"
                    variant="contained"
                    sx={getContainedButtonSx(theme)}
                >
                    Apply
                </Button>
            </Box>

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
                <TableContainer
                    component={Paper}
                    elevation={0}
                    sx={tableContainerSx}
                >
                    <Table>
                        <TableHead>
                            <TableRow>
                                <TableCell sx={tableHeaderCellSx}>
                                    Time
                                </TableCell>
                                <TableCell sx={tableHeaderCellSx}>
                                    Actor
                                </TableCell>
                                <TableCell sx={tableHeaderCellSx}>
                                    Action
                                </TableCell>
                                <TableCell sx={tableHeaderCellSx}>
                                    Target
                                </TableCell>
                                <TableCell sx={tableHeaderCellSx}>
                                    Outcome
                                </TableCell>
                                <TableCell
                                    sx={tableHeaderCellSx}
                                    align="right"
                                >
                                    Details
                                </TableCell>
                            </TableRow>
                        </TableHead>
                        <TableBody>
                            {events.length === 0 && (
                                <TableRow>
                                    <TableCell
                                        colSpan={COLUMN_COUNT}
                                        align="center"
                                        sx={emptyRowSx}
                                    >
                                        <Typography
                                            color="text.secondary"
                                            sx={emptyRowTextSx}
                                        >
                                            No audit events match the
                                            current filters.
                                        </Typography>
                                    </TableCell>
                                </TableRow>
                            )}
                            {events.map((event) => {
                                const expanded = expandedId === event.id;
                                const outcomeColour = outcomePalette(
                                    theme,
                                    event.outcome,
                                );
                                return (
                                    <Fragment key={event.id}>
                                        <TableRow hover>
                                            <TableCell>
                                                <Typography variant="body2">
                                                    {formatTimestamp(
                                                        event.occurred_at,
                                                    )}
                                                </Typography>
                                            </TableCell>
                                            <TableCell>
                                                <Box
                                                    sx={{
                                                        display: 'flex',
                                                        alignItems: 'center',
                                                        gap: 1,
                                                    }}
                                                >
                                                    <Tooltip
                                                        title={
                                                            event.actor_ip
                                                                ? `IP: ${event.actor_ip}`
                                                                : ''
                                                        }
                                                    >
                                                        <Typography variant="body2">
                                                            {event.actor_name ||
                                                                '-'}
                                                        </Typography>
                                                    </Tooltip>
                                                    <Chip
                                                        label={event.actor_type}
                                                        size="small"
                                                        variant="outlined"
                                                    />
                                                </Box>
                                            </TableCell>
                                            <TableCell>
                                                <Typography variant="body2">
                                                    {event.action}
                                                </Typography>
                                            </TableCell>
                                            <TableCell>
                                                {event.target_type ? (
                                                    <Box
                                                        sx={{
                                                            display: 'flex',
                                                            alignItems: 'center',
                                                            gap: 1,
                                                        }}
                                                    >
                                                        <Chip
                                                            label={
                                                                event.target_type
                                                            }
                                                            size="small"
                                                            variant="outlined"
                                                        />
                                                        <Typography variant="body2">
                                                            {targetLabel(event)}
                                                        </Typography>
                                                    </Box>
                                                ) : (
                                                    <Typography variant="body2">
                                                        -
                                                    </Typography>
                                                )}
                                            </TableCell>
                                            <TableCell>
                                                <Chip
                                                    label={event.outcome}
                                                    size="small"
                                                    sx={{
                                                        bgcolor: alpha(
                                                            outcomeColour,
                                                            0.15,
                                                        ),
                                                        color: outcomeColour,
                                                        fontSize: '0.875rem',
                                                    }}
                                                />
                                            </TableCell>
                                            <TableCell align="right">
                                                <IconButton
                                                    size="small"
                                                    aria-label={
                                                        expanded
                                                            ? 'Hide details'
                                                            : 'Show details'
                                                    }
                                                    aria-expanded={expanded}
                                                    onClick={() => {
                                                        setExpandedId(
                                                            expanded
                                                                ? null
                                                                : event.id,
                                                        );
                                                    }}
                                                >
                                                    {expanded ? (
                                                        <CollapseIcon fontSize="small" />
                                                    ) : (
                                                        <ExpandIcon fontSize="small" />
                                                    )}
                                                </IconButton>
                                            </TableCell>
                                        </TableRow>
                                        <TableRow>
                                            <TableCell
                                                colSpan={COLUMN_COUNT}
                                                sx={{
                                                    py: 0,
                                                    borderBottom: expanded
                                                        ? undefined
                                                        : 'none',
                                                }}
                                            >
                                                <Collapse
                                                    in={expanded}
                                                    timeout="auto"
                                                    unmountOnExit
                                                >
                                                    <Box
                                                        data-testid={`audit-details-${String(event.id)}`}
                                                        sx={{ py: 2 }}
                                                    >
                                                        {event.error && (
                                                            <Typography
                                                                variant="body2"
                                                                color="error"
                                                                sx={{ mb: 1 }}
                                                            >
                                                                {event.error}
                                                            </Typography>
                                                        )}
                                                        {event.details ? (
                                                            <Box
                                                                component="pre"
                                                                sx={{
                                                                    m: 0,
                                                                    p: 1.5,
                                                                    bgcolor: alpha(
                                                                        theme
                                                                            .palette
                                                                            .grey[500],
                                                                        theme
                                                                            .palette
                                                                            .mode ===
                                                                            'dark'
                                                                            ? 0.3
                                                                            : 0.12,
                                                                    ),
                                                                    borderRadius: 1,
                                                                    overflowX: 'auto',
                                                                    fontSize: '0.875rem',
                                                                }}
                                                            >
                                                                {JSON.stringify(
                                                                    event.details,
                                                                    null,
                                                                    2,
                                                                )}
                                                            </Box>
                                                        ) : (
                                                            !event.error && (
                                                                <Typography
                                                                    variant="body2"
                                                                    color="text.secondary"
                                                                >
                                                                    No further
                                                                    detail was
                                                                    recorded.
                                                                </Typography>
                                                            )
                                                        )}
                                                    </Box>
                                                </Collapse>
                                            </TableCell>
                                        </TableRow>
                                    </Fragment>
                                );
                            })}
                        </TableBody>
                    </Table>
                    <TablePagination
                        component="div"
                        count={total}
                        page={page}
                        onPageChange={handleChangePage}
                        rowsPerPage={rowsPerPage}
                        onRowsPerPageChange={handleChangeRowsPerPage}
                        rowsPerPageOptions={ROWS_PER_PAGE_OPTIONS}
                    />
                </TableContainer>
            )}
        </Box>
    );
};

export default AdminAuditLog;
