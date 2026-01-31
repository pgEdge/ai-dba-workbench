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

import React, { useState, useMemo, useCallback, useRef, useEffect } from 'react';
import { useAuth } from '../../contexts/AuthContext';
import { useCluster } from '../../contexts/ClusterContext';
import { useAlerts } from '../../contexts/AlertsContext';
import {
    DndContext,
    DragOverlay,
    pointerWithin,
} from '@dnd-kit/core';
import {
    Box,
    Typography,
    IconButton,
    Tooltip,
    TextField,
    InputAdornment,
    Skeleton,
    alpha,
    useTheme,
} from '@mui/material';
import {
    Search as SearchIcon,
    Storage as ServerIcon,
    Refresh as RefreshIcon,
    Add as AddIcon,
    Autorenew as AutorenewIcon,
} from '@mui/icons-material';
import ServerDialog from '../ServerDialog';
import GroupDialog from '../GroupDialog';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import AddMenu from '../AddMenu';

// Import sub-components
import { STORAGE_KEYS } from './constants';
import {
    collectExpandableServerIds,
    filterServersRecursive,
    countServersRecursive,
    loadFromStorage,
    saveToStorage,
    formatRelativeTime,
} from './utils';
import StatusIndicator from './StatusIndicator';
import GroupItem from './GroupItem';
import { DragOverlayContent } from './DragDropComponents';

// -- Static sx constants --------------------------------------------------

const headerRowSx = {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    mb: 1.5,
};

const headerTitleSx = {
    color: 'text.primary',
    fontSize: '0.6875rem',
    fontWeight: 600,
    letterSpacing: '0.08em',
};

const headerButtonGroupSx = { display: 'flex', gap: 0.5 };
const icon18Sx = { fontSize: 18 };
const searchIconSx = { fontSize: 18, color: 'text.disabled' };

const scrollContainerSx = { flex: 1, overflow: 'auto', py: 1 };
const loadingContainerSx = { px: 2 };
const loadingGroupSx = { mb: 2 };

const emptyStateSx = {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    py: 4,
    px: 2,
    textAlign: 'center',
};

const emptyIconSx = { fontSize: 48, color: 'text.disabled', mb: 1.5 };
const emptyTitleSx = { color: 'text.secondary', mb: 0.5 };
const emptySubtitleSx = { color: 'text.disabled' };

const footerCountSx = { color: 'text.disabled', fontSize: '0.625rem' };

const addButtonBaseSx = {
    p: 0.5,
    '&:hover': { color: 'primary.main' },
};

const refreshButtonSx = {
    p: 0.5,
    color: 'text.secondary',
    '&:hover': { color: 'primary.main' },
};

const searchInputSx = {
    py: 0.875,
    '&::placeholder': {
        color: 'text.disabled',
        opacity: 1,
    },
};

// -- Style-getter functions -----------------------------------------------

const getPanelSx = (theme, panelWidth) => ({
    width: panelWidth,
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
    bgcolor: 'background.paper',
    borderRight: '1px solid',
    borderColor: 'divider',
    position: 'relative',
    flexShrink: 0,
});

const getResizeHandleSx = (theme, isResizing) => ({
    position: 'absolute',
    top: 0,
    right: -3,
    bottom: 0,
    width: 6,
    cursor: 'col-resize',
    zIndex: 10,
    '&:hover': {
        bgcolor: alpha(theme.palette.info.main, 0.3),
    },
    ...(isResizing && {
        bgcolor: alpha(theme.palette.info.main, 0.5),
    }),
});

const getHeaderSx = (theme) => ({
    px: 2,
    py: 1.5,
    borderBottom: '1px solid',
    borderColor: 'divider',
});

const getAddButtonSx = (theme) => ({
    ...addButtonBaseSx,
    color: theme.palette.mode === 'dark' ? alpha(theme.palette.common.white, 0.7) : 'text.secondary',
});

const getAutoRefreshSx = (theme, enabled) => ({
    p: 0.5,
    color: enabled
        ? 'primary.main'
        : (theme.palette.mode === 'dark' ? alpha(theme.palette.common.white, 0.4) : 'text.disabled'),
});

