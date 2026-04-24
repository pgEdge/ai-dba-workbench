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
import { useTheme } from '@mui/material/styles';
import { blendColors } from '../../../utils/colors';
import { useClusterSelection } from '../../../contexts/ClusterSelectionContext';
import { buildGraph } from './topology/graphBuilder';
import { computeLayout, computeContainerHeight, NODE_WIDTH, NODE_HEIGHT } from './topology/layoutEngine';
import TopologyEdges from './topology/TopologyEdges';
import TopologyNode from './topology/TopologyNode';
import { TopoNode } from './topology/types';
import type { ClusterSelection } from '../../../types/selection';

interface TopologySectionProps {
    selection: ClusterSelection;
}

const EMPTY_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 4,
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
        () => selection.servers || [],
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
