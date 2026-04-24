/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { memo, useMemo } from 'react';
import {
    Box,
    IconButton,
    Collapse,
    Chip,
    alpha,
    useTheme,
} from '@mui/material';
import type { Theme } from '@mui/material/styles';
import type { Cluster, Server } from './utils';
import {
    ExpandMore as ExpandIcon,
    ChevronRight as CollapseIcon,
    Dns as ClusterIcon,
    Settings as SettingsIcon,
    DeleteOutline as DeleteIcon,
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
    fontSize: '0.875rem',
    lineHeight: 1.3,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const countChipBase = {
    height: 18,
    maxWidth: 'none',
    fontSize: '0.875rem',
    fontWeight: 600,
    '& .MuiChip-label': { px: 0.75 },
};

const settingsIconSx = { fontSize: 14 };
const settingsButtonSx = { p: 0.25, color: 'text.disabled', '&:hover': { color: 'primary.main' } };
const deleteIconSx = { fontSize: 14 };
const deleteButtonSx = { p: 0.25, color: 'text.disabled', '&:hover': { color: 'error.main' } };

// -- Style-getter functions -----------------------------------------------

const getHeaderSx = (theme: Theme, isSelected: boolean) => ({
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

const getClusterNameSx = (_isSelected) => ({
    ...clusterNameBase,
    fontWeight: 600,
    color: 'text.primary',
});

const getClusterActionsSx = (theme: Theme) => ({
    position: 'absolute',
    right: 40,
    top: '50%',
    transform: 'translateY(-50%)',
    display: 'flex',
    gap: 0.25,
    opacity: 0,
    transition: 'opacity 0.15s',
    bgcolor: alpha(theme.palette.background.paper, 0.95),
    borderRadius: 1,
    px: 0.5,
    '.cluster-header:hover &': { opacity: 1 },
});

const getCountChipSx = (theme: Theme) => ({
    ...countChipBase,
    bgcolor: alpha(theme.palette.grey[500], theme.palette.mode === 'dark' ? 0.2 : 0.1),
    color: theme.palette.grey[theme.palette.mode === 'dark' ? 400 : 500],
});

/**
 * ClusterItem - Cluster entry that can be expanded to show member servers
 * Entire cluster (header + servers) is wrapped in a container
 * Memoized to prevent unnecessary re-renders during data refresh
 */
interface UserInfo {
    isSuperuser?: boolean;
    username?: string;
}

interface ClusterItemProps {
    cluster: Cluster;
    groupId: string;
    isExpanded: boolean;
    onToggle: () => void;
    selectedServerId?: number;
    selectedClusterId?: string;
    onSelectServer: (server: Server) => void;
    onSelectCluster?: (cluster: Cluster) => void;
    depth?: number;
    isDark: boolean;
    expandedServers?: Set<number>;
    onToggleServer?: (serverId: number) => void;
    isLast?: boolean;
    user?: UserInfo;
    onUpdateCluster: (clusterId: string, newName: string, groupId: string, autoClusterKey?: string) => Promise<void>;
    onUpdateServer: (serverId: number, newName: string) => Promise<void>;
    onEditServer?: (server: Server) => void;
    onDeleteServer?: (server: Server) => void;
    onConfigureCluster?: (cluster: Cluster) => void;
    onDeleteCluster?: (cluster: Cluster) => void;
    getServerAlertCount?: (serverId: number) => number;
    getServerBlackoutStatus?: (serverId: number) => { active: boolean; inherited: boolean };
    getClusterBlackoutStatus?: (clusterId: string) => { active: boolean; inherited: boolean };
}

const ClusterItem = memo<ClusterItemProps>(({
    cluster,
    groupId,
    isExpanded,
    onToggle,
    selectedServerId,
    selectedClusterId,
    onSelectServer,
    onSelectCluster,
    depth: _depth = 0,
    isDark,
    expandedServers,
    onToggleServer,
    isLast: _isLast = false,
    user,
    onUpdateCluster,
    onUpdateServer,
    onEditServer,
    onDeleteServer,
    onConfigureCluster,
    onDeleteCluster,
    getServerAlertCount,
    getServerBlackoutStatus,
    getClusterBlackoutStatus,
}) => {
    const theme = useTheme();

    // Superusers can edit:
    // - Database-backed clusters (cluster-{id} format)
    // - Auto-detected clusters that have auto_cluster_key (binary, logical, spock)
    const isDbBackedCluster = /^cluster-\d+$/.test(cluster.id);
    const isAutoDetectedCluster = !!cluster.auto_cluster_key;
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
        if (!getServerAlertCount) {return 0;}
        const collectServerIds = (servers: Server[] | undefined): number[] => {
            const ids: number[] = [];
            servers?.forEach(s => {
                ids.push(s.id);
                if (s.children) {ids.push(...collectServerIds(s.children));}
            });
            return ids;
        };
        const serverIds = collectServerIds(cluster.servers);
        return serverIds.reduce((sum, id) => sum + (getServerAlertCount(id) || 0), 0);
    }, [cluster.servers, getServerAlertCount]);

    const handleClusterClick = (e: React.MouseEvent<HTMLDivElement>) => {
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
                className="cluster-header"
                onClick={handleClusterClick}
                sx={{ ...getHeaderSx(theme, isSelected), position: 'relative' }}
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
                <StatusIndicator
                    status={clusterStatus}
                    alertCount={clusterAlertCount}
                    blackoutActive={getClusterBlackoutStatus?.(cluster.id)?.active}
                    blackoutInherited={getClusterBlackoutStatus?.(cluster.id).inherited}
                />
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
                {canEditCluster && (onConfigureCluster || (onDeleteCluster && totalCount === 0)) && (
                    <Box sx={getClusterActionsSx(theme)}>
                        {onConfigureCluster && (
                            <IconButton
                                size="small"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    onConfigureCluster(cluster);
                                }}
                                sx={settingsButtonSx}
                            >
                                <SettingsIcon sx={settingsIconSx} />
                            </IconButton>
                        )}
                        {onDeleteCluster && totalCount === 0 && (
                            <IconButton
                                size="small"
                                aria-label={`Delete cluster ${cluster.name}`}
                                onClick={(e) => {
                                    e.stopPropagation();
                                    onDeleteCluster(cluster);
                                }}
                                sx={deleteButtonSx}
                            >
                                <DeleteIcon sx={deleteIconSx} />
                            </IconButton>
                        )}
                    </Box>
                )}
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
                            isLast={index === (cluster.servers?.length ?? 0) - 1}
                            showTreeLines={true}
                            clusterType={clusterType}
                            user={user}
                            onUpdateServer={onUpdateServer}
                            onEditServer={onEditServer}
                            onDeleteServer={onDeleteServer}
                            alertCount={getServerAlertCount ? getServerAlertCount(server.id) : 0}
                            getServerAlertCount={getServerAlertCount}
                            getServerBlackoutStatus={getServerBlackoutStatus}
                            getClusterBlackoutStatus={getClusterBlackoutStatus}
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
