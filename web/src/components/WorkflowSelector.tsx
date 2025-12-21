import { fetchWorkflows } from '../api';
import { useState, useEffect } from 'react';

interface Props {
    value: string;
    onChange: (id: string) => void;
}

export default function WorkflowSelector({ value, onChange }: Props) {
    const [workflows, setWorkflows] = useState<{ id: string }[]>([]);

    useEffect(() => {
        fetchWorkflows().then(setWorkflows);
    }, []);

    return (
        <div className="workflow-selector">
            <label>Workflow:</label>
            <select value={value} onChange={(e) => onChange(e.target.value)}>
                {workflows.map((wf) => (
                    <option key={wf.id} value={wf.id}>
                        {wf.id}
                    </option>
                ))}
            </select>
        </div>
    );
}
