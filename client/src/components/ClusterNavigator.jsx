/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Cluster Navigator Component
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * A hierarchical navigation panel for cluster groups, clusters, and servers
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useMemo, useCallback, useRef, useEffect, memo } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { useCluster } from '../contexts/ClusterContext';
import InlineEditText from './InlineEditText';
import {
    DndContext,
    useDraggable,
    useDroppable,
    DragOverlay,
    pointerWithin,
} from '@dnd-kit/core';
import {
    Box,
    Typography,
    IconButton,
    Collapse,
    Tooltip,
    TextField,
    InputAdornment,
    Chip,
    alpha,
    Skeleton,
} from '@mui/material';
import {
    ExpandMore as ExpandIcon,
    ChevronRight as CollapseIcon,
    Search as SearchIcon,
    Storage as ServerIcon,
    Dns as ClusterIcon,
    FolderOpen as GroupOpenIcon,
    Folder as GroupIcon,
    Circle as StatusIcon,
    Refresh as RefreshIcon,
    Add as AddIcon,
    Star as PrimaryIcon,
    Backup as StandbyIcon,
    AccountTree as CascadingIcon,
    Hub as SpockIcon,
    Publish as PublisherIcon,
    Download as SubscriberIcon,
    Storage as StandaloneIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    Autorenew as AutorenewIcon,
    DragIndicator as DragIcon,
} from '@mui/icons-material';
import ServerDialog from './ServerDialog';
import GroupDialog from './GroupDialog';
import DeleteConfirmationDialog from './DeleteConfirmationDialog';
import AddMenu from './AddMenu';

// Status color mapping
const STATUS_COLORS = {
    online: '#22C55E',
    warning: '#F59E0B',
    offline: '#EF4444',
    unknown: '#6B7280',
};

// Status labels
const STATUS_LABELS = {
    online: 'Online',
    warning: 'Warning',
    offline: 'Offline',
    unknown: 'Unknown',
};

// Role configuration with colors and labels
const ROLE_CONFIGS = {
    binary_primary: { label: 'Primary', color: '#15AABF', darkColor: '#22B8CF' },
    binary_standby: { label: 'Standby', color: '#6366F1', darkColor: '#818CF8' },
    binary_cascading: { label: 'Cascade', color: '#8B5CF6', darkColor: '#A78BFA' },
    spock_node: { label: 'Spock', color: '#F59E0B', darkColor: '#FBBF24' },
    standalone: { label: 'Standalone', color: '#6B7280', darkColor: '#94A3B8' },
    logical_publisher: { label: 'Publisher', color: '#22C55E', darkColor: '#4ADE80' },
    logical_subscriber: { label: 'Subscriber', color: '#3B82F6', darkColor: '#60A5FA' },
};

// Role to icon mapping
const ROLE_ICONS = {
    binary_primary: PrimaryIcon,
    binary_standby: StandbyIcon,
    binary_cascading: CascadingIcon,
    spock_node: SpockIcon,
    standalone: StandaloneIcon,
    logical_publisher: PublisherIcon,
    logical_subscriber: SubscriberIcon,
};

/**
 * RolePill - Displays a colored chip based on the server role
 */
const RolePill = ({ role, isDark }) => {
    const config = ROLE_CONFIGS[role];
    if (!config) return null;

    const color = isDark ? config.darkColor : config.color;
    const IconComponent = ROLE_ICONS[role];

    return (
        <Chip
            icon={IconComponent ? <IconComponent sx={{ fontSize: '10px !important', color: `${color} !important` }} /> : undefined}
            label={config.label}
            size="small"
            sx={{
                height: 18,
                fontSize: '0.625rem',
                fontWeight: 600,
                bgcolor: alpha(color, isDark ? 0.2 : 0.12),
                color: color,
                '& .MuiChip-icon': { ml: 0.5, mr: -0.25 },
                '& .MuiChip-label': { pl: 0.75, pr: 0.75 },
            }}
        />
    );
};

/**
 * Recursively collect all roles from servers and their children
 */
const collectAllRoles = (servers) => {
    if (!servers || servers.length === 0) return [];
    const roles = [];
    const traverse = (serverList) => {
        serverList.forEach(s => {
            const role = s.primary_role || s.role;
            if (role) roles.push(role);
            if (s.children?.length > 0) traverse(s.children);
        });
    };
    traverse(servers);
    return roles;
};

/**
 * Detect cluster type based on server roles (including children)
 * Returns: 'spock' | 'binary' | 'logical' | 'default'
 * Priority: spock > logical > binary (logical takes precedence since it's often combined with binary)
 */
const getClusterType = (cluster) => {
    if (!cluster) return 'default';
    if (cluster.name?.toLowerCase().includes('spock')) return 'spock';
    if (!cluster.servers || cluster.servers.length === 0) return 'default';

    // Collect roles from all servers including nested children
    const roles = collectAllRoles(cluster.servers);
    if (roles.length === 0) return 'default';

    // Check for Spock (highest priority)
    if (roles.some(r => r === 'spock_node')) return 'spock';

    // Check for logical replication (takes precedence over binary)
    const logicalRoles = ['logical_publisher', 'logical_subscriber', 'logical_bidirectional'];
    if (roles.some(r => logicalRoles.includes(r))) return 'logical';

    // Check for binary replication
    const binaryRoles = ['binary_primary', 'binary_standby', 'binary_cascading'];
    if (roles.some(r => binaryRoles.includes(r))) return 'binary';

    return 'default';
};

// Cluster type color schemes
const CLUSTER_TYPE_COLORS = {
    spock: {
        border: { dark: alpha('#F59E0B', 0.5), light: alpha('#F59E0B', 0.4) },
        bg: { dark: alpha('#F59E0B', 0.08), light: alpha('#F59E0B', 0.05) },
    },
    binary: {
        border: { dark: alpha('#22B8CF', 0.4), light: alpha('#15AABF', 0.35) },
        bg: { dark: alpha('#22B8CF', 0.06), light: alpha('#15AABF', 0.04) },
    },
    logical: {
        border: { dark: alpha('#8B5CF6', 0.4), light: alpha('#8B5CF6', 0.35) },
        bg: { dark: alpha('#8B5CF6', 0.06), light: alpha('#8B5CF6', 0.04) },
    },
    default: {
        border: { dark: alpha('#475569', 0.5), light: alpha('#D1D5DB', 0.6) },
        bg: { dark: alpha('#1E293B', 0.5), light: alpha('#F9FAFB', 0.5) },
    },
};