const getEstateSx = (theme, isSelected) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    mb: 1.5,
    py: 0.5,
    px: 1,
    mx: -1,
    borderRadius: 1,
    cursor: 'pointer',
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

const getSearchFieldSx = (theme) => ({
    '& .MuiOutlinedInput-root': {
        bgcolor: alpha(theme.palette.background.default, theme.palette.mode === 'dark' ? 0.5 : 0.8),
        fontSize: '0.8125rem',
        '& fieldset': {
            borderColor: 'divider',
        },
        '&:hover fieldset': {
            borderColor: theme.palette.mode === 'dark' ? theme.palette.grey[600] : theme.palette.grey[300],
        },
    },
    '& .MuiOutlinedInput-input': searchInputSx,
});

const getSkeletonSx = (theme) => ({
    mb: 1,
    bgcolor: theme.palette.mode === 'dark' ? theme.palette.grey[700] : theme.palette.grey[200],
});

const getSkeletonChildSx = (theme) => ({
    ml: 2,
    mb: 0.5,
    bgcolor: theme.palette.mode === 'dark' ? theme.palette.grey[700] : theme.palette.grey[200],
});

const getFooterSx = (theme) => ({
    px: 2,
    py: 1,
    borderTop: '1px solid',
    borderColor: 'divider',
    bgcolor: alpha(theme.palette.background.default, 0.5),
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
});

const getLastRefreshSx = (theme) => ({
    color: theme.palette.mode === 'dark' ? alpha(theme.palette.common.white, 0.5) : 'text.disabled',
    fontSize: '0.625rem',
});

const getSpinnerSx = (loading) => ({
    fontSize: 18,
    animation: loading ? 'spin 1s linear infinite' : 'none',
    '@keyframes spin': {
        '0%': { transform: 'rotate(0deg)' },
        '100%': { transform: 'rotate(360deg)' },
    },
});

/**
 * ClusterNavigator - Main navigation panel component
 */
