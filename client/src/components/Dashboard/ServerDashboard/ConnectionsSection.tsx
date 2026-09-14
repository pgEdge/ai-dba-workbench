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
import { useCallback, useMemo, useState } from 'react';
import Box from '@mui/material/Box';
import Tab from '@mui/material/Tab';
import Tabs from '@mui/material/Tabs';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { Cable as CableIcon } from '@mui/icons-material';
import { useDashboard } from '../../../contexts/useDashboard';
import { useConnectionGroups } from '../../../hooks/useConnectionGroups';
import { formatNumber } from '../../../utils/formatters';
import { METRIC_LABEL_SX, MONO_CAPTION_SX } from '../../../theme/tokens';
import CollapsibleSection from '../CollapsibleSection';
import type {
    ConnectionGroupBy,
    ConnectionGroupRow,
    ServerSectionProps,
} from './types';

/** Grid template shared by the header row and the data rows. */
const GRID_TEMPLATE = '2fr repeat(5, minmax(0, 1fr))';

/** Scroll container for the grouping table. */
const TABLE_CONTAINER_SX = {
    overflowX: 'auto' as const,
    mb: 1,
};

/** Header row of the grouping table. */
const TABLE_HEADER_SX = {
    display: 'grid',
    gridTemplateColumns: GRID_TEMPLATE,
    gap: 1,
    px: 1.5,
    py: 1,
    borderBottom: '2px solid',
    borderColor: 'divider',
};

/** Data row of the grouping table. */
const TABLE_ROW_SX = {
    display: 'grid',
    gridTemplateColumns: GRID_TEMPLATE,
    gap: 1,
    px: 1.5,
    py: 1,
    alignItems: 'center',
    borderBottom: '1px solid',
    borderColor: 'divider',
    '&:last-child': {
        borderBottom: 'none',
    },
};

/** Header cell typography. */
const HEADER_CELL_SX = {
    ...METRIC_LABEL_SX,
    fontWeight: 700,
};

/** Right-aligned header cell typography for the numeric columns. */
const NUMERIC_HEADER_CELL_SX = {
    ...HEADER_CELL_SX,
    textAlign: 'right' as const,
};

/** Group label cell typography. */
const LABEL_CELL_SX = {
    ...MONO_CAPTION_SX,
    color: 'text.primary',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
};

/** Secondary line beneath a group label (client hostname). */
const SECONDARY_LABEL_SX = {
    ...MONO_CAPTION_SX,
    color: 'text.secondary',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
};

/** Numeric cell typography. */
const NUMERIC_CELL_SX = {
    ...MONO_CAPTION_SX,
    fontWeight: 500,
    color: 'text.primary',
    textAlign: 'right' as const,
};

/** Centred block used for the loading, error, and empty states. */
const MESSAGE_SX = { textAlign: 'center' as const, py: 3 };

/** The available groupings, in display order. */
const TAB_DEFS: {
    key: ConnectionGroupBy;
    tabLabel: string;
    columnLabel: string;
}[] = [
    { key: 'user', tabLabel: 'By User', columnLabel: 'User' },
    { key: 'client', tabLabel: 'By Client', columnLabel: 'Client' },
    { key: 'database', tabLabel: 'By Database', columnLabel: 'Database' },
];

/** Build the DOM id of a grouping tab. */
const tabId = (key: ConnectionGroupBy): string => `connections-tab-${key}`;

/** Build the DOM id of a grouping tab panel. */
const panelId = (key: ConnectionGroupBy): string =>
    `connections-tabpanel-${key}`;

/**
 * Format a snapshot timestamp for display. Returns null when the
 * timestamp is absent or unparseable, so the caller can omit the
 * "as of" line entirely rather than printing a placeholder.
 */
const formatSnapshotTime = (
    collectedAt: string | null,
): string | null => {
    if (!collectedAt) { return null; }
    const date = new Date(collectedAt);
    if (Number.isNaN(date.getTime())) { return null; }
    return date.toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
};

/**
 * The grouping table itself: a header row followed by one row per
 * group. The client grouping additionally shows the resolved hostname
 * beneath the client address when the collector recorded one.
 */