/**
 * Compute effective role for display based on cluster context
 * In logical replication clusters, binary_primary should display as logical_publisher
 */
const getEffectiveRole = (serverRole, clusterType) => {
    if (clusterType === 'logical' && serverRole === 'binary_primary') {
        return 'logical_publisher';
    }
    return serverRole;
};

/**
 * ClusterContainer - Wraps entire cluster (header + servers) with styled border
 * Color varies by cluster type: spock (amber), binary (cyan), logical (purple), default (gray)
 */
const ClusterContainer = ({ children, cluster, isDark }) => {
    const clusterType = getClusterType(cluster);
    const colors = CLUSTER_TYPE_COLORS[clusterType];
    const borderColor = isDark ? colors.border.dark : colors.border.light;
    const bgColor = isDark ? colors.bg.dark : colors.bg.light;

    return (
        <Box
            sx={{
                border: `1px solid`,
                borderColor: borderColor,
                bgcolor: bgColor,
                borderRadius: '8px',
                mx: 1,
                my: 0.5,
                overflow: 'hidden',
            }}
        >
            {children}
        </Box>
    );
};

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
}) => {
    // User can edit if they're superuser or the owner
    const canEditServer = user?.isSuperuser || server.owner_username === user?.username;
    const statusColor = STATUS_COLORS[server.status] || STATUS_COLORS.unknown;
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
                <Tooltip title={STATUS_LABELS[server.status] || 'Unknown'} placement="right">
                    <StatusIcon
                        sx={{
                            fontSize: 8,
                            color: statusColor,
                            filter: `drop-shadow(0 0 2px ${statusColor})`,
                        }}
                    />
                </Tooltip>
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
                {effectiveRole && ROLE_CONFIGS[effectiveRole] && (
                    <Box sx={{ ml: 'auto', flexShrink: 0 }}>
                        <RolePill role={effectiveRole} isDark={isDark} />
                    </Box>
                )}
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

/**
 * Count all servers recursively (including children)
 */
const countServersRecursive = (servers, filterFn = () => true) => {
    if (!servers) return 0;
    return servers.reduce((count, server) => {
        const current = filterFn(server) ? 1 : 0;
        const childCount = countServersRecursive(server.children, filterFn);
        return count + current + childCount;
    }, 0);
};

/**
 * ClusterItem - Cluster entry that can be expanded to show member servers
 * Entire cluster (header + servers) is wrapped in a container
 * Memoized to prevent unnecessary re-renders during data refresh
 */
const ClusterItem = memo(({
    cluster,
    groupId,
    isExpanded,
    onToggle,
    selectedServerId,
    onSelectServer,
    depth = 0,
    isDark,
    expandedServers,
    onToggleServer,
    isLast = false,
    user,
    onUpdateCluster,
    onUpdateServer,
    onEditServer,
    onDeleteServer,
}) => {
    // Superusers can edit:
    // - Database-backed clusters (cluster-{id} format)
    // - Auto-detected clusters that have auto_cluster_key (binary, logical, spock)
    const isDbBackedCluster = /^cluster-\d+$/.test(cluster.id);
    const isAutoDetectedCluster = cluster.auto_cluster_key ? true : false;
    const canEditCluster = user?.isSuperuser && (isDbBackedCluster || isAutoDetectedCluster);
    const totalCount = countServersRecursive(cluster.servers);
    const onlineCount = countServersRecursive(cluster.servers, s => s.status === 'online');
    const hasWarning = countServersRecursive(cluster.servers, s => s.status === 'warning') > 0;
    const allOffline = totalCount > 0 && onlineCount === 0;

    const clusterStatus = allOffline ? 'offline' : (hasWarning ? 'warning' : 'online');
    const statusColor = STATUS_COLORS[clusterStatus];
    const clusterType = getClusterType(cluster);

    return (
        <ClusterContainer cluster={cluster} isDark={isDark}>
            {/* Cluster Header */}
            <Box
                onClick={onToggle}
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 0.75,
                    py: 0.75,
                    px: 1,
                    cursor: 'pointer',
                    borderRadius: 1,
                    mx: 0.5,
                    transition: 'all 0.15s ease',
                    '&:hover': {
                        bgcolor: isDark ? alpha('#22B8CF', 0.08) : alpha('#15AABF', 0.04),
                    },
                }}
            >
                <IconButton
                    size="small"
                    sx={{
                        p: 0.25,
                        color: 'text.secondary',
                    }}
                    onClick={(e) => {
                        e.stopPropagation();
                        onToggle();
                    }}
                >
                    {isExpanded ? (
                        <ExpandIcon sx={{ fontSize: 18 }} />
                    ) : (
                        <CollapseIcon sx={{ fontSize: 18 }} />
                    )}
                </IconButton>
                <Tooltip title={`${onlineCount}/${totalCount} servers online`} placement="right">
                    <StatusIcon
                        sx={{
                            fontSize: 8,
                            color: statusColor,
                            filter: `drop-shadow(0 0 2px ${statusColor})`,
                        }}
                    />
                </Tooltip>
                <ClusterIcon
                    sx={{
                        fontSize: 18,
                        color: 'text.secondary',
                    }}
                />
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <InlineEditText
                        value={cluster.name}
                        onSave={(newName) => onUpdateCluster(cluster.id, newName, groupId, cluster.auto_cluster_key)}
                        canEdit={canEditCluster}
                        typographyProps={{
                            variant: 'body2',
                            sx: {
                                fontWeight: 500,
                                color: 'text.primary',
                                fontSize: '0.8125rem',
                                lineHeight: 1.3,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                            },
                        }}
                    />
                </Box>
                <Chip
                    label={`${onlineCount}/${totalCount}`}
                    size="small"
                    sx={{
                        ml: 'auto',
                        flexShrink: 0,
                        height: 18,
                        fontSize: '0.625rem',
                        fontWeight: 600,
                        bgcolor: isDark ? alpha(statusColor, 0.15) : alpha(statusColor, 0.1),
                        color: statusColor,
                        '& .MuiChip-label': {
                            px: 0.75,
                        },
                    }}
                />
            </Box>
            {/* Server List */}
            <Collapse in={isExpanded} timeout="auto">
                <Box sx={{ pb: 0.5 }}>
                    {cluster.servers?.map((server, index) => (
                        <ServerItem
                            key={server.id}
                            server={server}
                            isSelected={selectedServerId === server.id}
                            onSelect={onSelectServer}
                            depth={1}
                            isDark={isDark}
                            expandedServers={expandedServers}
                            onToggleServer={onToggleServer}
                            selectedServerId={selectedServerId}
                            isLast={index === cluster.servers.length - 1}
                            showTreeLines={true}
                            clusterType={clusterType}
                            user={user}
                            onUpdateServer={onUpdateServer}
                            onEditServer={onEditServer}
                            onDeleteServer={onDeleteServer}
                        />
                    ))}
                </Box>
            </Collapse>
        </ClusterContainer>
    );
});

