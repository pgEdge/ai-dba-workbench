/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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
    alpha,
} from '@mui/material';
import {
    ExpandMore as ExpandIcon,
    ChevronRight as CollapseIcon,
    Storage as ServerIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';
import InlineEditText from '../InlineEditText';
import { ROLE_CONFIGS } from './constants';
import { getEffectiveRole } from './utils';
import RolePill from './RolePill';
import StatusIndicator from './StatusIndicator';

/**
 * ServerItem - Individual server entry in the navigation tree
 * Supports recursive rendering for replication topology with cascading standbys
 * Memoized to prevent unnecessary re-renders during data refresh
 */
const ServerItem = memo(({
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
}) => {
    // User can edit if they're superuser or the owner
    const canEditServer = user?.isSuperuser || server.owner_username === user?.username;
    const hasChildren = server.children?.length > 0 || server.is_expandable;
    const isExpanded = expandedServers?.has(server.id);
    const serverRole = server.primary_role || server.role;
    const effectiveRole = getEffectiveRole(serverRole, clusterType);
    const lineColor = isDark ? '#475569' : '#C1C7CD';

    const handleToggle = (e) => {
        e.stopPropagation();
        if (hasChildren && onToggleServer) {
            onToggleServer(server.id);
        }
    };

    const handleSelect = (e) => {
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
        <Box sx={{ position: 'relative' }}>
            {/* Tree lines for nested items */}
            {showTreeLines && depth > 0 && (
                <>
                    {/* Vertical line from parent's expand icon */}
                    <Box
                        sx={{
                            position: 'absolute',
                            left: `${lineLeftPos}px`,
                            top: 0,
                            bottom: isLast ? `calc(100% - ${rowCenterY}px)` : '-2px',
                            width: '1px',
                            bgcolor: lineColor,
                        }}
                    />
                    {/* Horizontal connector to this node */}
                    <Box
                        sx={{
                            position: 'absolute',
                            left: `${lineLeftPos}px`,
                            top: `${rowCenterY}px`,
                            width: `${horizontalLineWidth}px`,
                            height: '1px',
                            bgcolor: lineColor,
                        }}
                    />
                </>
            )}
            <Box
                className="server-item-row"
                onClick={handleSelect}
                sx={{
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
                        ? (isDark ? alpha('#22B8CF', 0.20) : alpha('#15AABF', 0.12))
                        : 'transparent',
                    borderLeft: isSelected ? '2px solid' : '2px solid transparent',
                    borderLeftColor: isSelected ? 'primary.main' : 'transparent',
                    transition: 'all 0.15s ease',
                    '&:hover': {
                        bgcolor: isSelected
                            ? (isDark ? alpha('#22B8CF', 0.25) : alpha('#15AABF', 0.16))
                            : (isDark ? alpha('#22B8CF', 0.08) : alpha('#15AABF', 0.04)),
                    },
                }}
            >
                {hasChildren && (
                    <IconButton
                        size="small"
                        sx={{
                            p: 0.25,
                            color: 'text.secondary',
                        }}
                        onClick={handleToggle}
                    >
                        {isExpanded ? (
                            <ExpandIcon sx={{ fontSize: 16 }} />
                        ) : (
                            <CollapseIcon sx={{ fontSize: 16 }} />
                        )}
                    </IconButton>
                )}
                <StatusIndicator status={server.status} alertCount={alertCount} isDark={isDark} connectionError={server.connection_error} />
                <ServerIcon
                    sx={{
                        fontSize: 16,
                        color: isSelected ? 'primary.main' : 'text.secondary',
                        opacity: server.status === 'offline' ? 0.5 : 1,
                    }}
                />
                <Box sx={{ flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 1 }}>
                    <InlineEditText
                        value={server.name}
                        onSave={(newName) => onUpdateServer(server.id, newName)}
                        canEdit={canEditServer}
                        typographyProps={{
                            variant: 'body2',
                            sx: {
                                fontWeight: isSelected ? 600 : 400,
                                color: isSelected ? 'text.primary' : 'text.secondary',
                                fontSize: '0.8125rem',
                                lineHeight: 1.3,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                                opacity: server.status === 'offline' ? 0.6 : 1,
                            },
                        }}
                    />
                </Box>
                {server.status === 'unknown' ? (
                    <Box sx={{ ml: 'auto', flexShrink: 0 }}>
                        <Chip
                            label="Initializing"
                            size="small"
                            sx={{
                                height: 20,
                                fontSize: '0.625rem',
                                fontWeight: 600,
                                bgcolor: isDark ? 'rgba(107, 114, 128, 0.2)' : 'rgba(107, 114, 128, 0.1)',
                                color: isDark ? '#9CA3AF' : '#6B7280',
                                '& .MuiChip-label': { px: 1 },
                            }}
                        />
                    </Box>
                ) : effectiveRole && ROLE_CONFIGS[effectiveRole] ? (
                    <Box sx={{ ml: 'auto', flexShrink: 0 }}>
                        <RolePill role={effectiveRole} isDark={isDark} />
                    </Box>
                ) : null}
                {canEditServer && (
                    <Box
                        className="action-buttons"
                        sx={{
                            position: 'absolute',
                            right: 8,
                            top: '50%',
                            transform: 'translateY(-50%)',
                            display: 'flex',
                            gap: 0.25,
                            opacity: 0,
                            transition: 'opacity 0.15s',
                            bgcolor: isDark ? 'rgba(30, 41, 59, 0.95)' : 'rgba(255, 255, 255, 0.95)',
                            borderRadius: 1,
                            px: 0.5,
                            py: 0.25,
                            '.server-item-row:hover &': { opacity: 1 },
                        }}
                    >
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                e.stopPropagation();
                                onEditServer?.(server);
                            }}
                            sx={{ p: 0.25, color: 'text.disabled', '&:hover': { color: 'primary.main' } }}
                        >
                            <EditIcon sx={{ fontSize: 14 }} />
                        </IconButton>
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                e.stopPropagation();
                                onDeleteServer?.(server);
                            }}
                            sx={{ p: 0.25, color: 'text.disabled', '&:hover': { color: 'error.main' } }}
                        >
                            <DeleteIcon sx={{ fontSize: 14 }} />
                        </IconButton>
                    </Box>
                )}
            </Box>
            {/* Render child servers recursively */}
            {hasChildren && server.children?.length > 0 && (
                <Collapse in={isExpanded} timeout="auto">
                    <Box sx={{ position: 'relative' }}>
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
