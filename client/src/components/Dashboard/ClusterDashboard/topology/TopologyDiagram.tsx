/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo, useRef, useState, useEffect } from 'react';
import Box from '@mui/material/Box';
import { useTheme } from '@mui/material/styles';
import { blendColors } from '../../../../utils/colors';
import { ClusterServer } from '../../../../contexts/ClusterDataContext';
import { buildGraph } from './graphBuilder';
import {
    computeLayout,
    computeContainerHeight,
    NODE_WIDTH,
    NODE_HEIGHT,
} from './layoutEngine';
import TopologyEdges from './TopologyEdges';
import TopologyNode from './TopologyNode';
import { TopoNode } from './types';

interface TopologyDiagramProps {
    /** Servers to display in the topology diagram. */
    servers: ClusterServer[];
    /** Optional click handler for node selection. */
    onNodeClick?: (node: TopoNode) => void;
    /** ID of the server to highlight with a distinct border. */
    highlightServerId?: number;
    /** Minimum height override for the container. */
    minHeight?: number;
    /** Maximum width for castellated layout overflow handling. */
    maxWidth?: number;
}

/**
 * TopologyDiagram renders a cluster topology as a connected
 * diagram with nodes and edges. This component is a reusable
 * wrapper around the lower-level topology primitives, free of
 * dashboard-specific context dependencies.
 *
 * When `highlightServerId` is provided, the matching node
 * receives a primary-color border to distinguish the current
 * server from its peers.
 */
const TopologyDiagram: React.FC<TopologyDiagramProps> = ({
    servers,
    onNodeClick,
    highlightServerId,
    minHeight,
    maxWidth,
}) => {
    const theme = useTheme();
    const containerRef = useRef<HTMLDivElement>(null);
    const [containerWidth, setContainerWidth] = useState(500);

    const labelBackground =
        theme.palette.mode === 'dark'
            ? blendColors(
                theme.palette.background.default,
                theme.palette.background.paper,
                0.4,
            )
            : theme.palette.grey[100];

    // Observe container width changes for responsive layout
    useEffect(() => {
        const el = containerRef.current;
        if (!el) {
            return;
        }

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

    const graph = useMemo(() => buildGraph(servers), [servers]);

    const layout = useMemo(
        () => computeLayout(graph, containerWidth, maxWidth),
        [graph, containerWidth, maxWidth],
    );

    const containerHeight = useMemo(
        () => computeContainerHeight(layout),
        [layout],
    );

    // No-op click handler when the diagram is read-only
    const noopClick = useMemo(() => () => {}, []);

    if (servers.length === 0) {
        return null;
    }

    return (
        <Box
            ref={containerRef}
            data-testid="topology-diagram"
            sx={{
                position: 'relative',
                minHeight: minHeight ?? containerHeight,
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
            {layout.nodes.map((node) => (
                <TopologyNode
                    key={node.id}
                    node={node}
                    nodeWidth={NODE_WIDTH}
                    onClick={onNodeClick ?? noopClick}
                    highlight={node.id === highlightServerId}
                />
            ))}
        </Box>
    );
};

export default TopologyDiagram;
