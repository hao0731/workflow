import { memo } from 'react';
import { Handle, Position, type Node } from '@xyflow/react';
import type { NodeStatus } from '../../types';

export interface CustomNodeData extends Record<string, unknown> {
    label: string;
    type: string;
    status: NodeStatus;
    outputPorts?: string[];
}

export type CustomNodeType = Node<CustomNodeData>;

const statusColors: Record<NodeStatus, string> = {
    pending: '#6b7280',
    scheduled: '#3b82f6',
    running: '#3b82f6',
    completed: '#22c55e',
    failed: '#ef4444',
};

const statusGlow: Record<NodeStatus, string> = {
    pending: 'none',
    scheduled: '0 0 15px #3b82f6',
    running: '0 0 20px #3b82f6',
    completed: 'none',
    failed: '0 0 10px #ef4444',
};

interface CustomNodeProps {
    data: CustomNodeData;
    selected?: boolean;
}

function CustomNode({ data, selected }: CustomNodeProps) {
    const status = data.status || 'pending';
    const bgColor = statusColors[status];
    const glow = statusGlow[status];

    return (
        <div
            style={{
                background: bgColor,
                color: '#fff',
                border: selected ? '2px solid #fff' : '2px solid #4b5563',
                borderRadius: '8px',
                padding: '10px 16px',
                minWidth: '160px',
                boxShadow: glow,
                transition: 'all 0.2s ease',
            }}
        >
            <Handle type="target" position={Position.Left} />

            <div style={{ fontWeight: 600, fontSize: '0.875rem' }}>{data.label}</div>
            <div style={{ fontSize: '0.75rem', opacity: 0.8, marginTop: '2px' }}>
                {data.type}
            </div>

            {data.outputPorts && data.outputPorts.length > 1 && (
                <div style={{ display: 'flex', gap: '8px', marginTop: '6px', fontSize: '0.65rem' }}>
                    {data.outputPorts.map((port) => (
                        <span
                            key={port}
                            style={{
                                background: 'rgba(255,255,255,0.2)',
                                padding: '2px 6px',
                                borderRadius: '4px',
                            }}
                        >
                            {port}
                        </span>
                    ))}
                </div>
            )}

            <Handle type="source" position={Position.Right} />
        </div>
    );
}

export default memo(CustomNode);
