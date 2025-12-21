import { useState, useEffect } from 'react';
import { fetchExecutions } from '../api';
import type { Execution } from '../api';

interface Props {
    workflowId: string;
    selectedId: string | null;
    onSelect: (id: string | null) => void;
}

export default function ExecutionList({ workflowId, selectedId, onSelect }: Props) {
    const [executions, setExecutions] = useState<Execution[]>([]);

    useEffect(() => {
        fetchExecutions(workflowId).then(setExecutions);
    }, [workflowId]);

    return (
        <div className="execution-list">
            <h3>Executions</h3>
            {executions.length === 0 && <p style={{ color: '#6b7280' }}>No executions yet</p>}
            {executions.map((exec) => (
                <div
                    key={exec.id}
                    className={`execution-item ${selectedId === exec.id ? 'selected' : ''}`}
                    onClick={() => onSelect(selectedId === exec.id ? null : exec.id)}
                >
                    <span className={`status-dot ${exec.status}`} />
                    <div>
                        <div style={{ fontWeight: 500 }}>{exec.id.slice(0, 16)}...</div>
                        <div style={{ color: '#9ca3af', fontSize: '0.75rem' }}>
                            {new Date(exec.started_at).toLocaleTimeString()}
                        </div>
                    </div>
                </div>
            ))}
        </div>
    );
}
