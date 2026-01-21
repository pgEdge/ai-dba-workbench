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

import React, { useState, useMemo } from 'react';
import {
    Box,
    Typography,
    IconButton,
    Collapse,
    Tooltip,
    TextField,
    InputAdornment,
    Chip,
    Divider,
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
} from '@mui/icons-material';

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

// Tree spacing constants
const TREE_SPACING = {
    INDENT_SIZE: 20,
    LINE_WIDTH: 1,
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
 * Format role for display - converts snake_case to readable format
 */
const formatRole = (role) => {
    if (!role) return null;
    // Convert snake_case to readable format
    const roleMap = {
        'binary_primary': 'Primary',
        'binary_standby': 'Binary Standby',
        'binary_cascading': 'Cascading Standby',
        'spock_node': 'Spock Node',
        'standalone': 'Standalone',
        'logical_publisher': 'Logical Publisher',
        'logical_subscriber': 'Logical Replica',
        'logical_bidirectional': 'Logical Bidirectional',
        'bdr_node': 'BDR Node',
        'unknown': null,
    };
    return roleMap[role] || role.replace(/_/g, ' ').toUpperCase();
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
 */
const ServerItem = ({
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
}) => {
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

    // Calculate indentation based on depth
    const baseIndent = 1.5; // Base left padding
    const depthIndent = depth * 2.5; // Additional indent per depth level
    const expanderWidth = 2; // Width reserved for expand/collapse button

    // Calculate tree line position to align with parent's expand icon
    // Parent's expand icon center is at: baseIndent + (depth-1)*depthIndent + iconOffset
    // In pixels: 12 + (depth-1)*20 + 10 = 22 + (depth-1)*20
    const lineLeftPos = 22 + (depth - 1) * 20;

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
                            bottom: isLast ? '50%' : 0,
                            width: '1px',
                            bgcolor: lineColor,
                        }}
                    />
                    {/* Horizontal connector to this node */}
                    <Box
                        sx={{
                            position: 'absolute',
                            left: `${lineLeftPos}px`,
                            top: '50%',
                            width: '14px',
                            height: '1px',
                            bgcolor: lineColor,
                        }}
                    />
                </>
            )}
            <Box
                onClick={handleSelect}
                sx={{
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
                    <Typography
                        variant="body2"
                        sx={{
                            fontWeight: isSelected ? 600 : 400,
                            color: isSelected ? 'text.primary' : 'text.secondary',
                            fontSize: '0.8125rem',
                            lineHeight: 1.3,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                            opacity: server.status === 'offline' ? 0.6 : 1,
                        }}
                    >
                        {server.name}
                    </Typography>
                </Box>
                {effectiveRole && ROLE_CONFIGS[effectiveRole] && (
                    <RolePill role={effectiveRole} isDark={isDark} />
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
                            />
                        ))}
                    </Box>
                </Collapse>
            )}
        </Box>
    );
};

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
 */
const ClusterItem = ({
    cluster,
    isExpanded,
    onToggle,
    selectedServerId,
    onSelectServer,
    depth = 0,
    isDark,
    expandedServers,
    onToggleServer,
    isLast = false,
}) => {
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
                    <Typography
                        variant="body2"
                        sx={{
                            fontWeight: 500,
                            color: 'text.primary',
                            fontSize: '0.8125rem',
                            lineHeight: 1.3,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                        }}
                    >
                        {cluster.name}
                    </Typography>
                </Box>
                <Chip
                    label={`${onlineCount}/${totalCount}`}
                    size="small"
                    sx={{
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
                        />
                    ))}
                </Box>
            </Collapse>
        </ClusterContainer>
    );
};

/**
 * GroupItem - Cluster group that can be expanded to show clusters
 */