const ConnectionGroupTable: React.FC<{
    columnLabel: string;
    groups: ConnectionGroupRow[];
    showHostname: boolean;
}> = ({ columnLabel, groups, showHostname }) => (
    <Box sx={TABLE_CONTAINER_SX}>
        <Box sx={TABLE_HEADER_SX}>
            <Typography sx={HEADER_CELL_SX}>{columnLabel}</Typography>
            <Typography sx={NUMERIC_HEADER_CELL_SX}>Total</Typography>
            <Typography sx={NUMERIC_HEADER_CELL_SX}>Active</Typography>
            <Typography sx={NUMERIC_HEADER_CELL_SX}>Idle</Typography>
            <Typography sx={NUMERIC_HEADER_CELL_SX}>
                Idle in transaction
            </Typography>
            <Typography sx={NUMERIC_HEADER_CELL_SX}>Other</Typography>
        </Box>

        {groups.map((group, index) => (
            <Box
                key={group.group_label || `group-${index}`}
                sx={TABLE_ROW_SX}
            >
                <Box sx={{ minWidth: 0 }}>
                    <Typography sx={LABEL_CELL_SX} title={group.group_label}>
                        {group.group_label}
                    </Typography>
                    {showHostname && group.client_hostname && (
                        <Typography
                            sx={SECONDARY_LABEL_SX}
                            title={group.client_hostname}
                        >
                            {group.client_hostname}
                        </Typography>
                    )}
                </Box>
                <Typography sx={NUMERIC_CELL_SX}>
                    {formatNumber(group.total)}
                </Typography>
                <Typography sx={NUMERIC_CELL_SX}>
                    {formatNumber(group.active)}
                </Typography>
                <Typography sx={NUMERIC_CELL_SX}>
                    {formatNumber(group.idle)}
                </Typography>
                <Typography sx={NUMERIC_CELL_SX}>
                    {formatNumber(group.idle_in_transaction)}
                </Typography>
                <Typography sx={NUMERIC_CELL_SX}>
                    {formatNumber(group.other)}
                </Typography>
            </Box>
        ))}
    </Box>
);

/**
 * Connections section shows the active connections of a server broken
 * down by database user, client address, or database. The counts come
 * from the most recent snapshot the collector stored within the
 * selected dashboard time range, so the section reports a point in
 * time rather than an average over the period.
 */
const ConnectionsSection: React.FC<ServerSectionProps> = ({
    connectionId,
}) => {
    const { timeRange } = useDashboard();
    const [groupBy, setGroupBy] = useState<ConnectionGroupBy>('user');

    const params = useMemo(() => ({
        connectionId,
        groupBy,
        timeRange: timeRange.range,
    }), [connectionId, groupBy, timeRange.range]);

    const { groups, collectedAt, loading, error } =
        useConnectionGroups(params);

    const handleTabChange = useCallback((
        _event: React.SyntheticEvent,
        value: ConnectionGroupBy,
    ): void => {
        setGroupBy(value);
    }, []);

    const activeTab = TAB_DEFS.find(tab => tab.key === groupBy) ?? TAB_DEFS[0];
    const snapshotTime = formatSnapshotTime(collectedAt);
    const showTable = !loading && !error && groups.length > 0;
    const showEmpty = !loading && !error && groups.length === 0;

    return (
        <CollapsibleSection
            title="Connections"
            icon={<CableIcon sx={{ fontSize: 16 }} />}
            defaultExpanded
            storageKey="dashboard-section-connections-expanded"
        >
            <Tabs
                value={groupBy}
                onChange={handleTabChange}
                aria-label="Connection groupings"
                variant="scrollable"
                scrollButtons="auto"
                sx={{ borderBottom: '1px solid', borderColor: 'divider' }}
            >
                {TAB_DEFS.map(tab => (
                    <Tab
                        key={tab.key}
                        value={tab.key}
                        label={tab.tabLabel}
                        id={tabId(tab.key)}
                        aria-controls={panelId(tab.key)}
                    />
                ))}
            </Tabs>

            <Box
                role="tabpanel"
                id={panelId(activeTab.key)}
                aria-labelledby={tabId(activeTab.key)}
                sx={{ mt: 1 }}
            >
                {loading && (
                    <Box sx={{
                        display: 'flex',
                        justifyContent: 'center',
                        py: 3,
                    }}>
                        <CircularProgress
                            size={24}
                            aria-label="Loading connections"
                        />
                    </Box>
                )}

                {error && (
                    <Typography
                        variant="body2"
                        color="error"
                        sx={MESSAGE_SX}
                    >
                        {error}
                    </Typography>
                )}

                {showEmpty && (
                    <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={MESSAGE_SX}
                    >
                        No connection snapshots in the selected period.
                        This server may not have been collected yet, or
                        it may have had no client connections.
                    </Typography>
                )}

                {showTable && (
                    <ConnectionGroupTable
                        columnLabel={activeTab.columnLabel}
                        groups={groups}
                        showHostname={activeTab.key === 'client'}
                    />
                )}

                {snapshotTime && !error && (
                    <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ textAlign: 'right' }}
                    >
                        {`As of ${snapshotTime}`}
                    </Typography>
                )}
            </Box>
        </CollapsibleSection>
    );
};

export default ConnectionsSection;
