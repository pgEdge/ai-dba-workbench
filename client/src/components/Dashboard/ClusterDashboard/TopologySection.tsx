/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo, useCallback } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import { alpha, useTheme, Theme } from '@mui/material/styles';
import { useClusterSelection } from '../../../contexts/ClusterSelectionContext';
import { ClusterServer } from '../../../contexts/ClusterDataContext';

interface TopologySectionProps {
    selection: Record<string, unknown>;
}

interface ServerCardData {
    id: number;
    name: string;
    role: string;
    status: string;
    host: string;
    port: number;
    version: string;
    raw: ClusterServer;
}

const TOPOLOGY_GRID_SX = {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))',
    gap: 2,
};

const CARD_HEADER_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    mb: 1,
};

const SERVER_NAME_SX = {
    fontWeight: 600,
    fontSize: '0.95rem',
    color: 'text.primary',
    lineHeight: 1.2,
    flex: 1,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const DETAIL_LABEL_SX = {
    fontSize: '0.75rem',
    color: 'text.secondary',
    fontWeight: 500,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
};

const DETAIL_VALUE_SX = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.8rem',
    color: 'text.primary',
    fontWeight: 500,
};

const DETAIL_ROW_SX = {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    py: 0.25,
};

const STATUS_DOT_SX = {
    width: 10,
    height: 10,
    borderRadius: '50%',
    flexShrink: 0,
};

const EMPTY_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 4,
};

/**
 * Determine the dot color for a given server status.
 */
const getStatusDotColor = (status: string, theme: Theme): string => {
    switch (status) {
        case 'online':
            return theme.palette.success.main;
        case 'warning':
            return theme.palette.warning.main;
        case 'offline':
            return theme.palette.error.main;
        default:
            return theme.palette.grey[500];
    }
};

/**
 * Determine a display-friendly role label.
 */
const getRoleLabel = (role: string): string => {
    const normalized = (role || '').toLowerCase();
    if (normalized === 'primary' || normalized === 'master') {
        return 'Primary';
    }
    if (normalized === 'standby' || normalized === 'replica' || normalized === 'subscriber') {
        return 'Standby';
    }
    if (normalized === 'writer') {
        return 'Writer';
    }
    if (normalized === 'reader') {
        return 'Reader';
    }
    if (!role || role === 'unknown') {
        return 'Standalone';
    }
    return role.charAt(0).toUpperCase() + role.slice(1);
};

/**
 * Build the card sx (clickable, themed).
 */
const getCardSx = (theme: Theme) => ({
    p: 2,
    borderRadius: 2,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[800], 0.8)
        : theme.palette.grey[50],
    border: '1px solid',
    borderColor: theme.palette.divider,
    cursor: 'pointer',
    transition: 'border-color 0.2s, box-shadow 0.2s',
    '&:hover': {
        borderColor: theme.palette.primary.main,
        boxShadow: `0 0 0 1px ${alpha(theme.palette.primary.main, 0.3)}`,
    },
});

/**
 * Collect server card data from selection, flattening children.
 */
const extractServerCards = (selection: Record<string, unknown>): ServerCardData[] => {
    const cards: ServerCardData[] = [];
    const servers = selection.servers as Array<Record<string, unknown>> | undefined;

    const collectServers = (serverList: Array<Record<string, unknown>> | undefined): void => {
        serverList?.forEach(s => {
            cards.push({
                id: s.id as number,
                name: s.name as string,
                role: (s.primary_role || s.role || 'unknown') as string,
                status: s.status as string || 'unknown',
                host: s.host as string || 'N/A',
                port: s.port as number || 5432,
                version: s.version as string || 'N/A',
                raw: s as unknown as ClusterServer,
            });
            if (s.children) {
                collectServers(s.children as Array<Record<string, unknown>>);
            }
        });
    };

    collectServers(servers);

    cards.sort((a, b) => {
        const roleOrder: Record<string, number> = {
            primary: 0,
            master: 0,
            writer: 1,
            standby: 2,
            replica: 2,
            reader: 3,
            subscriber: 3,
        };
        const aOrder = roleOrder[a.role.toLowerCase()] ?? 99;
        const bOrder = roleOrder[b.role.toLowerCase()] ?? 99;
        return aOrder - bOrder;
    });

    return cards;
};

/**
 * TopologySection displays the cluster topology as a vertical
 * card-based layout. Each server card shows the server name,
 * role badge, status indicator, host and port, and version.
 * Cards are clickable to select the server via
 * ClusterSelectionContext.
 */
const TopologySection: React.FC<TopologySectionProps> = ({ selection }) => {
    const theme = useTheme();
    const { selectServer } = useClusterSelection();

    const serverCards = useMemo(() => extractServerCards(selection), [selection]);
    const cardSx = useMemo(() => getCardSx(theme), [theme]);

    const handleServerClick = useCallback(
        (server: ClusterServer) => (): void => {
            selectServer(server);
        },
        [selectServer]
    );

    if (serverCards.length === 0) {
        return (
            <Typography sx={EMPTY_SX}>
                No servers found in this cluster.
            </Typography>
        );
    }

    return (
        <Box sx={TOPOLOGY_GRID_SX}>
            {serverCards.map(card => (
                <Paper
                    key={card.id}
                    elevation={0}
                    sx={cardSx}
                    onClick={handleServerClick(card.raw)}
                    role="button"
                    tabIndex={0}
                    aria-label={`Select server ${card.name}`}
                    onKeyDown={(e: React.KeyboardEvent) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            selectServer(card.raw);
                        }
                    }}
                >
                    <Box sx={CARD_HEADER_SX}>
                        <Box
                            sx={{
                                ...STATUS_DOT_SX,
                                bgcolor: getStatusDotColor(card.status, theme),
                            }}
                            aria-label={`Status: ${card.status}`}
                        />
                        <Typography sx={SERVER_NAME_SX}>
                            {card.name}
                        </Typography>
                        <Chip
                            label={getRoleLabel(card.role)}
                            size="small"
                            color={
                                card.role.toLowerCase() === 'primary' ||
                                card.role.toLowerCase() === 'master'
                                    ? 'primary'
                                    : 'default'
                            }
                            variant="outlined"
                            sx={{
                                height: 20,
                                fontSize: '0.7rem',
                                fontWeight: 600,
                            }}
                        />
                    </Box>

                    <Box sx={DETAIL_ROW_SX}>
                        <Typography sx={DETAIL_LABEL_SX}>Host</Typography>
                        <Typography sx={DETAIL_VALUE_SX}>
                            {card.host}:{card.port}
                        </Typography>
                    </Box>

                    <Box sx={DETAIL_ROW_SX}>
                        <Typography sx={DETAIL_LABEL_SX}>Version</Typography>
                        <Typography sx={DETAIL_VALUE_SX}>
                            {card.version}
                        </Typography>
                    </Box>
                </Paper>
            ))}
        </Box>
    );
};

export default TopologySection;
