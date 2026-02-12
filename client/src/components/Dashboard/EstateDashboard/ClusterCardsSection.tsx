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
import { ClusterEntry } from '../../../contexts/ClusterDataContext';

interface ClusterCardsSectionProps {
    selection: Record<string, unknown>;
}

interface ClusterCardData {
    cluster: ClusterEntry;
    name: string;
    serverCount: number;
    online: number;
    warning: number;
    offline: number;
    roles: Record<string, number>;
    alertCount: number;
}

const CARDS_GRID_SX = {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
    gap: 2,
};

const CARD_HEADER_SX = {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    mb: 1.5,
};

const CARD_NAME_SX = {
    fontWeight: 600,
    fontSize: '1rem',
    color: 'text.primary',
    lineHeight: 1.2,
};

const STATUS_ROW_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 1.5,
    mb: 1,
};

const STATUS_ITEM_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
};

const STATUS_DOT_SX = {
    width: 8,
    height: 8,
    borderRadius: '50%',
};

const STATUS_LABEL_SX = {
    fontSize: '0.75rem',
    color: 'text.secondary',
    fontWeight: 500,
};

const ROLES_ROW_SX = {
    display: 'flex',
    flexWrap: 'wrap',
    gap: 0.5,
    mt: 1,
};

const SERVER_COUNT_SX = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.75rem',
    fontWeight: 600,
    color: 'text.secondary',
};

const EMPTY_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 4,
};

/**
 * Build a paper sx that is clickable and themed.
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
 * Collect servers recursively including children.
 */
const collectAllServers = (servers: Array<Record<string, unknown>>): Array<Record<string, unknown>> => {
    const result: Array<Record<string, unknown>> = [];
    servers.forEach(s => {
        result.push(s);
        if (s.children) {
            result.push(...collectAllServers(s.children as Array<Record<string, unknown>>));
        }
    });
    return result;
};

/**
 * Extract cluster card data from the estate selection hierarchy.
 */
const extractClusterCards = (selection: Record<string, unknown>): ClusterCardData[] => {
    const cards: ClusterCardData[] = [];
    const groups = selection.groups as Array<Record<string, unknown>> | undefined;

    groups?.forEach(group => {
        const clusters = group.clusters as Array<Record<string, unknown>> | undefined;
        clusters?.forEach(clusterObj => {
            const rawServers = (clusterObj.servers || []) as Array<Record<string, unknown>>;
            const allServers = collectAllServers(rawServers);

            let online = 0;
            let warning = 0;
            let offline = 0;
            let alertCount = 0;
            const roles: Record<string, number> = {};

            allServers.forEach(s => {
                const status = s.status as string;
                const alerts = s.active_alert_count as number | undefined;

                if (status === 'offline') {
                    offline += 1;
                } else if (alerts && alerts > 0) {
                    warning += 1;
                    alertCount += alerts;
                } else {
                    online += 1;
                }

                const role = (s.role || s.primary_role || 'unknown') as string;
                roles[role] = (roles[role] || 0) + 1;
            });

            const cluster: ClusterEntry = {
                id: clusterObj.id as string,
                name: clusterObj.name as string,
                servers: rawServers as unknown as ClusterEntry['servers'],
                isStandalone: clusterObj.isStandalone as boolean | undefined,
            };

            cards.push({
                cluster,
                name: clusterObj.name as string,
                serverCount: allServers.length,
                online,
                warning,
                offline,
                roles,
                alertCount,
            });
        });
    });

    return cards;
};

/**
 * ClusterCardsSection shows a grid of cards, one per cluster.
 * Each card shows the cluster name, server status summary,
 * role distribution, and alert badge count. Cards are clickable
 * to select the cluster via ClusterSelectionContext.
 */
const ClusterCardsSection: React.FC<ClusterCardsSectionProps> = ({ selection }) => {
    const theme = useTheme();
    const { selectCluster } = useClusterSelection();

    const clusterCards = useMemo(() => extractClusterCards(selection), [selection]);

    const cardSx = useMemo(() => getCardSx(theme), [theme]);

    const handleClusterClick = useCallback(
        (cluster: ClusterEntry) => (): void => {
            selectCluster(cluster);
        },
        [selectCluster]
    );

    if (clusterCards.length === 0) {
        return (
            <Typography sx={EMPTY_SX}>
                No clusters found in the estate.
            </Typography>
        );
    }

    return (
        <Box sx={CARDS_GRID_SX}>
            {clusterCards.map(card => (
                <Paper
                    key={card.cluster.id}
                    elevation={0}
                    sx={cardSx}
                    onClick={handleClusterClick(card.cluster)}
                    role="button"
                    tabIndex={0}
                    aria-label={`Select cluster ${card.name}`}
                    onKeyDown={(e: React.KeyboardEvent) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            selectCluster(card.cluster);
                        }
                    }}
                >
                    <Box sx={CARD_HEADER_SX}>
                        <Box>
                            <Typography sx={CARD_NAME_SX}>
                                {card.name}
                            </Typography>
                            <Typography sx={SERVER_COUNT_SX}>
                                {card.serverCount} server{card.serverCount !== 1 ? 's' : ''}
                            </Typography>
                        </Box>
                        {card.alertCount > 0 && (
                            <Chip
                                label={card.alertCount}
                                size="small"
                                color="warning"
                                sx={{
                                    height: 22,
                                    fontSize: '0.75rem',
                                    fontWeight: 700,
                                }}
                            />
                        )}
                    </Box>

                    <Box sx={STATUS_ROW_SX}>
                        <Box sx={STATUS_ITEM_SX}>
                            <Box
                                sx={{
                                    ...STATUS_DOT_SX,
                                    bgcolor: theme.palette.success.main,
                                }}
                            />
                            <Typography sx={STATUS_LABEL_SX}>
                                {card.online} online
                            </Typography>
                        </Box>
                        {card.warning > 0 && (
                            <Box sx={STATUS_ITEM_SX}>
                                <Box
                                    sx={{
                                        ...STATUS_DOT_SX,
                                        bgcolor: theme.palette.warning.main,
                                    }}
                                />
                                <Typography sx={STATUS_LABEL_SX}>
                                    {card.warning} warning
                                </Typography>
                            </Box>
                        )}
                        {card.offline > 0 && (
                            <Box sx={STATUS_ITEM_SX}>
                                <Box
                                    sx={{
                                        ...STATUS_DOT_SX,
                                        bgcolor: theme.palette.error.main,
                                    }}
                                />
                                <Typography sx={STATUS_LABEL_SX}>
                                    {card.offline} offline
                                </Typography>
                            </Box>
                        )}
                    </Box>

                    {Object.keys(card.roles).length > 0 && (
                        <Box sx={ROLES_ROW_SX}>
                            {Object.entries(card.roles).map(([role, count]) => (
                                <Chip
                                    key={role}
                                    label={`${role}: ${count}`}
                                    size="small"
                                    variant="outlined"
                                    sx={{
                                        height: 20,
                                        fontSize: '0.7rem',
                                        fontWeight: 500,
                                    }}
                                />
                            ))}
                        </Box>
                    )}
                </Paper>
            ))}
        </Box>
    );
};

export default ClusterCardsSection;
