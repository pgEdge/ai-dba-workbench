/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useMemo, useCallback } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Popover from '@mui/material/Popover';
import Typography from '@mui/material/Typography';
import { alpha, useTheme } from '@mui/material/styles';
import { PlanNode } from '../../../hooks/useQueryPlan';
import { formatNumber } from '../../../utils/formatters';

/** Monospace font stack for conditions and filters. */
const MONO_FONT = '"JetBrains Mono", "SF Mono", monospace';

/** Layout constants. */
const TILE_WIDTH = 180;
const TILE_HEIGHT = 56;
const H_GAP = 80;
const V_GAP = 16;
const PADDING = 20;

/** Cost threshold for warning highlight (percentage of root cost). */
const WARNING_THRESHOLD = 0.5;

/** Cost threshold for critical highlight (percentage of root cost). */
const CRITICAL_THRESHOLD = 0.8;

/** Maximum container height before scrolling. */
const MAX_CONTAINER_HEIGHT = 600;

// -----------------------------------------------------------------------
// Layout types and algorithm
// -----------------------------------------------------------------------

interface LayoutNode {
    id: number;
    x: number;
    y: number;
    node: PlanNode;
    parentId: number | null;
}

interface FlatNode {
    id: number;
    depth: number;
    node: PlanNode;
    parentId: number | null;
    children: number[];
}

/**
 * Flatten the PlanNode tree into an array of FlatNodes with
 * depth and parent references.
 */
function flattenTree(
    roots: PlanNode[],
): FlatNode[] {
    const result: FlatNode[] = [];
    let nextId = 0;

    function walk(
        node: PlanNode,
        depth: number,
        parentId: number | null,
    ): number {
        const id = nextId++;
        const children: number[] = [];
        const flat: FlatNode = {
            id,
            depth,
            node,
            parentId,
            children,
        };
        result.push(flat);

        if (Array.isArray(node.Plans)) {
            for (const child of node.Plans) {
                const childId = walk(child, depth + 1, id);
                children.push(childId);
            }
        }
        return id;
    }

    for (const root of roots) {
        walk(root, 0, null);
    }
    return result;
}

/**
 * Pure function that computes the visual layout of a plan tree.
 * Leaf scans are positioned at the left; the root node is at
 * the right.
 */
export function layoutPlanTree(plan: PlanNode[]): LayoutNode[] {
    if (!plan || plan.length === 0) {
        return [];
    }

    const flatNodes = flattenTree(plan);
    const maxDepth = Math.max(...flatNodes.map(n => n.depth));

    // Build a lookup for fast access.
    const nodeMap = new Map<number, FlatNode>();
    for (const fn of flatNodes) {
        nodeMap.set(fn.id, fn);
    }

    // Assign Y positions bottom-up: leaves get sequential slots,
    // parents center on the Y span of their children.
    let nextSlot = 0;
    const yPositions = new Map<number, number>();

    // Process nodes in reverse order (deepest first). Within the
    // same depth, maintain insertion order.
    const byDepth = [...flatNodes].sort(
        (a, b) => b.depth - a.depth,
    );

    for (const fn of byDepth) {
        if (fn.children.length === 0) {
            // Leaf node: assign the next sequential Y slot.
            yPositions.set(
                fn.id,
                PADDING + nextSlot * (TILE_HEIGHT + V_GAP),
            );
            nextSlot++;
        } else {
            // Parent node: center on children's Y span.
            const childYs = fn.children.map(
                cid => yPositions.get(cid)!,
            );
            const minY = Math.min(...childYs);
            const maxY = Math.max(...childYs);
            yPositions.set(fn.id, (minY + maxY) / 2);
        }
    }

    // Assign X positions: column = maxDepth - depth, so leaves
    // appear at the left and the root at the right.
    return flatNodes.map(fn => ({
        id: fn.id,
        x: PADDING + (maxDepth - fn.depth) * (TILE_WIDTH + H_GAP),
        y: yPositions.get(fn.id)!,
        node: fn.node,
        parentId: fn.parentId,
    }));
}

// -----------------------------------------------------------------------
// Helper functions
// -----------------------------------------------------------------------

/**
 * Determine the left border color based on cost ratio relative
 * to the root node's total cost.
 */
function getCostBorderColor(
    nodeCost: number,
    rootCost: number,
    warningColor: string,
    errorColor: string,
    dividerColor: string,
): string {
    if (rootCost <= 0) {
        return dividerColor;
    }
    const ratio = nodeCost / rootCost;
    if (ratio >= CRITICAL_THRESHOLD) {
        return errorColor;
    }
    if (ratio >= WARNING_THRESHOLD) {
        return warningColor;
    }
    return dividerColor;
}

