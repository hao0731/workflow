import { useMemo } from 'react';
import type { Node, Edge } from '@xyflow/react';

interface LayoutOptions {
    direction?: 'LR' | 'TB';
    nodeWidth?: number;
    nodeHeight?: number;
    nodeSep?: number;
    rankSep?: number;
}

interface NodeLevel {
    [nodeId: string]: number;
}

/**
 * Simple topological sort-based layout algorithm
 * Arranges nodes in layers based on their dependencies
 */
export function useDagreLayout(
    nodes: Node[],
    edges: Edge[],
    options: LayoutOptions = {}
) {
    const {
        direction = 'LR',
        nodeWidth = 180,
        nodeHeight = 70,
        nodeSep = 80,
        rankSep = 120,
    } = options;

    return useMemo(() => {
        if (nodes.length === 0) {
            return { nodes: [], edges: [] };
        }

        // Build adjacency list
        const outgoing: Map<string, string[]> = new Map();
        const incoming: Map<string, string[]> = new Map();

        nodes.forEach(n => {
            outgoing.set(n.id, []);
            incoming.set(n.id, []);
        });

        edges.forEach(edge => {
            outgoing.get(edge.source)?.push(edge.target);
            incoming.get(edge.target)?.push(edge.source);
        });

        // Calculate levels using topological layering
        const levels: NodeLevel = {};
        const visited = new Set<string>();

        // Find root nodes (no incoming edges)
        const roots = nodes.filter(n => (incoming.get(n.id)?.length || 0) === 0);

        // BFS to assign levels
        const queue: { id: string; level: number }[] = roots.map(n => ({ id: n.id, level: 0 }));

        while (queue.length > 0) {
            const { id, level } = queue.shift()!;

            if (visited.has(id)) {
                // Update level if we found a longer path
                levels[id] = Math.max(levels[id] || 0, level);
                continue;
            }

            visited.add(id);
            levels[id] = level;

            const targets = outgoing.get(id) || [];
            targets.forEach(target => {
                queue.push({ id: target, level: level + 1 });
            });
        }

        // Handle disconnected nodes
        nodes.forEach(n => {
            if (!(n.id in levels)) {
                levels[n.id] = 0;
            }
        });

        // Group nodes by level
        const nodesByLevel: Map<number, Node[]> = new Map();
        nodes.forEach(n => {
            const level = levels[n.id];
            if (!nodesByLevel.has(level)) {
                nodesByLevel.set(level, []);
            }
            nodesByLevel.get(level)!.push(n);
        });

        // Calculate positions
        const layoutedNodes = nodes.map(node => {
            const level = levels[node.id];
            const nodesAtLevel = nodesByLevel.get(level) || [];
            const indexAtLevel = nodesAtLevel.findIndex(n => n.id === node.id);

            const levelCount = nodesAtLevel.length;
            const totalHeight = levelCount * nodeHeight + (levelCount - 1) * nodeSep;
            const startY = -totalHeight / 2;

            let x: number, y: number;

            if (direction === 'LR') {
                x = level * (nodeWidth + rankSep) + 40;
                y = startY + indexAtLevel * (nodeHeight + nodeSep) + 200;
            } else {
                x = startY + indexAtLevel * (nodeWidth + nodeSep) + 200;
                y = level * (nodeHeight + rankSep) + 40;
            }

            return {
                ...node,
                position: { x, y },
            };
        });

        return { nodes: layoutedNodes, edges };
    }, [nodes, edges, direction, nodeWidth, nodeHeight, nodeSep, rankSep]);
}