// Display name for debugging
ClusterItem.displayName = 'ClusterItem';

/**
 * DraggableCluster - Wrapper that makes a cluster draggable via drag handle
 * Uses a drag handle approach to avoid blocking click events on child components
 */
const DraggableCluster = ({
    cluster,
    groupId,
    children,
    isDark,
    canDrag = true,
}) => {
    const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
        id: `draggable-${cluster.id}`,
        data: {
            type: 'cluster',
            cluster,
            groupId,
        },
        disabled: !canDrag,
    });

    const style = transform
        ? {
            transform: `translate3d(${transform.x}px, ${transform.y}px, 0)`,
        }
        : undefined;

    return (
        <Box
            ref={setNodeRef}
            style={style}
            className="draggable-cluster"
            sx={{
                position: 'relative',
                opacity: isDragging ? 0.5 : 1,
            }}
        >
            {/* Drag handle - only appears on hover when dragging is allowed */}
            {canDrag && (
                <Box
                    {...attributes}
                    {...listeners}
                    sx={{
                        position: 'absolute',
                        left: -4,
                        top: '50%',
                        transform: 'translateY(-50%)',
                        zIndex: 10,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 20,
                        height: 28,
                        borderRadius: 1,
                        cursor: 'grab',
                        opacity: 0,
                        transition: 'opacity 0.15s',
                        bgcolor: isDark ? 'rgba(30, 41, 59, 0.95)' : 'rgba(255, 255, 255, 0.95)',
                        boxShadow: isDark
                            ? '0 2px 8px rgba(0, 0, 0, 0.3)'
                            : '0 2px 8px rgba(0, 0, 0, 0.1)',
                        '.draggable-cluster:hover &': { opacity: 1 },
                        '&:hover': {
                            bgcolor: isDark ? alpha('#22B8CF', 0.15) : alpha('#15AABF', 0.1),
                        },
                        '&:active': { cursor: 'grabbing' },
                    }}
                >
                    <DragIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                </Box>
            )}
            {children}
        </Box>
    );
};

/**
 * DroppableGroup - Wrapper that makes a group a drop target
 */
const DroppableGroup = ({
    groupId,
    children,
    isDark,
}) => {
    const { isOver, setNodeRef } = useDroppable({
        id: `droppable-${groupId}`,
        data: {
            type: 'group',
            groupId,
        },
    });

    return (
        <Box
            ref={setNodeRef}
            sx={{
                position: 'relative',
                transition: 'all 0.2s ease',
                ...(isOver && {
                    '&::before': {
                        content: '""',
                        position: 'absolute',
                        top: 0,
                        left: 8,
                        right: 8,
                        bottom: 0,
                        border: '2px dashed',
                        borderColor: isDark ? '#22B8CF' : '#15AABF',
                        borderRadius: 2,
                        bgcolor: isDark ? alpha('#22B8CF', 0.08) : alpha('#15AABF', 0.05),
                        pointerEvents: 'none',
                        zIndex: 1,
                    },
                }),
            }}
        >
            {children}
        </Box>
    );
};

/**
 * DragOverlayContent - Content shown during drag
 */
const DragOverlayContent = ({ cluster, isDark }) => {
    if (!cluster) return null;

    const totalCount = countServersRecursive(cluster.servers);
    const onlineCount = countServersRecursive(cluster.servers, s => s.status === 'online');

    return (
        <Box
            sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                py: 0.75,
                px: 1.5,
                bgcolor: isDark ? '#1E293B' : '#FFFFFF',
                border: '1px solid',
                borderColor: isDark ? '#22B8CF' : '#15AABF',
                borderRadius: 2,
                boxShadow: isDark
                    ? '0 8px 24px rgba(0, 0, 0, 0.4)'
                    : '0 8px 24px rgba(0, 0, 0, 0.15)',
                cursor: 'grabbing',
            }}
        >
            <DragIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
            <ClusterIcon sx={{ fontSize: 16, color: 'text.secondary' }} />
            <Typography
                variant="body2"
                sx={{
                    fontWeight: 500,
                    fontSize: '0.8125rem',
                    color: 'text.primary',
                }}
            >
                {cluster.name}
            </Typography>
            <Chip
                label={`${onlineCount}/${totalCount}`}
                size="small"
                sx={{
                    height: 18,
                    fontSize: '0.625rem',
                    fontWeight: 600,
                    bgcolor: isDark ? alpha('#22B8CF', 0.15) : alpha('#15AABF', 0.1),
                    color: isDark ? '#22B8CF' : '#15AABF',
                    '& .MuiChip-label': { px: 0.75 },
                }}
            />
        </Box>
    );
};

/**
 * GroupItem - Cluster group that can be expanded to show clusters
 * Memoized to prevent unnecessary re-renders during data refresh
 */
