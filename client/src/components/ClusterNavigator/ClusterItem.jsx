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
    useTheme,
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

// -- Static sx constants --------------------------------------------------

const expandButtonSx = { p: 0.25, color: 'text.secondary' };
const expandIcon18Sx = { fontSize: 18 };
const flexMinWidthSx = { flex: 1, minWidth: 0 };
const trailingSx = { ml: 'auto', flexShrink: 0 };
const serverListSx = { pb: 0.5 };

const clusterNameBase = {
    fontSize: '0.8125rem',
    lineHeight: 1.3,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const countChipBase = {
    height: 18,
    fontSize: '0.625rem',
    fontWeight: 600,
    '& .MuiChip-label': { px: 0.75 },
};

// -- Style-getter functions -----------------------------------------------

const getHeaderSx = (theme, isSelected) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    py: 0.75,
    px: 1,
    cursor: 'pointer',
    borderRadius: 1,
    mx: 0.5,
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

const getClusterIconSx = (isSelected) => ({
    fontSize: 18,
    color: isSelected ? 'primary.main' : 'text.secondary',
});

const getClusterNameSx = (isSelected) => ({
    ...clusterNameBase,
    fontWeight: isSelected ? 600 : 500,
    color: 'text.primary',
});

const getCountChipSx = (theme) => ({
    ...countChipBase,
    bgcolor: alpha(theme.palette.grey[500], theme.palette.mode === 'dark' ? 0.2 : 0.1),
    color: theme.palette.grey[theme.palette.mode === 'dark' ? 400 : 500],
});

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
    const theme = useTheme();

    // Superusers can edit:
    // - Database-backed clusters (cluster-{id} format)
    // - Auto-detected clusters that have auto_cluster_key (binary, logical, spock)
    const isDbBackedCluster = /^cluster-\d+$/.test(cluster.id);
    const isAutoDetectedCluster = cluster.auto_cluster_key ? true : false;
    const canEditCluster = user?.isSuperuser && (isDbBackedCluster || isAutoDetectedCluster);
    const totalCount = countServersRecursive(cluster.servers);
    const offlineCount = countServersRecursive(cluster.servers, s => s.status === 'offline');
    const onlineCount = countServersRecursive(cluster.servers, s => s.status !== 'offline');
    const warningCount = totalCount - offlineCount - onlineCount;
    const allOffline = totalCount > 0 && offlineCount === totalCount;

    const clusterStatus = allOffline ? 'offline' : (warningCount > 0 || offlineCount > 0 ? 'warning' : 'online');
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
                sx={getHeaderSx(theme, isSelected)}
            >
                <IconButton
                    size="small"
                    sx={expandButtonSx}
                    onClick={(e) => {
                        e.stopPropagation();
                        onToggle();
                    }}
                >
                    {isExpanded ? (
                        <ExpandIcon sx={expandIcon18Sx} />
                    ) : (
                        <CollapseIcon sx={expandIcon18Sx} />
                    )}
                </IconButton>
                <StatusIndicator status={clusterStatus} alertCount={clusterAlertCount} />
                <ClusterIcon sx={getClusterIconSx(isSelected)} />
                <Box sx={flexMinWidthSx}>
                    <InlineEditText
                        value={cluster.name}
                        onSave={(newName) => onUpdateCluster(cluster.id, newName, groupId, cluster.auto_cluster_key)}
                        canEdit={canEditCluster}
                        typographyProps={{
                            variant: 'body2',
                            sx: getClusterNameSx(isSelected),
                        }}
                    />
                </Box>
                <Box sx={trailingSx}>
                    <Chip
                        label={`${onlineCount}/${totalCount}`}
                        size="small"
                        sx={getCountChipSx(theme)}
                    />
                </Box>
            </Box>
            {/* Server List */}
            <Collapse in={isExpanded} timeout="auto">
                <Box sx={serverListSx}>
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
