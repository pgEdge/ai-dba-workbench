/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { memo, useMemo } from 'react';
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
    Dns as ClusterIcon,
} from '@mui/icons-material';
import InlineEditText from '../InlineEditText';
import { getClusterType, countServersRecursive } from './utils';
import ClusterContainer from './ClusterContainer';
import StatusIndicator from './StatusIndicator';
import ServerItem from './ServerItem';

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
    selectedClusterId,
    onSelectServer,
    onSelectCluster,
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
    getServerAlertCount,
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
    const clusterType = getClusterType(cluster);
    const isSelected = selectedClusterId === cluster.id;

    // Calculate total alert count for the cluster
    const clusterAlertCount = useMemo(() => {
        if (!getServerAlertCount) return 0;
        const collectServerIds = (servers) => {
            const ids = [];
            servers?.forEach(s => {
                ids.push(s.id);
                if (s.children) ids.push(...collectServerIds(s.children));
            });
            return ids;
        };
        const serverIds = collectServerIds(cluster.servers);
        return serverIds.reduce((sum, id) => sum + (getServerAlertCount(id) || 0), 0);
    }, [cluster.servers, getServerAlertCount]);

    const handleClusterClick = (e) => {
        // Don't select if clicking on expand button or inline edit
        if (e.target.closest('.MuiIconButton-root') || e.target.closest('.inline-edit-input')) {
            return;
        }
        if (onSelectCluster) {
            onSelectCluster(cluster);
        }
    };

    return (
        <ClusterContainer cluster={cluster} isDark={isDark}>
            {/* Cluster Header */}
            <Box
                onClick={handleClusterClick}
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 0.75,
                    py: 0.75,
                    px: 1,
                    cursor: 'pointer',
                    borderRadius: 1,
                    mx: 0.5,
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
                <StatusIndicator status={clusterStatus} alertCount={clusterAlertCount} isDark={isDark} />
                <ClusterIcon
                    sx={{
                        fontSize: 18,
                        color: isSelected ? 'primary.main' : 'text.secondary',
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
                                fontWeight: isSelected ? 600 : 500,
                                color: isSelected ? 'text.primary' : 'text.primary',
                                fontSize: '0.8125rem',
                                lineHeight: 1.3,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                            },
                        }}
                    />
                </Box>
                <Box sx={{ ml: 'auto', flexShrink: 0 }}>
                    <Chip
                        label={`${onlineCount}/${totalCount}`}
                        size="small"
                        sx={{
                            height: 18,
                            fontSize: '0.625rem',
                            fontWeight: 600,
                            bgcolor: isDark ? alpha('#64748B', 0.2) : alpha('#64748B', 0.1),
                            color: isDark ? '#94A3B8' : '#64748B',
                            '& .MuiChip-label': {
                                px: 0.75,
                            },
                        }}
                    />
                </Box>
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
                            alertCount={getServerAlertCount ? getServerAlertCount(server.id) : 0}
                            getServerAlertCount={getServerAlertCount}
                        />
                    ))}
                </Box>
            </Collapse>
        </ClusterContainer>
    );
});

// Display name for debugging
ClusterItem.displayName = 'ClusterItem';

export default ClusterItem;