const GroupItem = memo(({
    group,
    isExpanded,
    onToggle,
    expandedClusters,
    onToggleCluster,
    selectedServerId,
    onSelectServer,
    isDark,
    expandedServers,
    onToggleServer,
    user,
    onUpdateGroup,
    onUpdateCluster,
    onUpdateServer,
    onEditServer,
    onDeleteServer,
    onDeleteGroup,
}) => {
    // Superusers can edit both database-backed groups (ID: group-{number})
    // and auto-detected groups (groups with auto_group_key)
    const isEditableGroup = /^group-\d+$/.test(group.id) || !!group.auto_group_key;
    const canEditGroup = user?.isSuperuser && isEditableGroup;
    const totalServers = group.clusters?.reduce(
        (acc, c) => acc + countServersRecursive(c.servers),
        0
    ) || 0;
    const onlineServers = group.clusters?.reduce(
        (acc, c) => acc + countServersRecursive(c.servers, s => s.status === 'online'),
        0
    ) || 0;

    // Groups can be deleted if they're not auto-detected and not the default group
    const canDeleteGroup = canEditGroup && !group.auto_group_key && !group.is_default;

    return (
        <DroppableGroup groupId={group.id} isDark={isDark}>
            <Box sx={{ mb: 0.5 }}>
                <Box
                className="group-item-row"
                onClick={onToggle}
                sx={{
                    position: 'relative',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 0.75,
                    py: 1,
                    px: 1.5,
                    cursor: 'pointer',
                    borderRadius: 1,
                    mx: 1,
                    bgcolor: isDark ? alpha('#334155', 0.4) : alpha('#F3F4F6', 0.8),
                    transition: 'all 0.15s ease',
                    '&:hover': {
                        bgcolor: isDark ? alpha('#334155', 0.6) : alpha('#E5E7EB', 0.8),
                    },
                }}
            >
                {isExpanded ? (
                    <GroupOpenIcon sx={{ fontSize: 18, color: 'primary.main' }} />
                ) : (
                    <GroupIcon sx={{ fontSize: 18, color: 'text.secondary' }} />
                )}
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <InlineEditText
                        value={group.name}
                        onSave={(newName) => onUpdateGroup(group.id, newName)}
                        canEdit={canEditGroup}
                        typographyProps={{
                            variant: 'body2',
                            sx: {
                                fontWeight: 600,
                                color: 'text.primary',
                                fontSize: '0.8125rem',
                                textTransform: 'uppercase',
                                letterSpacing: '0.04em',
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                            },
                        }}
                    />
                </Box>
                <Typography
                    variant="caption"
                    sx={{
                        color: 'text.disabled',
                        fontSize: '0.6875rem',
                        ml: 'auto',
                        flexShrink: 0,
                    }}
                >
                    {onlineServers}/{totalServers}
                </Typography>
                <IconButton
                    size="small"
                    sx={{
                        p: 0.25,
                        color: 'text.secondary',
                        ml: 0.5,
                    }}
                >
                    {isExpanded ? (
                        <ExpandIcon sx={{ fontSize: 18 }} />
                    ) : (
                        <CollapseIcon sx={{ fontSize: 18 }} />
                    )}
                </IconButton>
                {canDeleteGroup && (
                    <Box
                        className="action-buttons"
                        sx={{
                            position: 'absolute',
                            right: 40,
                            top: '50%',
                            transform: 'translateY(-50%)',
                            display: 'flex',
                            gap: 0.25,
                            opacity: 0,
                            transition: 'opacity 0.15s',
                            bgcolor: isDark ? 'rgba(30, 41, 59, 0.95)' : 'rgba(255, 255, 255, 0.95)',
                            borderRadius: 1,
                            px: 0.5,
                            '.group-item-row:hover &': { opacity: 1 },
                        }}
                    >
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                e.stopPropagation();
                                onDeleteGroup?.(group);
                            }}
                            sx={{ p: 0.25, color: 'text.disabled', '&:hover': { color: 'error.main' } }}
                        >
                            <DeleteIcon sx={{ fontSize: 14 }} />
                        </IconButton>
                    </Box>
                )}
            </Box>
            <Collapse in={isExpanded} timeout="auto">
                <Box sx={{ pt: 0.5 }}>
                    {group.clusters?.map((cluster, clusterIndex) => {
                        // For cluster_type "server", handle differently based on server count
                        if (cluster.cluster_type === 'server') {
                            const serverCount = cluster.servers?.length || 0;
                            const hasMultipleServers = serverCount > 1;
                            const hasChildServers = cluster.servers?.some(s => s.children?.length > 0);
                            const clusterType = getClusterType(cluster);

                            // Single standalone server without children - no container
                            if (serverCount === 1 && !hasChildServers) {
                                const server = cluster.servers[0];
                                return (
                                    <DraggableCluster
                                        key={cluster.id}
                                        cluster={cluster}
                                        groupId={group.id}
                                        isDark={isDark}
                                        canDrag={user?.isSuperuser}
                                    >
                                        <ServerItem
                                            server={server}
                                            isSelected={selectedServerId === server.id}
                                            onSelect={onSelectServer}
                                            depth={0}
                                            isDark={isDark}
                                            expandedServers={expandedServers}
                                            onToggleServer={onToggleServer}
                                            selectedServerId={selectedServerId}
                                            isLast={true}
                                            showTreeLines={false}
                                            clusterType={clusterType}
                                            user={user}
                                            onUpdateServer={onUpdateServer}
                                            onEditServer={onEditServer}
                                            onDeleteServer={onDeleteServer}
                                        />
                                    </DraggableCluster>
                                );
                            }

                            // Multiple servers or servers with children - use container
                            return (
                                <DraggableCluster
                                    key={cluster.id}
                                    cluster={cluster}
                                    groupId={group.id}
                                    isDark={isDark}
                                    canDrag={user?.isSuperuser}
                                >
                                    <ClusterContainer cluster={cluster} isDark={isDark}>
                                        {cluster.servers?.map((server, serverIndex) => (
                                            <ServerItem
                                                key={server.id}
                                                server={server}
                                                isSelected={selectedServerId === server.id}
                                                onSelect={onSelectServer}
                                                depth={0}
                                                isDark={isDark}
                                                expandedServers={expandedServers}
                                                onToggleServer={onToggleServer}
                                                selectedServerId={selectedServerId}
                                                isLast={serverIndex === cluster.servers.length - 1}
                                                showTreeLines={hasMultipleServers || hasChildServers}
                                                clusterType={clusterType}
                                                user={user}
                                                onUpdateServer={onUpdateServer}
                                                onEditServer={onEditServer}
                                                onDeleteServer={onDeleteServer}
                                            />
                                        ))}
                                    </ClusterContainer>
                                </DraggableCluster>
                            );
                        }

                        // Regular cluster with header
                        return (
                            <DraggableCluster
                                key={cluster.id}
                                cluster={cluster}
                                groupId={group.id}
                                isDark={isDark}
                                canDrag={user?.isSuperuser}
                            >
                                <ClusterItem
                                    cluster={cluster}
                                    groupId={group.id}
                                    isExpanded={expandedClusters.has(cluster.id)}
                                    onToggle={() => onToggleCluster(cluster.id)}
                                    selectedServerId={selectedServerId}
                                    onSelectServer={onSelectServer}
                                    depth={1}
                                    isDark={isDark}
                                    expandedServers={expandedServers}
                                    onToggleServer={onToggleServer}
                                    isLast={clusterIndex === group.clusters.length - 1}
                                    user={user}
                                    onUpdateCluster={onUpdateCluster}
                                    onUpdateServer={onUpdateServer}
                                    onEditServer={onEditServer}
                                    onDeleteServer={onDeleteServer}
                                />
                            </DraggableCluster>
                        );
                    })}
                </Box>
            </Collapse>
            </Box>
        </DroppableGroup>
    );
});

