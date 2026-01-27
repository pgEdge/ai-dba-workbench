/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { useDraggable, useDroppable } from '@dnd-kit/core';
import { Box, Typography, Chip, alpha } from '@mui/material';
import {
    DragIndicator as DragIcon,
    Dns as ClusterIcon,
} from '@mui/icons-material';
import { countServersRecursive } from './utils';

/**
 * DraggableCluster - Wrapper that makes a cluster draggable via drag handle
 * Uses a drag handle approach to avoid blocking click events on child components
 */
export const DraggableCluster = ({
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
export const DroppableGroup = ({
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
export const DragOverlayContent = ({ cluster, isDark }) => {
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