const GroupItem = ({
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
}) => {
    const totalServers = group.clusters?.reduce(
        (acc, c) => acc + countServersRecursive(c.servers),
        0
    ) || 0;
    const onlineServers = group.clusters?.reduce(
        (acc, c) => acc + countServersRecursive(c.servers, s => s.status === 'online'),
        0
    ) || 0;

    return (
        <Box sx={{ mb: 0.5 }}>
            <Box
                onClick={onToggle}
                sx={{
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
                    <Typography
                        variant="body2"
                        sx={{
                            fontWeight: 600,
                            color: 'text.primary',
                            fontSize: '0.8125rem',
                            textTransform: 'uppercase',
                            letterSpacing: '0.04em',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                        }}
                    >
                        {group.name}
                    </Typography>
                </Box>
                <Typography
                    variant="caption"
                    sx={{
                        color: 'text.disabled',
                        fontSize: '0.6875rem',
                    }}
                >
                    {onlineServers}/{totalServers}
                </Typography>
                <IconButton
                    size="small"
                    sx={{
                        p: 0.25,
                        color: 'text.secondary',
                    }}
                >
                    {isExpanded ? (
                        <ExpandIcon sx={{ fontSize: 18 }} />
                    ) : (
                        <CollapseIcon sx={{ fontSize: 18 }} />
                    )}
                </IconButton>
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
                                        isLast={true}
                                        showTreeLines={false}
                                        clusterType={clusterType}
                                    />
                                );
                            }

                            // Multiple servers or servers with children - use container
                            return (
                                <ClusterContainer key={cluster.id} cluster={cluster} isDark={isDark}>
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
                                        />
                                    ))}
                                </ClusterContainer>
                            );
                        }

                        // Regular cluster with header
                        return (
                            <ClusterItem
                                key={cluster.id}
                                cluster={cluster}
                                isExpanded={expandedClusters.has(cluster.id)}
                                onToggle={() => onToggleCluster(cluster.id)}
                                selectedServerId={selectedServerId}
                                onSelectServer={onSelectServer}
                                depth={1}
                                isDark={isDark}
                                expandedServers={expandedServers}
                                onToggleServer={onToggleServer}
                                isLast={clusterIndex === group.clusters.length - 1}
                            />
                        );
                    })}
                </Box>
            </Collapse>
        </Box>
    );
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
    width = 280,
}) => {
    const [searchQuery, setSearchQuery] = useState('');
    const [expandedGroups, setExpandedGroups] = useState(new Set());
    const [expandedClusters, setExpandedClusters] = useState(new Set());
    const [expandedServers, setExpandedServers] = useState(new Set());

    const isDark = mode === 'dark';

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

    // Initialize all groups, clusters, and expandable servers as expanded on first render
    React.useEffect(() => {
        if (data.length > 0 && expandedGroups.size === 0) {
            const allGroupIds = new Set(data.map(g => g.id));
            const allClusterIds = new Set(
                data.flatMap(g => g.clusters?.map(c => c.id) || [])
            );
            const allExpandableServerIds = new Set(
                data.flatMap(g =>
                    g.clusters?.flatMap(c => collectExpandableServerIds(c.servers)) || []
                )
            );
            setExpandedGroups(allGroupIds);
            setExpandedClusters(allClusterIds);
            setExpandedServers(allExpandableServerIds);
        }
    }, [data]);

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
        <Box
            sx={{
                width,
                height: '100%',
                display: 'flex',
                flexDirection: 'column',
                bgcolor: isDark ? '#1E293B' : '#FFFFFF',
                borderRight: '1px solid',
                borderColor: isDark ? '#334155' : '#E5E7EB',
            }}
        >
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
                            color: 'text.secondary',
                            fontSize: '0.6875rem',
                            fontWeight: 600,
                            letterSpacing: '0.08em',
                        }}
                    >
                        Database Servers
                    </Typography>
                    <Box sx={{ display: 'flex', gap: 0.5 }}>
                        <Tooltip title="Add server">
                            <IconButton
                                size="small"
                                sx={{
                                    p: 0.5,
                                    color: 'text.secondary',
                                    '&:hover': { color: 'primary.main' },
                                }}
                            >
                                <AddIcon sx={{ fontSize: 18 }} />
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
                            color: 'text.disabled',
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
            </Box>
        </Box>
    );
};

export default ClusterNavigator;
