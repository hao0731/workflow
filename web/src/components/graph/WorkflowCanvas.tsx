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

    // Convert workflow to React Flow nodes/edges
    const { rawNodes, rawEdges } = useMemo(() => {
        if (!currentWorkflow) {
            return { rawNodes: [], rawEdges: [] };
        }

        const rawNodes: CustomNodeType[] = currentWorkflow.nodes.map((n) => ({
            id: n.id,
            type: 'custom',
            position: { x: 0, y: 0 }, // Will be set by Dagre
            data: {
                label: n.name || n.id,
                type: n.type,
                status: (nodeStatuses.get(n.id) || 'pending') as NodeStatus,
            },
        }));

        const rawEdges: Edge[] = currentWorkflow.connections.map((c, i) => ({
            id: `e-${c.from_node}-${c.to_node}-${i}`,
            source: c.from_node,
            target: c.to_node,
            animated: nodeStatuses.get(c.from_node) === 'running',
            style: { stroke: '#6b7280' },
        }));

        return { rawNodes, rawEdges };
    }, [currentWorkflow, nodeStatuses]);

    // Apply Dagre layout
    const { nodes, edges } = useDagreLayout(rawNodes as Node[], rawEdges);

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
