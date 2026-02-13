/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useCallback } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import { alpha, useTheme, Theme } from '@mui/material/styles';
import RolePill from '../../../ClusterNavigator/RolePill';
import { ServerRole } from '../../../ClusterNavigator/constants';
import { TopoNode } from './types';

interface TopologyNodeProps {
    node: TopoNode;
    nodeWidth: number;
    onClick: (node: TopoNode) => void;
}

/**
 * Determine the status dot color for a server status string.
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

const STATUS_DOT_SX = {
    width: 8,
    height: 8,
    borderRadius: '50%',
    flexShrink: 0,
};

const NAME_SX = {
    fontWeight: 600,
    fontSize: '0.875rem',
    color: 'text.primary',
    lineHeight: 1.2,
    flex: 1,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const ROW_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
};

/**
 * TopologyNode renders a single server node card in the topology
 * diagram. The card is absolutely positioned according to the
 * layout coordinates on the TopoNode.
 */
const TopologyNode: React.FC<TopologyNodeProps> = ({
    node,
    nodeWidth,
    onClick,
}) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';

    const handleClick = useCallback(() => {
        onClick(node);
    }, [onClick, node]);

    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onClick(node);
            }
        },
        [onClick, node],
    );

    return (
        <Paper
            elevation={0}
            sx={{
                position: 'absolute',
                left: node.x,
                top: node.y,
                width: nodeWidth,
                p: 1,
                borderRadius: 1.5,
                bgcolor: isDark
                    ? alpha(theme.palette.grey[800], 0.8)
                    : theme.palette.grey[50],
                border: '1px solid',
                borderColor: theme.palette.divider,
                cursor: 'pointer',
                transition:
                    'border-color 0.2s, box-shadow 0.2s',
                '&:hover': {
                    borderColor: theme.palette.primary.main,
                    boxShadow: `0 0 0 1px ${alpha(
                        theme.palette.primary.main,
                        0.3,
                    )}`,
                },
            }}
            onClick={handleClick}
            onKeyDown={handleKeyDown}
            role="button"
            tabIndex={0}
            aria-label={`Select server ${node.name}`}
        >
            <Box sx={ROW_SX}>
                <Box
                    sx={{
                        ...STATUS_DOT_SX,
                        bgcolor: getStatusDotColor(
                            node.status,
                            theme,
                        ),
                    }}
                    aria-label={`Status: ${node.status}`}
                />
                <Typography sx={NAME_SX}>{node.name}</Typography>
            </Box>
            <Box sx={{ mt: 0.5 }}>
                <RolePill
                    role={node.role as ServerRole}
                    isDark={isDark}
                />
            </Box>
        </Paper>
    );
};

export default TopologyNode;