// Display name for debugging
GroupItem.displayName = 'GroupItem';

// localStorage keys for persisting navigator state
const STORAGE_KEYS = {
    WIDTH: 'clusterNavigator.width',
    EXPANDED_CLUSTERS: 'clusterNavigator.expandedClusters',
};

/**
 * Load a value from localStorage with JSON parsing
 */
const loadFromStorage = (key, defaultValue) => {
    try {
        const stored = localStorage.getItem(key);
        if (stored === null) return defaultValue;
        return JSON.parse(stored);
    } catch {
        return defaultValue;
    }
};

/**
 * Save a value to localStorage with JSON serialization
 */
const saveToStorage = (key, value) => {
    try {
        localStorage.setItem(key, JSON.stringify(value));
    } catch {
        // Ignore storage errors (quota exceeded, etc.)
    }
};

/**
 * ClusterNavigator - Main navigation panel component
 */
const ClusterNavigator = ({
    data = [],
    selectedServerId,
    onSelectServer,
    onRefresh,
    loading = false,
    mode = 'light',
    defaultWidth = 280,
    minWidth = 200,
    maxWidth = 500,
}) => {
    const [searchQuery, setSearchQuery] = useState('');
    const [expandedGroups, setExpandedGroups] = useState(new Set());
    // Initialize expandedClusters from localStorage (default to empty = all collapsed)
    const [expandedClusters, setExpandedClusters] = useState(() => {
        const stored = loadFromStorage(STORAGE_KEYS.EXPANDED_CLUSTERS, []);
        return new Set(stored);
    });
    const [expandedServers, setExpandedServers] = useState(new Set());
    // Initialize panelWidth from localStorage
    const [panelWidth, setPanelWidth] = useState(() => {
        const stored = loadFromStorage(STORAGE_KEYS.WIDTH, null);
        return stored !== null ? Math.min(maxWidth, Math.max(minWidth, stored)) : defaultWidth;
    });
    const [isResizing, setIsResizing] = useState(false);
    const panelWidthRef = useRef(panelWidth);
    const resizeRef = useRef(null);
    const scrollContainerRef = useRef(null);
    const scrollPositionRef = useRef(0);
    // Track whether initial state has been set up
    const initializedRef = useRef(false);

    // Dialog states
    const [addMenuAnchor, setAddMenuAnchor] = useState(null);
    const [serverDialogOpen, setServerDialogOpen] = useState(false);
    const [serverDialogMode, setServerDialogMode] = useState('create');
    const [editingServer, setEditingServer] = useState(null);
    const [groupDialogOpen, setGroupDialogOpen] = useState(false);
    const [groupDialogMode, setGroupDialogMode] = useState('create');
    const [editingGroup, setEditingGroup] = useState(null);
    const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
    const [deleteTarget, setDeleteTarget] = useState(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    // Drag and drop state
    const [activeDragItem, setActiveDragItem] = useState(null);

    const isDark = mode === 'dark';

    // Handle resize drag
    useEffect(() => {
        const handleMouseMove = (e) => {
            if (!isResizing) return;
            const newWidth = Math.min(maxWidth, Math.max(minWidth, e.clientX));
            panelWidthRef.current = newWidth;
            setPanelWidth(newWidth);
        };

        const handleMouseUp = () => {
            setIsResizing(false);
            document.body.style.cursor = '';
            document.body.style.userSelect = '';
            // Save the final width to localStorage (use ref to get current value)
            saveToStorage(STORAGE_KEYS.WIDTH, panelWidthRef.current);
        };

        if (isResizing) {
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';
            document.addEventListener('mousemove', handleMouseMove);
            document.addEventListener('mouseup', handleMouseUp);
        }

        return () => {
            document.removeEventListener('mousemove', handleMouseMove);
            document.removeEventListener('mouseup', handleMouseUp);
        };
    }, [isResizing, minWidth, maxWidth]);

    const handleResizeStart = useCallback((e) => {
        e.preventDefault();
        setIsResizing(true);
    }, []);

    // Get user info and update functions from contexts
    const { user } = useAuth();
    const {
        updateGroupName,
        updateClusterName,
        updateServerName,
        createServer,
        updateServer,
        deleteServer,
        createGroup,
        deleteGroup,
        moveClusterToGroup,
        autoRefreshEnabled,
        setAutoRefreshEnabled,
        lastRefresh,
    } = useCluster();

    // Helper to format relative time
    const formatRelativeTime = (date) => {
        if (!date) return '';
        const seconds = Math.floor((new Date() - date) / 1000);
        if (seconds < 60) return 'just now';
        const minutes = Math.floor(seconds / 60);
        return `${minutes}m ago`;
    };

    // Handler for adding a server
    const handleAddServer = () => {
        setAddMenuAnchor(null);
        setEditingServer(null);
        setServerDialogMode('create');
        setServerDialogOpen(true);
    };

    // Handler for editing a server
    const handleEditServer = (server) => {
        setEditingServer(server);
        setServerDialogMode('edit');
        setServerDialogOpen(true);
    };

    // Handler for saving a server (create or update)
    const handleSaveServer = async (serverData) => {
        if (serverDialogMode === 'create') {
            await createServer(serverData);
        } else {
            await updateServer(editingServer.id, serverData);
        }
        setServerDialogOpen(false);
    };

    // Handler for deleting a server
    const handleDeleteServer = (server) => {
        setDeleteTarget({ type: 'server', item: server });
        setDeleteDialogOpen(true);
    };

    // Handler for adding a group
    const handleAddGroup = () => {
        setAddMenuAnchor(null);
        setEditingGroup(null);
        setGroupDialogMode('create');
        setGroupDialogOpen(true);
    };

    // Handler for editing a group
    const handleEditGroup = (group) => {
        setEditingGroup(group);
        setGroupDialogMode('edit');
        setGroupDialogOpen(true);
    };

    // Handler for saving a group
    const handleSaveGroup = async (groupData) => {
        if (groupDialogMode === 'create') {
            await createGroup(groupData);
        } else {
            // For edit, we just update the name using existing function
            await updateGroupName(editingGroup.id, groupData.name);
        }
        setGroupDialogOpen(false);
    };

    // Handler for deleting a group
    const handleDeleteGroup = (group) => {
        setDeleteTarget({ type: 'group', item: group });
        setDeleteDialogOpen(true);
    };

    // Handler for confirming delete
    const handleConfirmDelete = async () => {
        if (!deleteTarget) return;

        setDeleteLoading(true);
        try {
            if (deleteTarget.type === 'server') {
                await deleteServer(deleteTarget.item.id);
            } else {
                await deleteGroup(deleteTarget.item.id);
            }
            setDeleteDialogOpen(false);
            setDeleteTarget(null);
        } catch (error) {
            // Error handling - could show a toast
            console.error('Delete failed:', error);
        } finally {
            setDeleteLoading(false);
        }
    };

    // Drag and drop handlers
    const handleDragStart = (event) => {
        const { active } = event;
        if (active.data.current?.type === 'cluster') {
            setActiveDragItem(active.data.current.cluster);
        }
    };

    const handleDragEnd = async (event) => {
        const { active, over } = event;
        setActiveDragItem(null);

        if (!over || !active.data.current) return;

        const dragData = active.data.current;
        const dropData = over.data.current;

        // Only handle dropping on groups
        if (dragData.type !== 'cluster' || dropData?.type !== 'group') return;

        const sourceGroupId = dragData.groupId;
        const targetGroupId = dropData.groupId;

        // Don't move if dropping on same group
        if (sourceGroupId === targetGroupId) return;

        try {
            await moveClusterToGroup(
                dragData.cluster.id,
                targetGroupId,
                dragData.cluster.auto_cluster_key,
                dragData.cluster.name
            );
        } catch (error) {
            console.error('Failed to move cluster:', error);
        }
    };

    /**
     * Collect all expandable server IDs recursively
     */
    const collectExpandableServerIds = (servers) => {
        if (!servers) return [];
        return servers.flatMap(server => {
            const ids = server.is_expandable || server.children?.length > 0 ? [server.id] : [];
            return [...ids, ...collectExpandableServerIds(server.children)];
        });
    };

    // Initialize groups and servers on first data load
    // Groups are always expanded by default to show clusters
    // Clusters default to collapsed unless restored from localStorage
    // Expandable servers (with children) are expanded by default
    React.useEffect(() => {
        if (data.length > 0 && !initializedRef.current) {
            initializedRef.current = true;

            // Always expand all groups to show clusters
            const allGroupIds = new Set(data.map(g => g.id));
            setExpandedGroups(allGroupIds);

            // Expand all expandable servers (those with children) by default
            const allExpandableServerIds = new Set(
                data.flatMap(g =>
                    g.clusters?.flatMap(c => collectExpandableServerIds(c.servers)) || []
                )
            );
            setExpandedServers(allExpandableServerIds);

            // Note: expandedClusters is already initialized from localStorage
            // and defaults to empty (all collapsed) if no saved state exists
        }
    }, [data]);

    // Preserve scroll position across data updates
    // Save scroll position before data changes, restore after render
    useEffect(() => {
        const scrollContainer = scrollContainerRef.current;
        if (scrollContainer && scrollPositionRef.current > 0) {
            // Restore scroll position after data update
            scrollContainer.scrollTop = scrollPositionRef.current;
        }
    }, [data]);

    // Track scroll position changes
    const handleScroll = useCallback((e) => {
        scrollPositionRef.current = e.target.scrollTop;
    }, []);

    /**
     * Recursively filter servers by search query, including children
     */
    const filterServersRecursive = (servers, query) => {
        if (!servers) return [];

        return servers.reduce((result, server) => {
            const serverMatches =
                server.name.toLowerCase().includes(query) ||
                server.host?.toLowerCase().includes(query);

            // Recursively filter children
            const filteredChildren = filterServersRecursive(server.children, query);

            // Include server if it matches or has matching children
            if (serverMatches || filteredChildren.length > 0) {
                result.push({
                    ...server,
                    children: serverMatches ? server.children : filteredChildren,
                });
            }

            return result;
        }, []);
    };

    // Filter data based on search query
    const filteredData = useMemo(() => {
        if (!searchQuery.trim()) return data;

        const query = searchQuery.toLowerCase();

        return data.map(group => {
            const filteredClusters = group.clusters?.map(cluster => {
                const filteredServers = filterServersRecursive(cluster.servers, query);

                if (filteredServers?.length > 0 || cluster.name.toLowerCase().includes(query)) {
                    return { ...cluster, servers: filteredServers.length > 0 ? filteredServers : cluster.servers };
                }
                return null;
            }).filter(Boolean);

            if (filteredClusters?.length > 0 || group.name.toLowerCase().includes(query)) {
                return { ...group, clusters: filteredClusters || group.clusters };
            }
            return null;
        }).filter(Boolean);
    }, [data, searchQuery]);

    const toggleGroup = (groupId) => {
        setExpandedGroups(prev => {
            const next = new Set(prev);
            if (next.has(groupId)) {
                next.delete(groupId);
            } else {
                next.add(groupId);
            }
            return next;
        });
    };

    const toggleCluster = (clusterId) => {
        setExpandedClusters(prev => {
            const next = new Set(prev);
            if (next.has(clusterId)) {
                next.delete(clusterId);
            } else {
                next.add(clusterId);
            }
            // Persist expanded clusters to localStorage
            saveToStorage(STORAGE_KEYS.EXPANDED_CLUSTERS, Array.from(next));
            return next;
        });
    };

    const toggleServer = (serverId) => {
        setExpandedServers(prev => {
            const next = new Set(prev);
            if (next.has(serverId)) {
                next.delete(serverId);
            } else {
                next.add(serverId);
            }
            return next;
        });
    };

    // Calculate totals (using recursive counting)
    const totalServers = data.reduce(
        (acc, g) => acc + (g.clusters?.reduce(
            (a, c) => a + countServersRecursive(c.servers), 0
        ) || 0),
        0
    );
    const onlineServers = data.reduce(
        (acc, g) => acc + (g.clusters?.reduce(
            (a, c) => a + countServersRecursive(c.servers, s => s.status === 'online'), 0
        ) || 0),
        0
    );

    return (
        <DndContext
            collisionDetection={pointerWithin}
            onDragStart={handleDragStart}
            onDragEnd={handleDragEnd}
        >
            <Box
                sx={{
                    width: panelWidth,
                    height: '100%',
                    display: 'flex',
                    flexDirection: 'column',
                    bgcolor: isDark ? '#1E293B' : '#FFFFFF',
                    borderRight: '1px solid',
                    borderColor: isDark ? '#334155' : '#E5E7EB',
                    position: 'relative',
                    flexShrink: 0,
                }}
            >
                {/* Resize handle */}
            <Box
                ref={resizeRef}
                onMouseDown={handleResizeStart}
                sx={{
                    position: 'absolute',
                    top: 0,
                    right: -3,
                    bottom: 0,
                    width: 6,
                    cursor: 'col-resize',
                    zIndex: 10,
                    '&:hover': {
                        bgcolor: alpha(isDark ? '#60A5FA' : '#3B82F6', 0.3),
                    },
                    ...(isResizing && {
                        bgcolor: alpha(isDark ? '#60A5FA' : '#3B82F6', 0.5),
                    }),
                }}
            />
            {/* Header */}
            <Box
                sx={{
                    px: 2,
                    py: 1.5,
                    borderBottom: '1px solid',
                    borderColor: isDark ? '#334155' : '#E5E7EB',
                }}
            >
                <Box sx={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    mb: 1.5,
                }}>
                    <Typography
                        variant="overline"
                        sx={{
                            color: 'text.primary',
                            fontSize: '0.6875rem',
                            fontWeight: 600,
                            letterSpacing: '0.08em',
                        }}
                    >
                        Database Servers
                    </Typography>
                    <Box sx={{ display: 'flex', gap: 0.5 }}>
                        <Tooltip title="Add server or group">
                            <IconButton
                                size="small"
                                onClick={(e) => setAddMenuAnchor(e.currentTarget)}
                                sx={{
                                    p: 0.5,
                                    color: isDark ? 'rgba(255,255,255,0.7)' : 'text.secondary',
                                    '&:hover': { color: 'primary.main' },
                                }}
                            >
                                <AddIcon sx={{ fontSize: 18 }} />
                            </IconButton>
                        </Tooltip>
                        <Tooltip title={autoRefreshEnabled ? 'Auto-refresh enabled' : 'Auto-refresh disabled'}>
                            <IconButton
                                size="small"
                                onClick={() => setAutoRefreshEnabled(!autoRefreshEnabled)}
                                sx={{
                                    p: 0.5,
                                    color: autoRefreshEnabled
                                        ? (isDark ? '#22B8CF' : '#15AABF')
                                        : (isDark ? 'rgba(255,255,255,0.4)' : 'text.disabled'),
                                }}
                            >
                                <AutorenewIcon sx={{ fontSize: 18 }} />
                            </IconButton>
                        </Tooltip>
                        <Tooltip title="Refresh">
                            <IconButton
                                size="small"
                                onClick={onRefresh}
                                disabled={loading}
                                sx={{
                                    p: 0.5,
                                    color: 'text.secondary',
                                    '&:hover': { color: 'primary.main' },
                                }}
                            >
                                <RefreshIcon
                                    sx={{
                                        fontSize: 18,
                                        animation: loading ? 'spin 1s linear infinite' : 'none',
                                        '@keyframes spin': {
                                            '0%': { transform: 'rotate(0deg)' },
                                            '100%': { transform: 'rotate(360deg)' },
                                        },
                                    }}
                                />
                            </IconButton>
                        </Tooltip>
                    </Box>
                </Box>

                {/* Status summary */}
                <Box sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 1,
                    mb: 1.5,
                }}>
                    <Chip
                        icon={<StatusIcon sx={{ fontSize: '8px !important', color: STATUS_COLORS.online }} />}
                        label={`${onlineServers} online`}
                        size="small"
                        sx={{
                            height: 22,
                            fontSize: '0.6875rem',
                            bgcolor: isDark ? alpha(STATUS_COLORS.online, 0.12) : alpha(STATUS_COLORS.online, 0.08),
                            color: isDark ? '#4ADE80' : '#16A34A',
                            '& .MuiChip-icon': { ml: 0.75 },
                            '& .MuiChip-label': { px: 0.75 },
                        }}
                    />
                    <Typography
                        variant="caption"
                        sx={{
                            color: 'text.secondary',
                            fontSize: '0.6875rem',
                        }}
                    >
                        of {totalServers} servers
                    </Typography>
                </Box>

                {/* Search */}
                <TextField
                    size="small"
                    placeholder="Search servers..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    fullWidth
                    InputProps={{
                        startAdornment: (
                            <InputAdornment position="start">
                                <SearchIcon sx={{ fontSize: 18, color: 'text.disabled' }} />
                            </InputAdornment>
                        ),
                    }}
                    sx={{
                        '& .MuiOutlinedInput-root': {
                            bgcolor: isDark ? alpha('#0F172A', 0.5) : alpha('#F9FAFB', 0.8),
                            fontSize: '0.8125rem',
                            '& fieldset': {
                                borderColor: isDark ? '#334155' : '#E5E7EB',
                            },
                            '&:hover fieldset': {
                                borderColor: isDark ? '#475569' : '#D1D5DB',
                            },
                        },
                        '& .MuiOutlinedInput-input': {
                            py: 0.875,
                            '&::placeholder': {
                                color: 'text.disabled',
                                opacity: 1,
                            },
                        },
                    }}
                />
            </Box>

            {/* Navigation Tree */}
            <Box
                ref={scrollContainerRef}
                onScroll={handleScroll}
                sx={{
                    flex: 1,
                    overflow: 'auto',
                    py: 1,
                }}
            >
                {loading ? (
                    // Loading skeletons
                    <Box sx={{ px: 2 }}>
                        {[1, 2, 3].map((i) => (
                            <Box key={i} sx={{ mb: 2 }}>
                                <Skeleton
                                    variant="rounded"
                                    height={36}
                                    sx={{
                                        mb: 1,
                                        bgcolor: isDark ? '#334155' : '#E5E7EB',
                                    }}
                                />
                                {[1, 2].map((j) => (
                                    <Skeleton
                                        key={j}
                                        variant="rounded"
                                        height={28}
                                        sx={{
                                            ml: 2,
                                            mb: 0.5,
                                            bgcolor: isDark ? '#334155' : '#E5E7EB',
                                        }}
                                    />
                                ))}
                            </Box>
                        ))}
                    </Box>
                ) : filteredData.length === 0 ? (
                    // Empty state
                    <Box
                        sx={{
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            justifyContent: 'center',
                            py: 4,
                            px: 2,
                            textAlign: 'center',
                        }}
                    >
                        <ServerIcon
                            sx={{
                                fontSize: 48,
                                color: 'text.disabled',
                                mb: 1.5,
                            }}
                        />
                        <Typography
                            variant="body2"
                            sx={{
                                color: 'text.secondary',
                                mb: 0.5,
                            }}
                        >
                            {searchQuery ? 'No servers found' : 'No servers configured'}
                        </Typography>
                        <Typography
                            variant="caption"
                            sx={{ color: 'text.disabled' }}
                        >
                            {searchQuery
                                ? 'Try a different search term'
                                : 'Add a server to get started'
                            }
                        </Typography>
                    </Box>
                ) : (
                    // Render groups, clusters, and servers
                    filteredData.map((group) => (
                        <GroupItem
                            key={group.id}
                            group={group}
                            isExpanded={expandedGroups.has(group.id)}
                            onToggle={() => toggleGroup(group.id)}
                            expandedClusters={expandedClusters}
                            onToggleCluster={toggleCluster}
                            selectedServerId={selectedServerId}
                            onSelectServer={onSelectServer}
                            isDark={isDark}
                            expandedServers={expandedServers}
                            onToggleServer={toggleServer}
                            user={user}
                            onUpdateGroup={updateGroupName}
                            onUpdateCluster={updateClusterName}
                            onUpdateServer={updateServerName}
                            onEditServer={handleEditServer}
                            onDeleteServer={handleDeleteServer}
                            onDeleteGroup={handleDeleteGroup}
                        />
                    ))
                )}
            </Box>

            {/* Footer */}
            <Box
                sx={{
                    px: 2,
                    py: 1,
                    borderTop: '1px solid',
                    borderColor: isDark ? '#334155' : '#E5E7EB',
                    bgcolor: isDark ? alpha('#0F172A', 0.5) : alpha('#F9FAFB', 0.5),
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                }}
            >
                <Typography
                    variant="caption"
                    sx={{
                        color: 'text.disabled',
                        fontSize: '0.625rem',
                    }}
                >
                    {filteredData.length} groups • {
                        filteredData.reduce((a, g) => a + (g.clusters?.length || 0), 0)
                    } clusters
                </Typography>
                {lastRefresh && (
                    <Typography
                        variant="caption"
                        sx={{
                            color: isDark ? 'rgba(255,255,255,0.5)' : 'text.disabled',
                            fontSize: '0.625rem',
                        }}
                    >
                        Updated {formatRelativeTime(lastRefresh)}
                    </Typography>
                )}
            </Box>

            {/* Add Menu */}
            <AddMenu
                anchorEl={addMenuAnchor}
                open={Boolean(addMenuAnchor)}
                onClose={() => setAddMenuAnchor(null)}
                onAddServer={handleAddServer}
                onAddGroup={handleAddGroup}
            />

            {/* Server Dialog */}
            <ServerDialog
                open={serverDialogOpen}
                onClose={() => setServerDialogOpen(false)}
                onSave={handleSaveServer}
                mode={serverDialogMode}
                server={editingServer}
                isSuperuser={user?.isSuperuser}
            />

            {/* Group Dialog */}
            <GroupDialog
                open={groupDialogOpen}
                onClose={() => setGroupDialogOpen(false)}
                onSave={handleSaveGroup}
                mode={groupDialogMode}
                group={editingGroup}
                isSuperuser={user?.isSuperuser}
            />

            {/* Delete Confirmation Dialog */}
            <DeleteConfirmationDialog
                open={deleteDialogOpen}
                onClose={() => {
                    setDeleteDialogOpen(false);
                    setDeleteTarget(null);
                }}
                onConfirm={handleConfirmDelete}
                title={deleteTarget?.type === 'server' ? 'Delete Server' : 'Delete Cluster Group'}
                message={deleteTarget?.type === 'server'
                    ? 'Are you sure you want to delete this server? This action cannot be undone.'
                    : 'Are you sure you want to delete this group? Servers in this group will be moved to Ungrouped.'}
                itemName={deleteTarget?.item?.name}
                loading={deleteLoading}
            />
            </Box>

            {/* Drag overlay for visual feedback */}
            <DragOverlay>
                {activeDragItem && (
                    <DragOverlayContent cluster={activeDragItem} isDark={isDark} />
                )}
            </DragOverlay>
        </DndContext>
    );
};

export default ClusterNavigator;
