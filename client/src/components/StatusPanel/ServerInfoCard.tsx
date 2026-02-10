/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo, useState } from 'react';
import {
    Box,
    Typography,
    IconButton,
    Tooltip,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    InfoOutlined as InfoIcon,
} from '@mui/icons-material';
import {
    SERVER_INFO_WRAPPER_SX,
    SERVER_INFO_LABEL_BASE_SX,
    SERVER_INFO_VALUE_BASE_SX,
    SPOCK_DOT_SX,
    SPOCK_LABEL_BASE_SX,
    SPOCK_VERSION_SX,
    SPOCK_NODE_SX,
} from './styles';
import ServerInfoDialog from '../ServerInfoDialog';

/**
 * ServerInfoCard - Unified compact server information display
 */
const ServerInfoCard = ({ selection }) => {
    const theme = useTheme();
    const [infoDialogOpen, setInfoDialogOpen] = useState(false);

    // Combine all data items into a single array for the grid
    const allData = [
        { label: 'HOST', value: selection.host, mono: true },
        { label: 'PORT', value: selection.port, mono: true },
        { label: 'DATABASE', value: selection.database, mono: true },
        { label: 'USER', value: selection.username, mono: true },
        { label: 'POSTGRESQL', value: selection.version, mono: true },
        { label: 'OS', value: selection.os, mono: false },
        { label: 'ROLE', value: selection.role?.replace(/_/g, ' '), mono: false, capitalize: true },
    ].filter(item => item.value);

    const replicationData = selection.spockVersion || selection.spockNodeName ? {
        version: selection.spockVersion,
        nodeName: selection.spockNodeName,
    } : null;

    const containerSx = useMemo(() => ({
        borderRadius: 1.5,
        overflow: 'hidden',
        border: '1px solid',
        borderColor: theme.palette.divider,
        bgcolor: theme.palette.background.paper,
    }), [theme]);

    const labelSx = useMemo(() => ({
        ...SERVER_INFO_LABEL_BASE_SX,
        color: theme.palette.grey[500],
    }), [theme.palette.grey]);

    const spockSectionSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 2,
        px: 1.5,
        py: 0.75,
        bgcolor: alpha(theme.palette.custom.status.sky, 0.06),
        borderTop: '1px solid',
        borderColor: alpha(theme.palette.custom.status.sky, 0.18),
    }), [theme]);

    const spockDotSx = useMemo(() => ({
        ...SPOCK_DOT_SX,
        bgcolor: theme.palette.custom.status.sky,
    }), [theme.palette.custom.status]);

    const spockLabelSx = useMemo(() => ({
        ...SPOCK_LABEL_BASE_SX,
        color: theme.palette.custom.status.skyDark,
    }), [theme.palette.custom.status]);

    return (
        <>
            <Box sx={containerSx}>
                {/* Data grid */}
                <Box sx={{
                    ...SERVER_INFO_WRAPPER_SX,
                    position: 'relative',
                }}>
                    {allData.map((item, idx) => (
                        <Box
                            key={item.label}
                            sx={{
                                display: 'flex',
                                flexDirection: 'column',
                                gap: 0.25,
                                px: 1.5,
                                py: 1,
                                borderRight: idx < allData.length - 1 ? '1px solid' : 'none',
                                borderColor: theme.palette.divider,
                                minWidth: item.label === 'OS' ? 180 : 'auto',
                            }}
                        >
                            <Typography sx={labelSx}>
                                {item.label}
                            </Typography>
                            <Typography
                                sx={{
                                    ...SERVER_INFO_VALUE_BASE_SX,
                                    fontFamily: item.mono ? '"JetBrains Mono", "SF Mono", monospace' : 'inherit',
                                    textTransform: item.capitalize ? 'capitalize' : 'none',
                                }}
                            >
                                {item.value}
                            </Typography>
                        </Box>
                    ))}
                    {/* Info button */}
                    <Box sx={{
                        display: 'flex',
                        alignItems: 'center',
                        px: 0.75,
                        ml: 'auto',
                    }}>
                        <Tooltip title="Server details" placement="top">
                            <IconButton
                                size="small"
                                onClick={() => setInfoDialogOpen(true)}
                                sx={{
                                    p: 0.5,
                                    color: theme.palette.grey[500],
                                    '&:hover': {
                                        color: theme.palette.primary.main,
                                        bgcolor: alpha(theme.palette.primary.main, 0.08),
                                    },
                                }}
                            >
                                <InfoIcon sx={{ fontSize: 16 }} />
                            </IconButton>
                        </Tooltip>
                    </Box>
                </Box>

                {/* Spock replication section */}
                {replicationData && (
                    <Box sx={spockSectionSx}>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                            <Box sx={spockDotSx} />
                            <Typography sx={spockLabelSx}>
                                Spock Replication
                            </Typography>
                        </Box>
                        {replicationData.version && (
                            <Typography sx={SPOCK_VERSION_SX}>
                                v{replicationData.version}
                            </Typography>
                        )}
                        {replicationData.nodeName && (
                            <Typography sx={SPOCK_NODE_SX}>
                                {replicationData.nodeName}
                            </Typography>
                        )}
                    </Box>
                )}
            </Box>

            {/* Server Info Dialog */}
            <ServerInfoDialog
                open={infoDialogOpen}
                onClose={() => setInfoDialogOpen(false)}
                connectionId={selection.id}
                serverName={selection.name || selection.host || 'Server'}
            />
        </>
    );
};

export default ServerInfoCard;
