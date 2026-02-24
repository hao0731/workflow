import { useRef, useEffect, useState } from 'react';
import { useExecution } from '../../context/ExecutionContext';
import { useUI } from '../../context/UIContext';

export default function ExecutionTimeline() {
    const { events } = useExecution();
    const { dispatch } = useUI();
    const listRef = useRef<HTMLDivElement>(null);
    const [autoScroll, setAutoScroll] = useState(true);
    const [expandedEvents, setExpandedEvents] = useState<Set<string>>(new Set());

    // Auto-scroll to bottom when new events arrive
    useEffect(() => {
        if (autoScroll && listRef.current) {
            listRef.current.scrollTop = listRef.current.scrollHeight;
        }
    }, [events, autoScroll]);

    const toggleExpanded = (eventId: string) => {
        setExpandedEvents((prev) => {
            const next = new Set(prev);
            if (next.has(eventId)) {
                next.delete(eventId);
            } else {
                next.add(eventId);
            }
            return next;
        });
    };

    const handleEventClick = (nodeId: string | undefined) => {
        if (nodeId) {
            dispatch({ type: 'SELECT_NODE', nodeId });
        }
    };

    const getEventIcon = (type: string) => {
        if (type.includes('started')) return '○';
        if (type.includes('completed')) return '●';
        if (type.includes('failed')) return '✕';
        if (type.includes('scheduled')) return '◐';
        return '•';
    };

    const getEventColor = (type: string) => {
        if (type.includes('failed')) return '#ef4444';
        if (type.includes('completed')) return '#22c55e';
        if (type.includes('scheduled') || type.includes('started')) return '#3b82f6';
        return '#6b7280';
    };

    if (events.length === 0) {
        return (
            <div className="timeline-empty">
                No execution events. Select an execution to view its timeline.
            </div>
        );
    }

    let prevTime: Date | null = null;

    return (
        <div className="timeline-container">
            <div className="timeline-controls">
                <label>
                    <input
                        type="checkbox"
                        checked={autoScroll}
                        onChange={(e) => setAutoScroll(e.target.checked)}
                    />
                    Auto-scroll
                </label>
            </div>

            <div className="timeline-list" ref={listRef}>
                {events.map((evt) => {
                    const time = new Date(evt.timestamp);
                    const delta = prevTime ? time.getTime() - prevTime.getTime() : 0;
                    prevTime = time;

                    const isExpanded = expandedEvents.has(evt.id);
                    const eventLabel = evt.type.split('.').slice(-2).join('.');

                    return (
                        <div
                            key={evt.id}
                            className="timeline-event"
                            onClick={() => handleEventClick(evt.node_id)}
                        >
                            <div className="timeline-time">
                                {time.toLocaleTimeString('en-US', {
                                    hour12: false,
                                    hour: '2-digit',
                                    minute: '2-digit',
                                    second: '2-digit',
                                })}
                                .{String(time.getMilliseconds()).padStart(3, '0')}
                            </div>

                            <div
                                className="timeline-icon"
                                style={{ color: getEventColor(evt.type) }}
                            >
                                {getEventIcon(evt.type)}
                            </div>

                            <div className="timeline-content">
                                <div className="timeline-label">
                                    {eventLabel}
                                    {evt.node_id && (
                                        <span className="timeline-node">: {evt.node_id}</span>
                                    )}
                                    {delta > 0 && (
                                        <span className="timeline-delta">[+{delta}ms]</span>
                                    )}
                                </div>

                                {evt.data && Object.keys(evt.data).length > 0 && (
                                    <>
                                        <button
                                            className="timeline-expand"
                                            onClick={(e) => {
                                                e.stopPropagation();
                                                toggleExpanded(evt.id);
                                            }}
                                        >
                                            {isExpanded ? '▼' : '▶'} Data
                                        </button>

                                        {isExpanded && (
                                            <pre className="timeline-data">
                                                {JSON.stringify(evt.data, null, 2)}
                                            </pre>
                                        )}
                                    </>
                                )}
                            </div>
                        </div>
                    );
                })}
            </div>
        </div>
    );
}