/**
 * Format a cost range as "startup..total".
 */
function formatCostRange(startup: number, total: number): string {
    return `${startup.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}..${total.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

/**
 * Build the display label for line 1 of a tile.
 */
function nodeTypeLabel(node: PlanNode): string {
    if (node['Join Type']) {
        return `${node['Node Type']} (${node['Join Type']})`;
    }
    return node['Node Type'];
}

/**
 * Build the secondary label for line 2 of a tile.
 */
function secondaryLabel(node: PlanNode): string {
    if (node['Relation Name']) {
        if (node.Schema) {
            return `${node['Schema']}.${node['Relation Name']}`;
        }
        return node['Relation Name'];
    }
    if (node['Index Name']) {
        return node['Index Name'];
    }
    return formatCostRange(
        node['Startup Cost'],
        node['Total Cost'],
    );
}

// -----------------------------------------------------------------------
// Popover content
// -----------------------------------------------------------------------

interface PopoverContentProps {
    node: PlanNode;
}

/**
 * Renders the detail popover content for a plan node.
 */
const PopoverContent: React.FC<PopoverContentProps> = ({ node }) => {
    const conditions: { label: string; value: string }[] = [];
    if (node['Filter']) {
        conditions.push({ label: 'Filter', value: node['Filter'] });
    }
    if (node['Index Cond']) {
        conditions.push({
            label: 'Index Cond',
            value: node['Index Cond'],
        });
    }
    if (node['Hash Cond']) {
        conditions.push({
            label: 'Hash Cond',
            value: node['Hash Cond'],
        });
    }
    if (node['Sort Key'] && node['Sort Key'].length > 0) {
        conditions.push({
            label: 'Sort Key',
            value: node['Sort Key'].join(', '),
        });
    }
    if (node['Merge Cond']) {
        conditions.push({
            label: 'Merge Cond',
            value: node['Merge Cond'],
        });
    }
    if (node['Recheck Cond']) {
        conditions.push({
            label: 'Recheck Cond',
            value: node['Recheck Cond'],
        });
    }
    if (node['Join Filter']) {
        conditions.push({
            label: 'Join Filter',
            value: node['Join Filter'],
        });
    }
    if (node['Group Key'] && node['Group Key'].length > 0) {
        conditions.push({
            label: 'Group Key',
            value: node['Group Key'].join(', '),
        });
    }

    const relationLabel = node['Relation Name']
        ? (node['Schema']
            ? `${node['Schema']}.${node['Relation Name']}`
            : node['Relation Name'])
        : node['Index Name'] ?? null;

    return (
        <Box sx={{ p: 1.5, maxWidth: 360 }}>
            <Typography
                sx={{
                    fontWeight: 700,
                    fontSize: '0.875rem',
                    color: 'text.primary',
                    mb: 0.5,
                }}
            >
                {nodeTypeLabel(node)}
            </Typography>

            <Typography sx={{
                fontSize: '0.875rem',
                color: 'text.secondary',
            }}>
                Cost: {formatCostRange(
                    node['Startup Cost'],
                    node['Total Cost'],
                )}
            </Typography>
            <Typography sx={{
                fontSize: '0.875rem',
                color: 'text.secondary',
            }}>
                Rows: {node['Plan Rows'].toLocaleString()}
            </Typography>
            <Typography sx={{
                fontSize: '0.875rem',
                color: 'text.secondary',
            }}>
                Width: {formatNumber(node['Plan Width'])} bytes
            </Typography>

            {relationLabel && (
                <Typography sx={{
                    fontSize: '0.875rem',
                    color: 'text.secondary',
                    mt: 0.5,
                }}>
                    Relation: {relationLabel}
                    {node['Alias']
                        && node['Alias'] !== node['Relation Name']
                        ? ` (${node['Alias']})`
                        : ''}
                </Typography>
            )}

            {node['Output'] && node['Output'].length > 0 && (
                <Typography sx={{
                    fontSize: '0.875rem',
                    fontFamily: MONO_FONT,
                    color: 'text.secondary',
                    mt: 0.5,
                    wordBreak: 'break-word',
                }}>
                    Output: {node['Output'].length > 5
                        ? node['Output'].slice(0, 5).join(', ')
                            + ', ...'
                        : node['Output'].join(', ')}
                </Typography>
            )}
            {node['Strategy'] && (
                <Typography sx={{
                    fontSize: '0.875rem',
                    color: 'text.secondary',
                    mt: 0.5,
                }}>
                    Strategy: {node['Strategy']}
                </Typography>
            )}
            {node['Scan Direction'] && (
                <Typography sx={{
                    fontSize: '0.875rem',
                    color: 'text.secondary',
                    mt: 0.5,
                }}>
                    Scan Direction: {node['Scan Direction']}
                </Typography>
            )}
            {node['Parent Relationship'] && (
                <Typography sx={{
                    fontSize: '0.875rem',
                    color: 'text.secondary',
                    mt: 0.5,
                }}>
                    Parent: {node['Parent Relationship']}
                </Typography>
            )}
            {node['Workers Planned'] != null && (
                <Typography sx={{
                    fontSize: '0.875rem',
                    color: 'text.secondary',
                    mt: 0.5,
                }}>
                    Workers: {node['Workers Planned']} planned
                    {node['Workers Launched'] != null
                        ? `, ${node['Workers Launched']} launched`
                        : ''}
                </Typography>
            )}
            {node['Subplan Name'] && (
                <Typography sx={{
                    fontSize: '0.875rem',
                    color: 'text.secondary',
                    mt: 0.5,
                }}>
                    Subplan: {node['Subplan Name']}
                </Typography>
            )}
            {node['CTE Name'] && (
                <Typography sx={{
                    fontSize: '0.875rem',
                    color: 'text.secondary',
                    mt: 0.5,
                }}>
                    CTE: {node['CTE Name']}
                </Typography>
            )}

            {conditions.map(cond => (
                <Typography
                    key={cond.label}
                    sx={{
                        fontSize: '0.8125rem',
                        fontFamily: MONO_FONT,
                        color: 'text.secondary',
                        mt: 0.5,
                        wordBreak: 'break-word',
                    }}
                >
                    {cond.label}: {cond.value}
                </Typography>
            ))}
        </Box>
    );
};

// -----------------------------------------------------------------------
// PlanTree component
// -----------------------------------------------------------------------

interface PlanTreeProps {
    plan: PlanNode[];
}

/**
 * PlanTree renders a graphical flow diagram of a PostgreSQL JSON
 * EXPLAIN plan. Leaf scan nodes appear on the left and the root
 * node on the right, connected by SVG bezier arrows. Clicking a
 * tile opens a popover with full node details.
 */
const PlanTree: React.FC<PlanTreeProps> = ({ plan }) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';

    const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(
        null,
    );
    const [selectedNode, setSelectedNode] =
        useState<PlanNode | null>(null);

    const layout = useMemo(
        () => layoutPlanTree(plan),
        [plan],
    );

    const rootTotalCost = useMemo(() => {
        if (!plan || plan.length === 0) {
            return 0;
        }
        return plan[0]['Total Cost'];
    }, [plan]);

    const containerWidth = useMemo(() => {
        if (layout.length === 0) {
            return 0;
        }
        return Math.max(
            ...layout.map(n => n.x),
        ) + TILE_WIDTH + PADDING;
    }, [layout]);

    const containerHeight = useMemo(() => {
        if (layout.length === 0) {
            return 0;
        }
        return Math.max(
            ...layout.map(n => n.y),
        ) + TILE_HEIGHT + PADDING;
    }, [layout]);

    const handleTileClick = useCallback(
        (event: React.MouseEvent<HTMLElement>, node: PlanNode) => {
            setAnchorEl(event.currentTarget);
            setSelectedNode(node);
        },
        [],
    );

    const handlePopoverClose = useCallback(() => {
        setAnchorEl(null);
        setSelectedNode(null);
    }, []);

    if (!plan || plan.length === 0) {
        return (
            <Typography
                variant="body2"
                color="text.secondary"
                sx={{ textAlign: 'center', py: 2 }}
            >
                No plan data available
            </Typography>
        );
    }

    // Build a lookup for layout nodes by id.
    const layoutMap = new Map(layout.map(n => [n.id, n]));

    // Collect edges: each child points to its parent.
    const edges: { child: LayoutNode; parent: LayoutNode }[] = [];
    for (const ln of layout) {
        if (ln.parentId !== null) {
            const parent = layoutMap.get(ln.parentId);
            if (parent) {
                edges.push({ child: ln, parent });
            }
        }
    }

    const arrowColor = theme.palette.text.secondary;

    return (
        <Box sx={{ overflow: 'auto', maxHeight: MAX_CONTAINER_HEIGHT }}>
            <Box
                data-testid="plan-tree-container"
                sx={{
                    position: 'relative',
                    width: containerWidth,
                    height: containerHeight,
                    minWidth: containerWidth,
                    minHeight: containerHeight,
                }}
            >
                {/* SVG arrow overlay */}
                <svg
                    data-testid="plan-tree-svg"
                    style={{
                        position: 'absolute',
                        top: 0,
                        left: 0,
                        width: containerWidth,
                        height: containerHeight,
                        pointerEvents: 'none',
                    }}
                >
                    <defs>
                        <marker
                            id="plan-arrow"
                            viewBox="0 0 6 10"
                            refX="5"
                            refY="5"
                            markerWidth="6"
                            markerHeight="10"
                            orient="auto"
                        >
                            <polygon
                                points="0 0, 6 5, 0 10"
                                fill={arrowColor}
                                fillOpacity={0.5}
                            />
                        </marker>
                    </defs>

                    {edges.map(({ child, parent }) => {
                        const x1 = child.x + TILE_WIDTH;
                        const y1 = child.y + TILE_HEIGHT / 2;
                        const x2 = parent.x;
                        const y2 = parent.y + TILE_HEIGHT / 2;
                        const midX = (x1 + x2) / 2;

                        const path =
                            `M ${x1} ${y1} ` +
                            `C ${midX} ${y1} ` +
                            `${midX} ${y2} ` +
                            `${x2} ${y2}`;

                        return (
                            <path
                                key={`edge-${child.id}-${parent.id}`}
                                d={path}
                                fill="none"
                                stroke={arrowColor}
                                strokeWidth={1.5}
                                strokeOpacity={0.5}
                                markerEnd="url(#plan-arrow)"
                            />
                        );
                    })}
                </svg>

                {/* Node tiles */}
                {layout.map(ln => {
                    const borderColor = getCostBorderColor(
                        ln.node['Total Cost'],
                        rootTotalCost,
                        theme.palette.warning.main,
                        theme.palette.error.main,
                        theme.palette.divider,
                    );

                    return (
                        <Paper
                            key={`tile-${ln.id}`}
                            elevation={0}
                            onClick={(e) =>
                                handleTileClick(e, ln.node)
                            }
                            sx={{
                                position: 'absolute',
                                left: ln.x,
                                top: ln.y,
                                width: TILE_WIDTH,
                                height: TILE_HEIGHT,
                                p: 0.75,
                                borderRadius: 1.5,
                                bgcolor: isDark
                                    ? alpha(
                                        theme.palette.grey[800],
                                        0.8,
                                    )
                                    : theme.palette.grey[50],
                                border: '1px solid',
                                borderColor: 'divider',
                                borderLeft: `3px solid ${borderColor}`,
                                cursor: 'pointer',
                                overflow: 'hidden',
                                display: 'flex',
                                flexDirection: 'column',
                                justifyContent: 'center',
                                transition:
                                    'border-color 0.2s, '
                                    + 'box-shadow 0.2s',
                                '&:hover': {
                                    borderColor:
                                        theme.palette.primary.main,
                                    boxShadow: `0 0 0 1px ${alpha(
                                        theme.palette.primary.main,
                                        0.3,
                                    )}`,
                                },
                            }}
                            role="button"
                            tabIndex={0}
                            aria-label={
                                `View details for `
                                + `${nodeTypeLabel(ln.node)}`
                            }
                        >
                            <Typography
                                sx={{
                                    fontWeight: 700,
                                    fontSize: '0.875rem',
                                    color: 'text.primary',
                                    lineHeight: 1.2,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                }}
                            >
                                {nodeTypeLabel(ln.node)}
                            </Typography>
                            <Typography
                                sx={{
                                    fontSize: '0.75rem',
                                    color: 'text.secondary',
                                    lineHeight: 1.2,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    mt: 0.25,
                                }}
                            >
                                {secondaryLabel(ln.node)}
                            </Typography>
                        </Paper>
                    );
                })}

                {/* Detail popover */}
                <Popover
                    open={Boolean(anchorEl)}
                    anchorEl={anchorEl}
                    onClose={handlePopoverClose}
                    anchorOrigin={{
                        vertical: 'bottom',
                        horizontal: 'center',
                    }}
                    transformOrigin={{
                        vertical: 'top',
                        horizontal: 'center',
                    }}
                >
                    {selectedNode && (
                        <PopoverContent node={selectedNode} />
                    )}
                </Popover>
            </Box>
        </Box>
    );
};

export default PlanTree;
