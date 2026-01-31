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
    Typography,
    IconButton,
    Collapse,
    alpha,
} from '@mui/material';
import {
    ExpandMore as ExpandIcon,
    ChevronRight as CollapseIcon,
    FolderOpen as GroupOpenIcon,
    Folder as GroupIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';
import InlineEditText from '../InlineEditText';
import { getClusterType, countServersRecursive } from './utils';
import ClusterContainer from './ClusterContainer';
import ClusterItem from './ClusterItem';
import ServerItem from './ServerItem';
import { DraggableCluster, DroppableGroup } from './DragDropComponents';

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
    getServerAlertCount,
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
        (acc, c) => acc + countServersRecursive(c.servers, s => s.status !== 'offline'),
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
                                            alertCount={getServerAlertCount ? getServerAlertCount(server.id) : 0}
                                            getServerAlertCount={getServerAlertCount}
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
                                    isLast={clusterIndex === group.clusters.length - 1}
                                    user={user}
                                    onUpdateCluster={onUpdateCluster}
                                    onUpdateServer={onUpdateServer}
                                    onEditServer={onEditServer}
                                    onDeleteServer={onDeleteServer}
                                    getServerAlertCount={getServerAlertCount}
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
