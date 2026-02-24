import { useMemo } from 'react';
import { useWorkflow } from '../../context/WorkflowContext';
import { useExecution } from '../../context/ExecutionContext';
import { useUI } from '../../context/UIContext';

export default function NodeInspector() {
    const { currentWorkflow } = useWorkflow();
    const { nodeStatuses, events } = useExecution();
    const { state } = useUI();

    const selectedNode = useMemo(() => {
        if (!currentWorkflow || !state.selectedNodeId) return null;
        return currentWorkflow.nodes.find((n) => n.id === state.selectedNodeId);
    }, [currentWorkflow, state.selectedNodeId]);

    const nodeConnections = useMemo(() => {
        if (!currentWorkflow || !state.selectedNodeId) return { inputs: [], outputs: [] };

        const inputs = currentWorkflow.connections
            .filter((c) => c.to_node === state.selectedNodeId)
            .map((c) => ({ from: c.from_node, port: c.from_port || 'default' }));

        const outputs = currentWorkflow.connections
            .filter((c) => c.from_node === state.selectedNodeId)
            .map((c) => ({ to: c.to_node, port: c.from_port || 'default' }));

        return { inputs, outputs };
    }, [currentWorkflow, state.selectedNodeId]);

    const nodeEvents = useMemo(() => {
        if (!state.selectedNodeId) return [];
        return events.filter((e) => e.node_id === state.selectedNodeId);
    }, [events, state.selectedNodeId]);

    if (!selectedNode) {
        return (
            <div className="node-inspector">
                <h3>Node Inspector</h3>
                <p style={{ color: '#6b7280' }}>Select a node to view details</p>
            </div>
        );
    }

    const status = nodeStatuses.get(selectedNode.id) || 'pending';

    return (
        <div className="node-inspector">
            <h3>Node Inspector</h3>

            <div className="inspector-section">
                <div className="inspector-label">ID</div>
                <div className="inspector-value">{selectedNode.id}</div>
            </div>

            <div className="inspector-section">
                <div className="inspector-label">Type</div>
                <div className="inspector-value">{selectedNode.type}</div>
            </div>

            <div className="inspector-section">
                <div className="inspector-label">Name</div>
                <div className="inspector-value">{selectedNode.name || '—'}</div>
            </div>

            <div className="inspector-section">
                <div className="inspector-label">Status</div>
                <span className={`status-badge ${status}`}>{status}</span>
            </div>

            {selectedNode.parameters && Object.keys(selectedNode.parameters).length > 0 && (
                <div className="inspector-section">
                    <div className="inspector-label">Parameters</div>
                    <pre className="json-view">
                        {JSON.stringify(selectedNode.parameters, null, 2)}
                    </pre>
                </div>
            )}

            <div className="inspector-section">
                <div className="inspector-label">Connections</div>
                {nodeConnections.inputs.length > 0 && (
                    <div style={{ marginBottom: '0.5rem' }}>
                        <span style={{ color: '#9ca3af' }}>Inputs from:</span>
                        {nodeConnections.inputs.map((c, i) => (
                            <div key={i} style={{ marginLeft: '1rem' }}>
                                {c.from} ({c.port})
                            </div>
                        ))}
                    </div>
                )}
                {nodeConnections.outputs.length > 0 && (
                    <div>
                        <span style={{ color: '#9ca3af' }}>Outputs to:</span>
                        {nodeConnections.outputs.map((c, i) => (
                            <div key={i} style={{ marginLeft: '1rem' }}>
                                {c.to} ({c.port})
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {nodeEvents.length > 0 && (
                <div className="inspector-section">
                    <div className="inspector-label">Events ({nodeEvents.length})</div>
                    {nodeEvents.slice(-3).map((evt) => (
                        <div key={evt.id} style={{ fontSize: '0.75rem', marginBottom: '0.25rem' }}>
                            {evt.type.split('.').pop()} - {new Date(evt.timestamp).toLocaleTimeString()}
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}
