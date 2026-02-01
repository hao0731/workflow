import { useMemo, useCallback } from 'react';
import {
    ReactFlow,
    Background,
    Controls,
    type Node,
    type Edge,
    type NodeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import CustomNode, { type CustomNodeType } from './CustomNode';
import { useDagreLayout } from '../../hooks/useDagreLayout';
import { useWorkflow } from '../../context/WorkflowContext';
import { useExecution } from '../../context/ExecutionContext';
import { useUI } from '../../context/UIContext';
import type { NodeStatus } from '../../types';

const nodeTypes: NodeTypes = {
    custom: CustomNode,
};



export default function WorkflowCanvas() {
    const { currentWorkflow } = useWorkflow();
    const { nodeStatuses } = useExecution();
    const { dispatch } = useUI();

    // Build structural nodes/edges from workflow topology only (stable across status changes)
    const { structuralNodes, structuralEdges } = useMemo(() => {
        if (!currentWorkflow) {
            return { structuralNodes: [], structuralEdges: [] };
        }

        const structuralNodes: CustomNodeType[] = currentWorkflow.nodes.map((n) => ({
            id: n.id,
            type: 'custom',
            position: { x: 0, y: 0 },
            initialWidth: 180,
            initialHeight: 70,
            data: {
                label: n.name || n.id,
                type: n.type,
                status: 'pending' as NodeStatus,
            },
        }));

        const structuralEdges: Edge[] = currentWorkflow.connections.map((c, i) => ({
            id: `e-${c.from_node}-${c.to_node}-${i}`,
            source: c.from_node,
            target: c.to_node,
            style: { stroke: '#6b7280' },
        }));

        return { structuralNodes, structuralEdges };
    }, [currentWorkflow]);

    // Apply layout based on stable structure
    const { nodes: layoutedNodes, edges: layoutedEdges } = useDagreLayout(
        structuralNodes as Node[],
        structuralEdges,
    );

    // Apply status updates to layouted nodes/edges without changing structure
    const nodes = useMemo(() => {
        return layoutedNodes.map((node) => ({
            ...node,
            data: {
                ...node.data,
                status: (nodeStatuses.get(node.id) || 'pending') as NodeStatus,
            },
        }));
    }, [layoutedNodes, nodeStatuses]);

    const edges = useMemo(() => {
        return layoutedEdges.map((edge) => ({
            ...edge,
            animated: nodeStatuses.get(edge.source) === 'running',
        }));
    }, [layoutedEdges, nodeStatuses]);

    const onNodeClick = useCallback(
        (_: React.MouseEvent, node: Node) => {
            dispatch({ type: 'SELECT_NODE', nodeId: node.id });
        },
        [dispatch]
    );

    const onPaneClick = useCallback(() => {
        dispatch({ type: 'SELECT_NODE', nodeId: null });
    }, [dispatch]);

    if (!currentWorkflow) {
        return (
            <div
                style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    height: '100%',
                    color: '#6b7280',
                }}
            >
                Select a workflow to view
            </div>
        );
    }

    return (
        <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodeClick={onNodeClick}
            onPaneClick={onPaneClick}
            fitView
            fitViewOptions={{ padding: 0.2 }}
        >
            <Background color="#374151" gap={16} />
            <Controls />
        </ReactFlow>
    );
}
