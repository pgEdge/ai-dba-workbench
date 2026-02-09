/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { useDraggable, useDroppable } from '@dnd-kit/core';
import { Box, Typography, Chip, alpha, useTheme } from '@mui/material';
import { Theme } from '@mui/material/styles';
import type { Cluster } from './utils';
import {
    DragIndicator as DragIcon,
    Dns as ClusterIcon,
} from '@mui/icons-material';
import { countServersRecursive } from './utils';

// -- Static sx constants --------------------------------------------------

const draggableContainerBase = {
    position: 'relative',
};

const dragHandleBase = {
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
    '.draggable-cluster:hover &': { opacity: 1 },
    '&:active': { cursor: 'grabbing' },
};

const droppableBase = {
    position: 'relative',
    transition: 'all 0.2s ease',
};

const dropOverlayBase = {
    content: '""',
    position: 'absolute',
    top: 0,
    left: 8,
    right: 8,
    bottom: 0,
    border: '2px dashed',
    borderRadius: 2,
    pointerEvents: 'none',
    zIndex: 1,
};

const overlayContainerBase = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    py: 0.75,
    px: 1.5,
    border: '1px solid',
    borderRadius: 2,
    cursor: 'grabbing',
};

const overlayClusterNameSx = {
    fontWeight: 500,
    fontSize: '0.8125rem',
    color: 'text.primary',
};

const overlayChipBase = {
    height: 18,
    fontSize: '0.625rem',
    fontWeight: 600,
    '& .MuiChip-label': { px: 0.75 },
};

const dragIconSx = { fontSize: 14, color: 'text.disabled' };
const overlayDragIconSx = { fontSize: 16, color: 'text.disabled' };
const overlayClusterIconSx = { fontSize: 16, color: 'text.secondary' };

// -- Style-getter functions -----------------------------------------------

const getDragHandleSx = (theme: Theme) => ({
    ...dragHandleBase,
    bgcolor: alpha(theme.palette.background.paper, 0.95),
    boxShadow: theme.palette.mode === 'dark'
        ? '0 2px 8px rgba(0, 0, 0, 0.3)'
        : '0 2px 8px rgba(0, 0, 0, 0.1)',
    '&:hover': {
        bgcolor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.15 : 0.1),
    },
});

const getDropOverlaySx = (theme: Theme) => ({
    '&::before': {
        ...dropOverlayBase,
        borderColor: theme.palette.primary.main,
        bgcolor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.08 : 0.05),
    },
});

const getOverlayContainerSx = (theme: Theme) => ({
    ...overlayContainerBase,
    bgcolor: theme.palette.background.paper,
    borderColor: theme.palette.primary.main,
    boxShadow: theme.palette.mode === 'dark'
        ? '0 8px 24px rgba(0, 0, 0, 0.4)'
        : '0 8px 24px rgba(0, 0, 0, 0.15)',
});

const getOverlayChipSx = (theme: Theme) => ({
    ...overlayChipBase,
    bgcolor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.15 : 0.1),
    color: theme.palette.primary.main,
});

/**
 * DraggableCluster - Wrapper that makes a cluster draggable via drag handle
 * Uses a drag handle approach to avoid blocking click events on child components
 */
interface DraggableClusterProps {
    cluster: Cluster;
    groupId: string;
    children: React.ReactNode;
    isDark: boolean;
    canDrag?: boolean;
}

export const DraggableCluster: React.FC<DraggableClusterProps> = ({
    cluster,
    groupId,
    children,
    isDark: _isDark,
    canDrag = true,
}) => {
    const theme = useTheme();
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
                ...draggableContainerBase,
                opacity: isDragging ? 0.5 : 1,
            }}
        >
            {/* Drag handle - only appears on hover when dragging is allowed */}
            {canDrag && (
                <Box
                    {...attributes}
                    {...listeners}
                    sx={getDragHandleSx(theme)}
                >
                    <DragIcon sx={dragIconSx} />
                </Box>
            )}
            {children}
        </Box>
    );
};

/**
 * DroppableGroup - Wrapper that makes a group a drop target
 */
interface DroppableGroupProps {
    groupId: string;
    children: React.ReactNode;
    isDark: boolean;
}

export const DroppableGroup: React.FC<DroppableGroupProps> = ({
    groupId,
    children,
    isDark: _isDark,
}) => {
    const theme = useTheme();
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
                ...droppableBase,
                ...(isOver && getDropOverlaySx(theme)),
            }}
        >
            {children}
        </Box>
    );
};

/**
 * DragOverlayContent - Content shown during drag
 */
interface DragOverlayContentProps {
    cluster: Cluster | null;
    isDark: boolean;
}

export const DragOverlayContent: React.FC<DragOverlayContentProps> = ({ cluster, isDark: _isDark }) => {
    const theme = useTheme();
    if (!cluster) {return null;}

    const totalCount = countServersRecursive(cluster.servers);
    const onlineCount = countServersRecursive(cluster.servers, s => s.status === 'online');

    return (
        <Box sx={getOverlayContainerSx(theme)}>
            <DragIcon sx={overlayDragIconSx} />
            <ClusterIcon sx={overlayClusterIconSx} />
            <Typography
                variant="body2"
                sx={overlayClusterNameSx}
            >
                {cluster.name}
            </Typography>
            <Chip
                label={`${onlineCount}/${totalCount}`}
                size="small"
                sx={getOverlayChipSx(theme)}
            />
        </Box>
    );
};