const ClusterNavigator = ({
    data = [],
    selectedServerId,
    selectedClusterId,
    selectionType,
    onSelectServer,
    onSelectCluster,
    onSelectEstate,
    onRefresh,
    loading = false,
    mode = 'light',
    defaultWidth = 280,
    minWidth = 200,
    maxWidth = 500,
}) => {
    const theme = useTheme();
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

    // Get alert counts from context
    const { getServerAlertCount, getTotalAlertCount } = useAlerts();

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
        getServer,
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

    // Handler for adding a server
    const handleAddServer = () => {
        setAddMenuAnchor(null);
        setEditingServer(null);
        setServerDialogMode('create');
        setServerDialogOpen(true);
    };

    // Handler for editing a server
    const handleEditServer = async (server) => {
        try {
            // Fetch full server details including username and database_name
            const fullServerDetails = await getServer(server.id);
            setEditingServer(fullServerDetails);
            setServerDialogMode('edit');
            setServerDialogOpen(true);
        } catch (err) {
            console.error('Failed to get server details:', err);
            // Fall back to using the limited data we have
            setEditingServer(server);
            setServerDialogMode('edit');
            setServerDialogOpen(true);
        }
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

    // Initialize groups and servers on first data load
    // Groups are always expanded by default to show clusters
    // Clusters default to collapsed unless restored from localStorage
    // Expandable servers (with children) are expanded by default
    useEffect(() => {
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
            (a, c) => a + countServersRecursive(c.servers, s => s.status !== 'offline'), 0
        ) || 0),
        0
    );
    const offlineServers = data.reduce(
        (acc, g) => acc + (g.clusters?.reduce(
            (a, c) => a + countServersRecursive(c.servers, s => s.status === 'offline'), 0
        ) || 0),
        0
    );
    const warningServers = totalServers - onlineServers - offlineServers;
    const estateStatus = offlineServers > 0 ? 'offline' : (warningServers > 0 ? 'warning' : 'online');

    const isEstateSelected = selectionType === 'estate';

    return (
        <DndContext
            collisionDetection={pointerWithin}
            onDragStart={handleDragStart}
            onDragEnd={handleDragEnd}
        >
            <Box sx={getPanelSx(theme, panelWidth)}>
                {/* Resize handle */}
            <Box
                ref={resizeRef}
                onMouseDown={handleResizeStart}
                sx={getResizeHandleSx(theme, isResizing)}
            />
            {/* Header */}
            <Box sx={getHeaderSx(theme)}>
                <Box sx={headerRowSx}>
                    <Typography
                        variant="overline"
                        sx={headerTitleSx}
                    >
                        Database Servers
                    </Typography>
                    <Box sx={headerButtonGroupSx}>
                        <Tooltip title="Add server or group">
                            <IconButton
                                size="small"
                                onClick={(e) => setAddMenuAnchor(e.currentTarget)}
                                sx={getAddButtonSx(theme)}
                            >
                                <AddIcon sx={icon18Sx} />
                            </IconButton>
                        </Tooltip>
                        <Tooltip title={autoRefreshEnabled ? 'Auto-refresh enabled' : 'Auto-refresh disabled'}>
                            <IconButton
                                size="small"
                                onClick={() => setAutoRefreshEnabled(!autoRefreshEnabled)}
                                sx={getAutoRefreshSx(theme, autoRefreshEnabled)}
                            >
                                <AutorenewIcon sx={icon18Sx} />
                            </IconButton>
                        </Tooltip>
                        <Tooltip title="Refresh">
                            <IconButton
                                size="small"
                                onClick={onRefresh}
                                disabled={loading}
                                sx={refreshButtonSx}
                            >
                                <RefreshIcon sx={getSpinnerSx(loading)} />
                            </IconButton>
                        </Tooltip>
                    </Box>
                </Box>

                {/* Status summary - clickable for estate selection */}
                <Tooltip title="View estate overview" placement="right">
                    <Box
                        onClick={() => onSelectEstate?.()}
                        sx={getEstateSx(theme, isEstateSelected)}
                    >
                        <StatusIndicator status={estateStatus} alertCount={getTotalAlertCount()} />
                        <Typography
                            variant="caption"
                            sx={{
                                color: isEstateSelected ? 'text.primary' : 'text.secondary',
                                fontSize: '0.6875rem',
                                fontWeight: isEstateSelected ? 600 : 400,
                                flex: 1,
                            }}
                        >
                            {onlineServers} online of {totalServers} servers
                        </Typography>
                    </Box>
                </Tooltip>

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
                                <SearchIcon sx={searchIconSx} />
                            </InputAdornment>
                        ),
                    }}
                    sx={getSearchFieldSx(theme)}
                />
            </Box>

            {/* Navigation Tree */}
            <Box
                ref={scrollContainerRef}
                onScroll={handleScroll}
                sx={scrollContainerSx}
            >
                {loading ? (
                    // Loading skeletons
                    <Box sx={loadingContainerSx}>
                        {[1, 2, 3].map((i) => (
                            <Box key={i} sx={loadingGroupSx}>
                                <Skeleton
                                    variant="rounded"
                                    height={36}
                                    sx={getSkeletonSx(theme)}
                                />
                                {[1, 2].map((j) => (
                                    <Skeleton
                                        key={j}
                                        variant="rounded"
                                        height={28}
                                        sx={getSkeletonChildSx(theme)}
                                    />
                                ))}
                            </Box>
                        ))}
                    </Box>
                ) : filteredData.length === 0 ? (
                    // Empty state
                    <Box sx={emptyStateSx}>
                        <ServerIcon sx={emptyIconSx} />
                        <Typography
                            variant="body2"
                            sx={emptyTitleSx}
                        >
                            {searchQuery ? 'No servers found' : 'No servers configured'}
                        </Typography>
                        <Typography
                            variant="caption"
                            sx={emptySubtitleSx}
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
                            selectedClusterId={selectedClusterId}
                            onSelectServer={onSelectServer}
                            onSelectCluster={onSelectCluster}
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
                            getServerAlertCount={getServerAlertCount}
                        />
                    ))
                )}
            </Box>

            {/* Footer */}
            <Box sx={getFooterSx(theme)}>
                <Typography
                    variant="caption"
                    sx={footerCountSx}
                >
                    {filteredData.length} groups • {
                        filteredData.reduce((a, g) => a + (g.clusters?.length || 0), 0)
                    } clusters
                </Typography>
                {lastRefresh && (
                    <Typography
                        variant="caption"
                        sx={getLastRefreshSx(theme)}
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
