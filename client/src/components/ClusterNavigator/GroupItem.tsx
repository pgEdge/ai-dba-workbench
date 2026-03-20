/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { memo } from 'react';
import {
    Box,
    Typography,
    IconButton,
    Collapse,
    alpha,
    useTheme,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import type { Cluster, Server } from './utils';
import {
    ExpandMore as ExpandIcon,
    ChevronRight as CollapseIcon,
    FolderOpen as GroupOpenIcon,
    Folder as GroupIcon,
    Delete as DeleteIcon,
    Settings as SettingsIcon,
} from '@mui/icons-material';
import InlineEditText from '../InlineEditText';
import { getClusterType, countServersRecursive } from './utils';
import ClusterContainer from './ClusterContainer';
import ClusterItem from './ClusterItem';
import ServerItem from './ServerItem';
import { DraggableCluster, DroppableGroup } from './DragDropComponents';

// -- Static sx constants --------------------------------------------------

const groupContainerSx = { mb: 0.5 };
const clusterListSx = { pt: 0.5 };
const expandButtonSx = { p: 0.25, color: 'text.secondary', ml: 0.5 };
const expandIcon18Sx = { fontSize: 18 };
const deleteIconSx = { fontSize: 14 };
const deleteButtonSx = { p: 0.25, color: 'text.disabled', '&:hover': { color: 'error.main' } };
const settingsIconSx = { fontSize: 14 };
const settingsButtonSx = { p: 0.25, color: 'text.disabled', '&:hover': { color: 'primary.main' } };
const flexMinWidthSx = { flex: 1, minWidth: 0 };

const groupNameSx = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '0.875rem',
    textTransform: 'uppercase',
    letterSpacing: '0.08em',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const countTextSx = {
    color: 'text.disabled',
    fontSize: '0.875rem',
    ml: 'auto',
    flexShrink: 0,
};

const folderIconExpandedSx = { fontSize: 18, color: 'primary.main' };
const folderIconCollapsedSx = { fontSize: 18, color: 'text.secondary' };

// -- Style-getter functions -----------------------------------------------

const getGroupRowSx = (theme: Theme) => ({
    position: 'relative',
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    py: 1,
    px: 1.5,
    cursor: 'pointer',
    borderRadius: 1,
    mx: 1,
    bgcolor: alpha(
        theme.palette.mode === 'dark' ? theme.palette.grey[700] : theme.palette.grey[100],
        theme.palette.mode === 'dark' ? 0.4 : 0.8
    ),
    transition: 'all 0.15s ease',
    '&:hover': {
        bgcolor: alpha(
            theme.palette.mode === 'dark' ? theme.palette.grey[700] : theme.palette.grey[200],
            theme.palette.mode === 'dark' ? 0.6 : 0.8
        ),
    },
});

const getActionButtonsSx = (theme: Theme) => ({
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
    '.group-item-row:hover &': { opacity: 1 },
});

/**
 * GroupItem - Cluster group that can be expanded to show clusters
 * Memoized to prevent unnecessary re-renders during data refresh
 */
interface UserInfo {
    isSuperuser?: boolean;
    username?: string;
}

interface GroupData {
    id: string;
    name: string;
    auto_group_key?: string;
    is_default?: boolean;
    clusters?: Array<Cluster & { cluster_type?: string }>;
}

interface GroupItemProps {
    group: GroupData;
    isExpanded: boolean;
    onToggle: () => void;
    expandedClusters: Set<string>;
    onToggleCluster: (clusterId: string) => void;
    selectedServerId?: number;
    selectedClusterId?: string;
    onSelectServer: (server: Server) => void;
    onSelectCluster?: (cluster: Cluster) => void;
    isDark: boolean;
    expandedServers?: Set<number>;
    onToggleServer?: (serverId: number) => void;
    user?: UserInfo;
    onUpdateGroup: (groupId: string, newName: string) => Promise<void>;
    onUpdateCluster: (clusterId: string, newName: string, groupId: string, autoClusterKey?: string) => Promise<void>;
    onUpdateServer: (serverId: number, newName: string) => Promise<void>;
    onEditServer?: (server: Server) => void;
    onDeleteServer?: (server: Server) => void;
    onDeleteGroup?: (group: GroupData) => void;
    onConfigureGroup?: (group: GroupData) => void;
    onConfigureCluster?: (cluster: Cluster) => void;
    onDeleteCluster?: (cluster: Cluster) => void;
    getServerAlertCount?: (serverId: number) => number;
    getServerBlackoutStatus?: (serverId: number) => { active: boolean; inherited: boolean };
    getClusterBlackoutStatus?: (clusterId: string) => { active: boolean; inherited: boolean };
}

const GroupItem = memo<GroupItemProps>(({
    group,
    isExpanded,
    onToggle,
    expandedClusters,
    onToggleCluster,
    selectedServerId,
    selectedClusterId,
    onSelectServer,
    onSelectCluster,
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
    onConfigureGroup,
    onConfigureCluster,
    onDeleteCluster,
    getServerAlertCount,
    getServerBlackoutStatus,
    getClusterBlackoutStatus,
}) => {
    const theme = useTheme();

    // Superusers can edit both database-backed groups (ID: group-{number})
    // and auto-detected groups (groups with auto_group_key)
    const isEditableGroup = /^group-\d+$/.test(group.id) || !!group.auto_group_key;
    const canEditGroup = user?.isSuperuser && isEditableGroup;
    const totalServers = group.clusters?.reduce(
        (acc, c) => acc + countServersRecursive(c.servers),
        0
    ) || 0;
    const onlineServers = group.clusters?.reduce(
        (acc, c) => acc + countServersRecursive(c.servers, s => s.status !== 'offline'),
        0
    ) || 0;

    // Groups can be deleted if they're not auto-detected and not the default group
    const canDeleteGroup = canEditGroup && !group.auto_group_key && !group.is_default;

    return (
        <DroppableGroup groupId={group.id} isDark={isDark}>
            <Box sx={groupContainerSx}>
                <Box
                className="group-item-row"
                onClick={onToggle}
                sx={getGroupRowSx(theme)}
            >
                {isExpanded ? (
                    <GroupOpenIcon sx={folderIconExpandedSx} />
                ) : (
                    <GroupIcon sx={folderIconCollapsedSx} />
                )}
                <Box sx={flexMinWidthSx}>
                    <InlineEditText
                        value={group.name}
                        onSave={(newName) => onUpdateGroup(group.id, newName)}
                        canEdit={canEditGroup}
                        typographyProps={{
                            variant: 'body2',
                            sx: groupNameSx,
                        }}
                    />
                </Box>
                <Typography
                    variant="caption"
                    sx={countTextSx}
                >
                    {onlineServers}/{totalServers}
                </Typography>
                <IconButton
                    size="small"
                    sx={expandButtonSx}
                >
                    {isExpanded ? (
                        <ExpandIcon sx={expandIcon18Sx} />
                    ) : (
                        <CollapseIcon sx={expandIcon18Sx} />
                    )}
                </IconButton>
                {canEditGroup && (
                    <Box
                        className="action-buttons"
                        sx={getActionButtonsSx(theme)}
                    >
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                e.stopPropagation();
                                onConfigureGroup?.(group);
                            }}
                            sx={settingsButtonSx}
                        >
                            <SettingsIcon sx={settingsIconSx} />
                        </IconButton>
                        {canDeleteGroup && (
                            <IconButton
                                size="small"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    onDeleteGroup?.(group);
                                }}
                                sx={deleteButtonSx}
                            >
                                <DeleteIcon sx={deleteIconSx} />
                            </IconButton>
                        )}
                    </Box>
                )}
            </Box>
            <Collapse in={isExpanded} timeout="auto">
                <Box sx={clusterListSx}>
                    {group.clusters?.map((cluster, clusterIndex) => {
                        // For cluster_type "server", handle differently based on server count
                        if (cluster.cluster_type === 'server') {
                            const serverCount = cluster.servers?.length || 0;
                            const hasMultipleServers = serverCount > 1;
                            const hasChildServers = cluster.servers?.some(s => (s.children?.length ?? 0) > 0);
                            const clusterType = getClusterType(cluster);

                            // Empty cluster - render as a simple cluster item
                            if (serverCount === 0) {
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
                                            selectedClusterId={selectedClusterId}
                                            onSelectServer={onSelectServer}
                                            onSelectCluster={onSelectCluster}
                                            depth={1}
                                            isDark={isDark}
                                            expandedServers={expandedServers}
                                            onToggleServer={onToggleServer}
                                            isLast={clusterIndex === (group.clusters?.length ?? 0) - 1}
                                            user={user}
                                            onUpdateCluster={onUpdateCluster}
                                            onUpdateServer={onUpdateServer}
                                            onEditServer={onEditServer}
                                            onDeleteServer={onDeleteServer}
                                            onConfigureCluster={onConfigureCluster}
                                            onDeleteCluster={onDeleteCluster}
                                            getServerAlertCount={getServerAlertCount}
                                            getServerBlackoutStatus={getServerBlackoutStatus}
                                            getClusterBlackoutStatus={getClusterBlackoutStatus}
                                        />
                                    </DraggableCluster>
                                );
                            }

                            // Single standalone server without children - no container
                            if (serverCount === 1 && !hasChildServers) {
                                const server = cluster.servers![0];
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
                                            alertCount={getServerAlertCount ? getServerAlertCount(server.id) : 0}
                                            getServerAlertCount={getServerAlertCount}
                                            getServerBlackoutStatus={getServerBlackoutStatus}
                                            getClusterBlackoutStatus={getClusterBlackoutStatus}
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
                                                isLast={serverIndex === (cluster.servers?.length ?? 0) - 1}
                                                showTreeLines={hasMultipleServers || hasChildServers}
                                                clusterType={clusterType}
                                                user={user}
                                                onUpdateServer={onUpdateServer}
                                                onEditServer={onEditServer}
                                                onDeleteServer={onDeleteServer}
                                                alertCount={getServerAlertCount ? getServerAlertCount(server.id) : 0}
                                                getServerAlertCount={getServerAlertCount}
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
                                    selectedClusterId={selectedClusterId}
                                    onSelectServer={onSelectServer}
                                    onSelectCluster={onSelectCluster}
                                    depth={1}
                                    isDark={isDark}
                                    expandedServers={expandedServers}
                                    onToggleServer={onToggleServer}
                                    isLast={clusterIndex === (group.clusters?.length ?? 0) - 1}
                                    user={user}
                                    onUpdateCluster={onUpdateCluster}
                                    onUpdateServer={onUpdateServer}
                                    onEditServer={onEditServer}
                                    onDeleteServer={onDeleteServer}
                                    onConfigureCluster={onConfigureCluster}
                                    onDeleteCluster={onDeleteCluster}
                                    getServerAlertCount={getServerAlertCount}
                                    getServerBlackoutStatus={getServerBlackoutStatus}
                                    getClusterBlackoutStatus={getClusterBlackoutStatus}
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

export default GroupItem;
