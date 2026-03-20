/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { memo } from 'react';
import {
    Box,
    IconButton,
    Collapse,
    Chip,
    Tooltip,
    alpha,
} from '@mui/material';
import {
    ExpandMore as ExpandIcon,
    ChevronRight as CollapseIcon,
    Storage as ServerIcon,
    Settings as SettingsIcon,
    Delete as DeleteIcon,
    PanTool as ManualIcon,
} from '@mui/icons-material';
import { useTheme, Theme } from '@mui/material/styles';
import InlineEditText from '../InlineEditText';
import { getRoleConfigs } from './constants';
import type { ClusterType } from './constants';
import { getEffectiveRole } from './utils';
import type { Server } from './utils';
import RolePill from './RolePill';
import StatusIndicator from './StatusIndicator';

// -- Static sx constants --------------------------------------------------

const outerContainerSx = { position: 'relative' };
const childrenContainerSx = { position: 'relative' };
const expandButtonSx = { p: 0.25, color: 'text.secondary' };
const expandIcon16Sx = { fontSize: 16 };
const flexContainerSx = { flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 1 };
const trailingSx = { ml: 'auto', flexShrink: 0 };
const editButtonSx = { p: 0.25, color: 'text.disabled', '&:hover': { color: 'primary.main' } };
const deleteButtonSx = { p: 0.25, color: 'text.disabled', '&:hover': { color: 'error.main' } };
const editIconSx = { fontSize: 14 };
const deleteIconSx = { fontSize: 14 };

const initializingChipBase = {
    height: 20,
    maxWidth: 'none',
    fontSize: '0.875rem',
    fontWeight: 600,
    '& .MuiChip-label': { px: 1 },
};

const serverNameBase = {
    fontSize: '0.875rem',
    lineHeight: 1.3,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

// -- Style-getter functions -----------------------------------------------

const getTreeLineSx = (lineLeftPos: number, isLast: boolean, rowCenterY: number, theme: Theme) => ({
    position: 'absolute',
    left: `${lineLeftPos}px`,
    top: 0,
    bottom: isLast ? `calc(100% - ${rowCenterY}px)` : '-2px',
    width: '1px',
    bgcolor: theme.palette.mode === 'dark' ? theme.palette.grey[600] : theme.palette.grey[300],
});

const getHorizontalLineSx = (lineLeftPos: number, rowCenterY: number, horizontalLineWidth: number, theme: Theme) => ({
    position: 'absolute',
    left: `${lineLeftPos}px`,
    top: `${rowCenterY}px`,
    width: `${horizontalLineWidth}px`,
    height: '1px',
    bgcolor: theme.palette.mode === 'dark' ? theme.palette.grey[600] : theme.palette.grey[300],
});

const getRowSx = (theme: Theme, isSelected: boolean, baseIndent: number, depthIndent: number, hasChildren: boolean | undefined, expanderWidth: number) => ({
    position: 'relative',
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
    py: 0.5,
    px: 1,
    pl: baseIndent + depthIndent + (hasChildren ? 0 : expanderWidth),
    cursor: 'pointer',
    borderRadius: 1,
    mx: 0.5,
    mb: 0.25,
    bgcolor: isSelected
        ? alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.20 : 0.12)
        : 'transparent',
    borderLeft: isSelected ? '2px solid' : '2px solid transparent',
    borderLeftColor: isSelected ? 'primary.main' : 'transparent',
    transition: 'all 0.15s ease',
    '&:hover': {
        bgcolor: isSelected
            ? alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.25 : 0.16)
            : alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.08 : 0.04),
    },
});

const getServerIconSx = (isSelected: boolean, isOffline: boolean) => ({
    fontSize: 16,
    color: isSelected ? 'primary.main' : 'text.secondary',
    opacity: isOffline ? 0.5 : 1,
});

const getServerNameSx = (isSelected: boolean, isOffline: boolean) => ({
    ...serverNameBase,
    fontWeight: isSelected ? 600 : 400,
    color: isSelected ? 'text.primary' : 'text.secondary',
    opacity: isOffline ? 0.6 : 1,
});

const getInitializingChipSx = (theme: Theme) => ({
    ...initializingChipBase,
    bgcolor: alpha(theme.palette.grey[500], theme.palette.mode === 'dark' ? 0.2 : 0.1),
    color: theme.palette.grey[500],
});

const getActionButtonsSx = (theme: Theme) => ({
    position: 'absolute',
    right: 8,
    top: '50%',
    transform: 'translateY(-50%)',
    display: 'flex',
    gap: 0.25,
    opacity: 0,
    transition: 'opacity 0.15s',
    bgcolor: alpha(theme.palette.background.paper, 0.95),
    borderRadius: 1,
    px: 0.5,
    py: 0.25,
    '.server-item-row:hover &': { opacity: 1 },
});

