/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import { useTheme } from '@mui/material/styles';
import { TopoEdge, TopoNode, ClusterTopologyType } from './types';

interface TopologyEdgesProps {
    edges: TopoEdge[];
    nodes: TopoNode[];
    nodeWidth: number;
    nodeHeight: number;
    topologyType: ClusterTopologyType;
    labelBackground: string;
}

/**
 * Return a theme-aware color for each replication type.
 */
const useEdgeColors = (): Record<string, string> => {
    const theme = useTheme();
    return useMemo(
        () => ({
            streaming: theme.palette.primary.main,
            spock: theme.palette.warning.main,
            logical: theme.palette.success.main,
        }),
        [theme],
    );
};

/**
 * Generate a unique marker id to avoid SVG id collisions.
 */
const markerId = (type: string): string =>
    `arrow-${type}`;

/**
 * Replication type display labels.
 */
const EDGE_LABELS: Record<string, string> = {
    streaming: 'Physical',
    spock: 'Spock',
    logical: 'Logical',
};

/**
 * TopologyEdges renders SVG edges with arrowheads between
 * topology nodes. The SVG is absolutely positioned over the
 * topology container so edges appear behind the node cards.
 */
const TopologyEdges: React.FC<TopologyEdgesProps> = ({
    edges,
    nodes,
    nodeWidth,
    nodeHeight,
    topologyType,
    labelBackground,
}) => {
    const colors = useEdgeColors();
    const theme = useTheme();
    const nodeMap = useMemo(
        () => new Map(nodes.map(n => [n.id, n])),
        [nodes],
    );

    const isSpockMesh =
        topologyType === 'spock_mesh';

    return (
        <svg
            style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                height: '100%',
                pointerEvents: 'none',
                overflow: 'visible',
            }}
        >
            <defs>
                {Object.entries(colors).map(([type, color]) => (
                    <marker
                        key={type}
                        id={markerId(type)}
                        viewBox="0 0 6 10"
                        refX="5"
                        refY="5"
                        markerWidth="6"
                        markerHeight="10"
                        orient="auto-start-reverse"
                    >
                        <polygon
                            points="0 0, 6 5, 0 10"
                            fill={color}
                        />
                    </marker>
                ))}
            </defs>

            {edges.map((edge, idx) => {
                const source = nodeMap.get(edge.sourceId);
                const target = nodeMap.get(edge.targetId);
                if (!source || !target) {return null;}

                const color = colors[edge.replicationType] ||
                    theme.palette.grey[500];

                let x1: number, y1: number, x2: number, y2: number;
                let path: string;

                // Determine whether the two nodes sit on the
                // same row (horizontal) or different rows.
                const isVerticalEdge =
                    Math.abs(source.y - target.y) > 1;
                const isSameLevel = !isVerticalEdge;

                if (isVerticalEdge) {
                    // Determine which node is upper (smaller y)
                    // and which is lower (larger y) so anchors
                    // are always bottom-of-upper to top-of-lower.
                    const upper =
                        source.y <= target.y ? source : target;
                    const lower =
                        source.y <= target.y ? target : source;

                    x1 = upper.x + nodeWidth / 2;
                    y1 = upper.y + nodeHeight;
                    x2 = lower.x + nodeWidth / 2;
                    y2 = lower.y;

                    const midY = (y1 + y2) / 2;
                    path =
                        `M ${x1} ${y1} ` +
                        `C ${x1} ${midY} ${x2} ${midY} ` +
                        `${x2} ${y2}`;
                } else if (isSameLevel && isSpockMesh) {
                    // For spock_mesh spock edges, determine
                    // whether source and target are adjacent.
                    const isSpockEdge =
                        edge.replicationType === 'spock';

                    if (isSpockEdge) {
                        const leftX = Math.min(source.x, target.x);
                        const rightX = Math.max(source.x, target.x);
                        const intermediateCount = nodes.filter(
                            n =>
                                n.role === 'spock_node' &&
                                n.id !== source.id &&
                                n.id !== target.id &&
                                n.x > leftX &&
                                n.x < rightX,
                        ).length;

                        if (intermediateCount === 0) {
                            // Adjacent: side-to-side straight
                            // horizontal line
                            if (source.x < target.x) {
                                x1 = source.x + nodeWidth;
                                y1 = source.y + nodeHeight / 2;
                                x2 = target.x;
                                y2 = target.y + nodeHeight / 2;
                            } else {
                                x1 = source.x;
                                y1 = source.y + nodeHeight / 2;
                                x2 = target.x + nodeWidth;
                                y2 = target.y + nodeHeight / 2;
                            }
                            path = `M ${x1} ${y1} L ${x2} ${y2}`;
                        } else {
                            // Non-adjacent: connect at top center
                            // and arc above intermediate nodes
                            x1 = source.x + nodeWidth / 2;
                            y1 = source.y;
                            x2 = target.x + nodeWidth / 2;
                            y2 = target.y;
                            const midX = (x1 + x2) / 2;
                            const midY =
                                y1 - (nodeHeight +
                                40 * intermediateCount);
                            path =
                                `M ${x1} ${y1} ` +
                                `Q ${midX} ${midY} ${x2} ${y2}`;
                        }
                    } else {
                        // Non-spock horizontal edges in spock_mesh:
                        // connect from right side to left side
                        if (source.x < target.x) {
                            x1 = source.x + nodeWidth;
                            y1 = source.y + nodeHeight / 2;
                            x2 = target.x;
                            y2 = target.y + nodeHeight / 2;
                        } else {
                            x1 = source.x;
                            y1 = source.y + nodeHeight / 2;
                            x2 = target.x + nodeWidth;
                            y2 = target.y + nodeHeight / 2;
                        }
                        const dx = x2 - x1;
                        const dy = y2 - y1;
                        const dist =
                            Math.sqrt(dx * dx + dy * dy);
                        const curveOffset =
                            Math.min(dist * 0.3, 40);
                        const midX = (x1 + x2) / 2;
                        const midY =
                            (y1 + y2) / 2 - curveOffset;
                        path =
                            `M ${x1} ${y1} ` +
                            `Q ${midX} ${midY} ${x2} ${y2}`;
                    }
                } else if (isSameLevel) {
                    // Same-level edges in non-spock topologies
                    // (e.g. two standbys with a relationship):
                    // connect from the side of each node.
                    if (source.x < target.x) {
                        x1 = source.x + nodeWidth;
                        y1 = source.y + nodeHeight / 2;
                        x2 = target.x;
                        y2 = target.y + nodeHeight / 2;
                    } else {
                        x1 = source.x;
                        y1 = source.y + nodeHeight / 2;
                        x2 = target.x + nodeWidth;
                        y2 = target.y + nodeHeight / 2;
                    }
                    path = `M ${x1} ${y1} L ${x2} ${y2}`;
                } else {
                    // Tree layout: connect bottom of source to top
                    // of target.
                    x1 = source.x + nodeWidth / 2;
                    y1 = source.y + nodeHeight;
                    x2 = target.x + nodeWidth / 2;
                    y2 = target.y;

                    const midY = (y1 + y2) / 2;
                    path = `M ${x1} ${y1} C ${x1} ${midY} ${x2} ${midY} ${x2} ${y2}`;
                }

                let labelX = (x1 + x2) / 2;
                let labelY = (y1 + y2) / 2;

                if (isVerticalEdge) {
                    // For vertical bezier curves, compute the
                    // midpoint of the cubic bezier at t=0.5.
                    const midY = (y1 + y2) / 2;
                    labelX =
                        (x1 + x1 + x2 + x2) / 4;
                    labelY =
                        (y1 + midY + midY + y2) / 4;
                } else if (isSameLevel && isSpockMesh) {
                    const isSpockEdge =
                        edge.replicationType === 'spock';

                    if (isSpockEdge) {
                        const leftX = Math.min(
                            source.x, target.x,
                        );
                        const rightX = Math.max(
                            source.x, target.x,
                        );
                        const intermediateCount = nodes.filter(
                            n =>
                                n.role === 'spock_node' &&
                                n.id !== source.id &&
                                n.id !== target.id &&
                                n.x > leftX &&
                                n.x < rightX,
                        ).length;

                        if (intermediateCount > 0) {
                            // Place label at the quadratic
                            // bezier apex (t=0.5):
                            // P = (P0 + 2*Pc + P1) / 4
                            const cy =
                                y1 - (nodeHeight +
                                40 * intermediateCount);
                            labelX =
                                (x1 + 2 * ((x1 + x2) / 2) +
                                x2) / 4;
                            labelY =
                                (y1 + 2 * cy + y2) / 4;
                        }
                    } else {
                        // Non-spock horizontal in spock_mesh:
                        // offset label to match the curve
                        const dx = x2 - x1;
                        const dy = y2 - y1;
                        const dist =
                            Math.sqrt(dx * dx + dy * dy);
                        const curveOffset =
                            Math.min(dist * 0.3, 40);
                        labelY =
                            (y1 + y2) / 2 - curveOffset / 2;
                    }
                }

                return (
                    <g key={`${edge.sourceId}-${edge.targetId}-${idx}`}>
                        <path
                            d={path}
                            fill="none"
                            stroke={color}
                            strokeWidth={2}
                            strokeOpacity={0.65}
                            markerEnd={
                                `url(#${markerId(edge.replicationType)})`
                            }
                            markerStart={
                                edge.bidirectional
                                    ? `url(#${markerId(edge.replicationType)})`
                                    : undefined
                            }
                        />
                        {(() => {
                            const label = EDGE_LABELS[edge.replicationType];
                            const estimatedWidth = label.length * 8 + 12;
                            const rectHeight = 20;
                            return (
                                <>
                                    <rect
                                        x={labelX - estimatedWidth / 2}
                                        y={labelY - rectHeight / 2}
                                        width={estimatedWidth}
                                        height={rectHeight}
                                        fill={labelBackground}
                                        rx="3"
                                    />
                                    <text
                                        x={labelX}
                                        y={labelY}
                                        textAnchor="middle"
                                        dominantBaseline="central"
                                        fill={theme.palette.text.secondary}
                                        fontSize="14"
                                        fontFamily="inherit"
                                    >
                                        {label}
                                    </text>
                                </>
                            );
                        })()}
                    </g>
                );
            })}
        </svg>
    );
};

export default TopologyEdges;
