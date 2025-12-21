import type { Node } from '@xyflow/react';
import type { NodeEvent } from '../api';

interface Props {
    node: Node;
    events: NodeEvent[];
    onClose: () => void;
}

export default function NodeDetails({ node, events, onClose }: Props) {
    const status = (node.data?.status as string) || 'pending';
    const lastEvent = events[events.length - 1];

    return (
        <div className="node-details">
            <div className="info">
                <div>
                    <div className="label">Node</div>
                    <div className="value">{node.data?.label as string}</div>
                </div>
                <div>
                    <div className="label">Type</div>
                    <div className="value">{node.data?.type as string}</div>
                </div>
                <div>
                    <div className="label">Status</div>
                    <span className={`status-badge ${status}`}>{status}</span>
                </div>
                {lastEvent && (
                    <div>
                        <div className="label">Last Event</div>
                        <div className="value">{new Date(lastEvent.timestamp).toLocaleTimeString()}</div>
                    </div>
                )}
            </div>
            <button className="close-btn" onClick={onClose}>
                ×
            </button>
        </div>
    );
}
