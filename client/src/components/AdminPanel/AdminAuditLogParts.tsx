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
import { Fragment } from 'react';
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
import {
    tableHeaderCellSx,
    emptyRowSx,
    emptyRowTextSx,
    getTableContainerSx,
    getContainedButtonSx,
} from './styles';

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
export interface AuditFilters {
    actor: string;
    action: string;
    targetType: string;
    outcome: string;
    since: string;
    until: string;
}

export const EMPTY_FILTERS: AuditFilters = {
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
export const ROWS_PER_PAGE_OPTIONS = [25, 50, 100];

/** Number of columns in the table, used for full-width rows. */
const COLUMN_COUNT = 6;

/** Column headings, in the order the table renders them. */
const COLUMN_HEADINGS = [
    'Time',
    'Actor',
    'Action',
    'Target',
    'Outcome',
] as const;

const filterFieldSx = { minWidth: 160, flex: '1 1 160px' };

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

/** The background and text colours used for one outcome chip. */
interface OutcomeChipColours {
    /** Translucent chip background. */
    background: string;
    /** Chip label colour, at least 4.5:1 against `background`. */
    color: string;
}

/**
 * Chip colours for each outcome: green for success, red for failure
 * and amber for a denial. The background is the translucent status
 * colour used by status chips elsewhere in the client, whilst the
 * label takes the mode-specific `palette.custom.chipText` shade, which
 * is chosen to clear the WCAG AA 4.5:1 ratio against that background
 * in both light and dark modes. Using the status colour itself for the
 * label would leave text and background sharing a hue at roughly 2:1.
 */
function outcomePalette(
    theme: Theme,
    outcome: AuditEvent['outcome'],
): OutcomeChipColours {
    if (outcome === 'success') {
        return {
            background: alpha(theme.palette.success.main, 0.15),
            color: theme.palette.custom.chipText.success,
        };
    }
    if (outcome === 'failure') {
        return {
            background: alpha(theme.palette.error.main, 0.15),
            color: theme.palette.custom.chipText.error,
        };
    }
    return {
        background: alpha(theme.palette.warning.main, 0.15),
        color: theme.palette.custom.chipText.warning,
    };
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

interface AuditSelectFieldProps {
    /** Field label shown above the control. */
    label: string;
    /** Currently selected value; empty string means "Any". */
    value: string;
    /** Non-empty values offered below the "Any" option. */
    options: readonly string[];
    /** Called with the new value whenever the selection changes. */
    onChange: (value: string) => void;
}

/** A filter-bar dropdown offering "Any" plus a fixed list of values. */
const AuditSelectField: React.FC<AuditSelectFieldProps> = ({
    label,
    value,
    options,
    onChange,
}) => (
    <TextField
        select
        label={label}
        size="small"
        value={value}
        onChange={(e) => { onChange(e.target.value); }}
        sx={filterFieldSx}
    >
        <MenuItem value="">Any</MenuItem>
        {options.map((option) => (
            <MenuItem key={option} value={option}>
                {option}
            </MenuItem>
        ))}
    </TextField>
);

interface AuditFilterBarProps {
    /** The values currently typed into the controls. */
    draft: AuditFilters;
    /** Called with the field and its new value on every keystroke. */
    onChange: (field: keyof AuditFilters, value: string) => void;
    /** Called when the form is submitted by the Apply button. */
    onApply: () => void;
}

/** The filter form above the audit table. */
export const AuditFilterBar: React.FC<AuditFilterBarProps> = ({
    draft,
    onChange,
    onApply,
}) => {
    const theme = useTheme();
    return (
        <Box
            component="form"
            aria-label="Audit log filters"
            onSubmit={(event: React.FormEvent) => {
                event.preventDefault();
                onApply();
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
                onChange={(e) => { onChange('actor', e.target.value); }}
                sx={filterFieldSx}
            />
            <TextField
                label="Action"
                size="small"
                value={draft.action}
                onChange={(e) => { onChange('action', e.target.value); }}
                sx={filterFieldSx}
            />
            <AuditSelectField
                label="Target type"
                value={draft.targetType}
                options={TARGET_TYPE_OPTIONS}
                onChange={(value) => { onChange('targetType', value); }}
            />
            <AuditSelectField
                label="Outcome"
                value={draft.outcome}
                options={OUTCOME_OPTIONS}
                onChange={(value) => { onChange('outcome', value); }}
            />
            <TextField
                label="Since"
                type="datetime-local"
                size="small"
                value={draft.since}
                onChange={(e) => { onChange('since', e.target.value); }}
                InputLabelProps={{ shrink: true }}
                sx={filterFieldSx}
            />
            <TextField
                label="Until"
                type="datetime-local"
                size="small"
                value={draft.until}
                onChange={(e) => { onChange('until', e.target.value); }}
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
    );
};

/** The actor name, its tooltip of the source address, and its type. */
const AuditActorCell: React.FC<{ event: AuditEvent }> = ({ event }) => (
    <TableCell>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Tooltip title={event.actor_ip ? `IP: ${event.actor_ip}` : ''}>
                <Typography variant="body2">
                    {event.actor_name || '-'}
                </Typography>
            </Tooltip>
            <Chip label={event.actor_type} size="small" variant="outlined" />
        </Box>
    </TableCell>
);

/** The target type chip and name, or a dash when there is no target. */
const AuditTargetCell: React.FC<{ event: AuditEvent }> = ({ event }) => (
    <TableCell>
        {event.target_type ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <Chip
                    label={event.target_type}
                    size="small"
                    variant="outlined"
                />
                <Typography variant="body2">{targetLabel(event)}</Typography>
            </Box>
        ) : (
            <Typography variant="body2">-</Typography>
        )}
    </TableCell>
);

/** The expandable panel holding an event's error text and details. */
const AuditDetailPanel: React.FC<{ event: AuditEvent }> = ({ event }) => {
    const theme = useTheme();
    return (
        <Box
            data-testid={`audit-details-${String(event.id)}`}
            sx={{ py: 2 }}
        >
            {event.error && (
                <Typography variant="body2" color="error" sx={{ mb: 1 }}>
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
                            theme.palette.grey[500],
                            theme.palette.mode === 'dark' ? 0.3 : 0.12,
                        ),
                        borderRadius: 1,
                        overflowX: 'auto',
                        fontSize: '0.875rem',
                    }}
                >
                    {JSON.stringify(event.details, null, 2)}
                </Box>
            ) : (
                !event.error && (
                    <Typography variant="body2" color="text.secondary">
                        No further detail was recorded.
                    </Typography>
                )
            )}
        </Box>
    );
};

