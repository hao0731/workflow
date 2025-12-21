import { useState, useCallback, useEffect } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  applyNodeChanges,
  applyEdgeChanges,
} from '@xyflow/react';
import type { Node, Edge, NodeChange, EdgeChange } from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import WorkflowSelector from './components/WorkflowSelector';
import ExecutionList from './components/ExecutionList';
import NodeDetails from './components/NodeDetails';
import { fetchWorkflow, fetchExecutionEvents } from './api';
import type { Workflow, NodeEvent } from './api';
import './App.css';

// Node status colors
const statusColors: Record<string, string> = {
  pending: '#6b7280',
  scheduled: '#3b82f6',
  running: '#3b82f6',
  completed: '#22c55e',
  failed: '#ef4444',
};

function workflowToFlow(workflow: Workflow | null): { nodes: Node[]; edges: Edge[] } {
  if (!workflow) return { nodes: [], edges: [] };

  const nodes: Node[] = workflow.nodes.map((n, i) => ({
    id: n.id,
    position: { x: 100, y: i * 120 },
    data: { label: n.name || n.id, type: n.type, status: 'pending' },
    style: {
      background: statusColors.pending,
      color: '#fff',
      border: '2px solid #4b5563',
      borderRadius: '8px',
      padding: '10px 20px',
      fontWeight: 500,
    },
  }));

  const edges: Edge[] = workflow.connections.map((c) => ({
    id: `${c.from_node}-${c.to_node}`,
    source: c.from_node,
    target: c.to_node,
    animated: false,
    style: { stroke: '#6b7280' },
  }));

  return { nodes, edges };
}

function applyStatusToNodes(nodes: Node[], events: NodeEvent[]): Node[] {
  const statusMap: Record<string, string> = {};

  events.forEach((e) => {
    if (e.type.includes('scheduled')) statusMap[e.node_id] = 'scheduled';
    if (e.type.includes('completed')) statusMap[e.node_id] = 'completed';
    if (e.type.includes('failed')) statusMap[e.node_id] = 'failed';
  });

  return nodes.map((n) => {
    const status = statusMap[n.id] || 'pending';
    return {
      ...n,
      data: { ...n.data, status },
      style: {
        ...n.style,
        background: statusColors[status],
        boxShadow: status === 'scheduled' ? '0 0 15px #3b82f6' : 'none',
      },
    };
  });
}

export default function App() {
  const [workflowId, setWorkflowId] = useState<string>('order-service');
  const [executionId, setExecutionId] = useState<string | null>(null);
  const [, setWorkflow] = useState<Workflow | null>(null);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [events, setEvents] = useState<NodeEvent[]>([]);

  // Load workflow
  useEffect(() => {
    fetchWorkflow(workflowId).then((wf) => {
      setWorkflow(wf);
      const { nodes: n, edges: e } = workflowToFlow(wf);
      setNodes(n);
      setEdges(e);
    });
  }, [workflowId]);

  // Load execution events
  useEffect(() => {
    if (executionId) {
      fetchExecutionEvents(executionId).then((evts) => {
        setEvents(evts);
        setNodes((prev) => applyStatusToNodes(prev, evts));
      });
    } else {
      // Reset to pending
      setNodes((prev) => prev.map((n) => ({
        ...n,
        data: { ...n.data, status: 'pending' },
        style: { ...n.style, background: statusColors.pending, boxShadow: 'none' },
      })));
    }
  }, [executionId]);

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => setNodes((nds) => applyNodeChanges(changes, nds)),
    []
  );

  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => setEdges((eds) => applyEdgeChanges(changes, eds)),
    []
  );

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    setSelectedNode(node);
  }, []);

  return (
    <div className="app">
      <header className="header">
        <h1>🔄 Workflow Dashboard</h1>
        <WorkflowSelector value={workflowId} onChange={setWorkflowId} />
      </header>

      <div className="main">
        <aside className="sidebar">
          <ExecutionList
            workflowId={workflowId}
            selectedId={executionId}
            onSelect={setExecutionId}
          />
        </aside>

        <div className="graph-container">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeClick={onNodeClick}
            fitView
          >
            <Background color="#374151" gap={16} />
            <Controls />
            <MiniMap nodeColor={(n) => statusColors[n.data?.status as string] || '#6b7280'} />
          </ReactFlow>
        </div>
      </div>

      {selectedNode && (
        <NodeDetails
          node={selectedNode}
          events={events.filter(e => e.node_id === selectedNode.id)}
          onClose={() => setSelectedNode(null)}
        />
      )}
    </div>
  );
}
