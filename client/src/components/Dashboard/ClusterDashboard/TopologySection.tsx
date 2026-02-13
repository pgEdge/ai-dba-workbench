/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo, useCallback, useRef, useState, useEffect } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { alpha, useTheme } from '@mui/material/styles';
import { useClusterSelection } from '../../../contexts/ClusterSelectionContext';
import { ClusterServer } from '../../../contexts/ClusterDataContext';
import { buildGraph } from './topology/graphBuilder';
import { computeLayout, computeContainerHeight, NODE_WIDTH, NODE_HEIGHT } from './topology/layoutEngine';
import TopologyEdges from './topology/TopologyEdges';
import TopologyNode from './topology/TopologyNode';
import { TopoNode } from './topology/types';

interface TopologySectionProps {
    selection: Record<string, unknown>;
}

const EMPTY_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 4,
};

/**
 * Blend two hex colors. Equivalent to layering `fg` at the
 * given opacity over `bg`.
 */
const blendColors = (bg: string, fg: string, opacity: number): string => {
    const parse = (hex: string): [number, number, number] => {
        const h = hex.replace('#', '');
        return [
            parseInt(h.substring(0, 2), 16),
            parseInt(h.substring(2, 4), 16),
            parseInt(h.substring(4, 6), 16),
        ];
    };
    const [br, bg2, bb] = parse(bg);
    const [fr, fg2, fb] = parse(fg);
    const r = Math.round(fr * opacity + br * (1 - opacity));
    const g = Math.round(fg2 * opacity + bg2 * (1 - opacity));
    const b = Math.round(fb * opacity + bb * (1 - opacity));
    return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`;
};

/**
 * TopologySection displays the cluster topology as a connected
 * diagram with nodes and edges representing servers and their
 * replication relationships. The layout adapts to the cluster
 * type (binary tree, spock mesh, logical flow, or standalone).
 */
const TopologySection: React.FC<TopologySectionProps> = ({ selection }) => {
    const { selectServer } = useClusterSelection();
    const theme = useTheme();
    const containerRef = useRef<HTMLDivElement>(null);
    const [containerWidth, setContainerWidth] = useState(600);

    const labelBackground = theme.palette.mode === 'dark'
        ? blendColors(theme.palette.background.default, theme.palette.background.paper, 0.4)
        : theme.palette.grey[100];

    // Observe container width changes for responsive layout
    useEffect(() => {
        const el = containerRef.current;
        if (!el) {return;}

        const observer = new ResizeObserver((entries) => {
            for (const entry of entries) {
                const width = entry.contentRect.width;
                if (width > 0) {
                    setContainerWidth(width);
                }
            }
        });

        observer.observe(el);
        return () => observer.disconnect();
    }, []);

    // Extract servers from the selection object
    const servers = useMemo(
        () => (selection.servers as ClusterServer[] | undefined) || [],
        [selection.servers],
    );

    // Build the topology graph from server hierarchy
    const graph = useMemo(() => buildGraph(servers), [servers]);

    // Compute positioned layout based on container width
    const layout = useMemo(
        () => computeLayout(graph, containerWidth),
        [graph, containerWidth],
    );

    const containerHeight = useMemo(
        () => computeContainerHeight(layout),
        [layout],
    );

    const handleNodeClick = useCallback(
        (node: TopoNode) => {
            selectServer(node.server);
        },
        [selectServer],
    );

    if (servers.length === 0) {
        return (
            <Typography sx={EMPTY_SX}>
                No topology data available.
            </Typography>
        );
    }

    return (
        <Box
            ref={containerRef}
            sx={{
                position: 'relative',
                minHeight: containerHeight,
                width: '100%',
            }}
        >
            <TopologyEdges
                edges={layout.edges}
                nodes={layout.nodes}
                nodeWidth={NODE_WIDTH}
                nodeHeight={NODE_HEIGHT}
                topologyType={layout.topologyType}
                labelBackground={labelBackground}
            />
            {layout.nodes.map(node => (
                <TopologyNode
                    key={node.id}
                    node={node}
                    nodeWidth={NODE_WIDTH}
                    onClick={handleNodeClick}
                />
            ))}
        </Box>
    );
};

export default TopologySection;