/**
 * ServerItem - Individual server entry in the navigation tree
 * Supports recursive rendering for replication topology with cascading standbys
 * Memoized to prevent unnecessary re-renders during data refresh
 */
interface ExtendedServer extends Server {
    owner_username?: string;
    primary_role?: string | null;
}

interface UserInfo {
    isSuperuser?: boolean;
    username?: string;
}

interface ServerItemProps {
    server: ExtendedServer;
    isSelected: boolean;
    onSelect: (server: ExtendedServer) => void;
    depth?: number;
    isDark: boolean;
    expandedServers?: Set<number>;
    onToggleServer?: (serverId: number) => void;
    selectedServerId?: number;
    isLast?: boolean;
    showTreeLines?: boolean;
    clusterType?: ClusterType;
    user?: UserInfo;
    onUpdateServer: (serverId: number, newName: string) => Promise<void>;
    onEditServer?: (server: ExtendedServer) => void;
    onDeleteServer?: (server: ExtendedServer) => void;
    alertCount?: number;
    getServerAlertCount?: (serverId: number) => number;
    getServerBlackoutStatus?: (serverId: number) => { active: boolean; inherited: boolean };
    getClusterBlackoutStatus?: (clusterId: string) => { active: boolean; inherited: boolean };
}

const ServerItem = memo<ServerItemProps>(({
    server,
    isSelected,
    onSelect,
    depth = 0,
    isDark,
    expandedServers,
    onToggleServer,
    selectedServerId,
    isLast = false,
    showTreeLines = true,
    clusterType = 'default',
    user,
    onUpdateServer,
    onEditServer,
    onDeleteServer,
    alertCount = 0,
    getServerAlertCount,
    getServerBlackoutStatus,
    getClusterBlackoutStatus,
}) => {
    const theme = useTheme();
    const ROLE_CONFIGS = getRoleConfigs(theme);

    // User can edit if they're superuser or the owner
    const canEditServer = user?.isSuperuser || server.owner_username === user?.username;
    const hasChildren = server.children?.length > 0 || server.is_expandable;
    const isExpanded = expandedServers?.has(server.id);
    const serverRole = server.primary_role || server.role;
    const effectiveRole = getEffectiveRole(serverRole, clusterType);

    const handleToggle = (e: React.MouseEvent) => {
        e.stopPropagation();
        if (hasChildren && onToggleServer) {
            onToggleServer(server.id);
        }
    };

    const handleSelect = (e: React.MouseEvent) => {
        e.stopPropagation();
        onSelect(server);
    };

    // Calculate indentation based on depth (MUI spacing: 1 unit = 8px)
    const baseIndent = 1.5; // Base left padding in MUI units (12px)
    const depthIndent = depth * 2.5; // Additional indent per depth level (20px per level)
    const expanderWidth = 2; // Width reserved for expand/collapse button (16px)

    // Tree line positioning (in pixels)
    // Lines connect from parent's expand button center to this node's expand button or status icon
    //
    // For depth=1, parent is the cluster header:
    //   Cluster header layout: mx(4px) + px(8px) + IconButton(p:0.25=2px, icon:18px)
    //   Expand button center = 4 + 8 + 2 + 9 = 23px from container content edge
    //
    // For depth>1, parent is another ServerItem at depth-1:
    //   ServerItem layout: mx(4px) + pl in pixels
    //   Parent pl = (1.5 + (d-1)*2.5) * 8 = 12 + (d-1)*20  (parent always hasChildren since it has us)
    //   Parent expand button center = 4 + parentPl + 2 + 8 = 26 + (d-1)*20 px
    //
    const lineLeftPos = depth === 1
        ? 23 // Align with cluster header expand button center
        : 26 + (depth - 1) * 20; // Align with parent ServerItem expand button center

    // This node's target position (where horizontal line should end)
    // ServerItem row: mx(4px) + pl + content
    // pl = (baseIndent + depthIndent + (hasChildren ? 0 : expanderWidth)) * 8
    //    = (1.5 + depth*2.5 + (hasChildren ? 0 : 2)) * 8
    // If hasChildren: expand button at pl, center = 4 + pl + 2 + 8 = 14 + pl
    // If no children: status icon at pl, center = 4 + pl + 4 = 8 + pl
    const thisNodePl = (baseIndent + depthIndent + (hasChildren ? 0 : expanderWidth)) * 8;
    const thisNodeTargetX = hasChildren
        ? 4 + thisNodePl + 10 // To expand button center (p:0.25=2px + icon:16px/2=8px)
        : 4 + thisNodePl + 4; // To status icon center (8px icon / 2)
    const horizontalLineWidth = thisNodeTargetX - lineLeftPos - 10;

    // Row height for vertical centering of horizontal connector
    // Row has py:0.5 (4px) + content height (~20px) + py:0.5 (4px) = ~28px total
    // Visual center of content is approximately at 4px + 10px = 14px, but accounting
    // for the status icon and server icon vertical alignment, 18px works better
    const rowCenterY = 18;

    return (
        <Box sx={outerContainerSx}>
            {/* Tree lines for nested items */}
            {showTreeLines && depth > 0 && (
                <>
                    {/* Vertical line from parent's expand icon */}
                    <Box sx={getTreeLineSx(lineLeftPos, isLast, rowCenterY, theme)} />
                    {/* Horizontal connector to this node */}
                    <Box sx={getHorizontalLineSx(lineLeftPos, rowCenterY, horizontalLineWidth, theme)} />
                </>
            )}
            <Box
                className="server-item-row"
                onClick={handleSelect}
                sx={getRowSx(theme, isSelected, baseIndent, depthIndent, hasChildren, expanderWidth)}
            >
                {hasChildren && (
                    <IconButton
                        size="small"
                        sx={expandButtonSx}
                        onClick={handleToggle}
                    >
                        {isExpanded ? (
                            <ExpandIcon sx={expandIcon16Sx} />
                        ) : (
                            <CollapseIcon sx={expandIcon16Sx} />
                        )}
                    </IconButton>
                )}
                <StatusIndicator
                    status={server.status}
                    alertCount={alertCount}
                    connectionError={server.connection_error}
                    blackoutActive={getServerBlackoutStatus?.(server.id)?.active}
                    blackoutInherited={getServerBlackoutStatus?.(server.id)?.inherited}
                />
                <ServerIcon sx={getServerIconSx(isSelected, server.status === 'offline')} />
                <Box sx={flexContainerSx}>
                    <InlineEditText
                        value={server.name}
                        onSave={(newName) => onUpdateServer(server.id, newName)}
                        canEdit={canEditServer}
                        typographyProps={{
                            variant: 'body2',
                            sx: getServerNameSx(isSelected, server.status === 'offline'),
                        }}
                    />
                </Box>
                {server.status === 'unknown' ? (
                    <Box sx={trailingSx}>
                        <Chip
                            label="Initializing"
                            size="small"
                            sx={getInitializingChipSx(theme)}
                        />
                    </Box>
                ) : effectiveRole && ROLE_CONFIGS[effectiveRole] ? (
                    <Box sx={{ ...trailingSx, display: 'flex', alignItems: 'center', gap: 0.5 }}>
                        {server.membership_source === 'manual' && (
                            <Tooltip title="Manually assigned" arrow>
                                <ManualIcon sx={{ fontSize: 12, color: 'text.disabled' }} />
                            </Tooltip>
                        )}
                        <RolePill role={effectiveRole} isDark={isDark} />
                    </Box>
                ) : null}
                {canEditServer && (
                    <Box
                        className="action-buttons"
                        sx={getActionButtonsSx(theme)}
                    >
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                e.stopPropagation();
                                onEditServer?.(server);
                            }}
                            sx={editButtonSx}
                        >
                            <SettingsIcon sx={editIconSx} />
                        </IconButton>
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                e.stopPropagation();
                                onDeleteServer?.(server);
                            }}
                            sx={deleteButtonSx}
                        >
                            <DeleteIcon sx={deleteIconSx} />
                        </IconButton>
                    </Box>
                )}
            </Box>
            {/* Render child servers recursively */}
            {hasChildren && server.children?.length > 0 && (
                <Collapse in={isExpanded} timeout="auto">
                    <Box sx={childrenContainerSx}>
                        {server.children.map((childServer, index) => (
                            <ServerItem
                                key={childServer.id}
                                server={childServer}
                                isSelected={selectedServerId === childServer.id}
                                onSelect={onSelect}
                                depth={depth + 1}
                                isDark={isDark}
                                expandedServers={expandedServers}
                                onToggleServer={onToggleServer}
                                selectedServerId={selectedServerId}
                                isLast={index === server.children.length - 1}
                                showTreeLines={showTreeLines}
                                clusterType={clusterType}
                                user={user}
                                onUpdateServer={onUpdateServer}
                                onEditServer={onEditServer}
                                onDeleteServer={onDeleteServer}
                                alertCount={getServerAlertCount ? getServerAlertCount(childServer.id) : 0}
                                getServerAlertCount={getServerAlertCount}
                                getServerBlackoutStatus={getServerBlackoutStatus}
                                getClusterBlackoutStatus={getClusterBlackoutStatus}
                            />
                        ))}
                    </Box>
                </Collapse>
            )}
        </Box>
    );
});

// Display name for debugging
ServerItem.displayName = 'ServerItem';

export default ServerItem;
