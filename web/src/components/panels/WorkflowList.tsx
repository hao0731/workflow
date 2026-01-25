import { useState } from 'react';
import { useWorkflow } from '../../context/WorkflowContext';

export default function WorkflowList() {
    const { workflows, currentWorkflowId, setCurrentWorkflowId, loading } = useWorkflow();
    const [search, setSearch] = useState('');

    const filtered = workflows.filter((wf) =>
        wf.id.toLowerCase().includes(search.toLowerCase())
    );

    return (
        <div className="workflow-list">
            <h3>Workflows</h3>

            <input
                type="text"
                placeholder="Search workflows..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="search-input"
            />

            {loading && <p style={{ color: '#6b7280' }}>Loading...</p>}

            {filtered.length === 0 && !loading && (
                <p style={{ color: '#6b7280' }}>No workflows found</p>
            )}

            {filtered.map((wf) => (
                <div
                    key={wf.id}
                    className={`workflow-item ${currentWorkflowId === wf.id ? 'selected' : ''}`}
                    onClick={() => setCurrentWorkflowId(wf.id)}
                >
                    <div style={{ fontWeight: 500 }}>{wf.id}</div>
                    <div style={{ color: '#9ca3af', fontSize: '0.75rem' }}>
                        {wf.nodes.length} nodes
                    </div>
                </div>
            ))}
        </div>
    );
}