interface AuditEventRowProps {
    event: AuditEvent;
    /** Whether this row's detail panel is currently open. */
    expanded: boolean;
    /** Called when the expander button is clicked. */
    onToggle: (event: AuditEvent) => void;
}

/** One audit event, plus the collapsible detail row beneath it. */
export const AuditEventRow: React.FC<AuditEventRowProps> = ({
    event,
    expanded,
    onToggle,
}) => {
    const theme = useTheme();
    const outcomeColours = outcomePalette(theme, event.outcome);
    return (
        <Fragment>
            <TableRow hover>
                <TableCell>
                    <Typography variant="body2">
                        {formatTimestamp(event.occurred_at)}
                    </Typography>
                </TableCell>
                <AuditActorCell event={event} />
                <TableCell>
                    <Typography variant="body2">{event.action}</Typography>
                </TableCell>
                <AuditTargetCell event={event} />
                <TableCell>
                    <Chip
                        label={event.outcome}
                        size="small"
                        sx={{
                            bgcolor: outcomeColours.background,
                            color: outcomeColours.color,
                            fontSize: '0.875rem',
                        }}
                    />
                </TableCell>
                <TableCell align="right">
                    <IconButton
                        size="small"
                        aria-label={
                            expanded ? 'Hide details' : 'Show details'
                        }
                        aria-expanded={expanded}
                        onClick={() => { onToggle(event); }}
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
                        borderBottom: expanded ? undefined : 'none',
                    }}
                >
                    <Collapse in={expanded} timeout="auto" unmountOnExit>
                        <AuditDetailPanel event={event} />
                    </Collapse>
                </TableCell>
            </TableRow>
        </Fragment>
    );
};

/** The row shown in place of the events when none match the filters. */
const AuditEmptyRow: React.FC = () => (
    <TableRow>
        <TableCell colSpan={COLUMN_COUNT} align="center" sx={emptyRowSx}>
            <Typography color="text.secondary" sx={emptyRowTextSx}>
                No audit events match the current filters.
            </Typography>
        </TableCell>
    </TableRow>
);

interface AuditTableProps {
    events: AuditEvent[];
    /** The id of the row whose detail panel is open, if any. */
    expandedId: number | null;
    onToggle: (event: AuditEvent) => void;
    /** Total number of matching events, for the pagination control. */
    total: number;
    page: number;
    rowsPerPage: number;
    onPageChange: (event: unknown, newPage: number) => void;
    onRowsPerPageChange: (
        event: React.ChangeEvent<HTMLInputElement>,
    ) => void;
}

/** The audit event table and its pagination control. */
export const AuditTable: React.FC<AuditTableProps> = ({
    events,
    expandedId,
    onToggle,
    total,
    page,
    rowsPerPage,
    onPageChange,
    onRowsPerPageChange,
}) => {
    const theme = useTheme();
    return (
        <TableContainer
            component={Paper}
            elevation={0}
            sx={getTableContainerSx(theme)}
        >
            <Table>
                <TableHead>
                    <TableRow>
                        {COLUMN_HEADINGS.map((heading) => (
                            <TableCell key={heading} sx={tableHeaderCellSx}>
                                {heading}
                            </TableCell>
                        ))}
                        <TableCell sx={tableHeaderCellSx} align="right">
                            Details
                        </TableCell>
                    </TableRow>
                </TableHead>
                <TableBody>
                    {events.length === 0 && <AuditEmptyRow />}
                    {events.map((event) => (
                        <AuditEventRow
                            key={event.id}
                            event={event}
                            expanded={expandedId === event.id}
                            onToggle={onToggle}
                        />
                    ))}
                </TableBody>
            </Table>
            <TablePagination
                component="div"
                count={total}
                page={page}
                onPageChange={onPageChange}
                rowsPerPage={rowsPerPage}
                onRowsPerPageChange={onRowsPerPageChange}
                rowsPerPageOptions={ROWS_PER_PAGE_OPTIONS}
            />
        </TableContainer>
    );
};
