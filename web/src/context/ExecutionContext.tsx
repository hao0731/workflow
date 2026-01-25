import {
    createContext,
    useContext,
    useState,
    useEffect,
    useCallback,
    type ReactNode,
} from 'react';
import type { ExecutionEvent, StreamMessage, NodeStatus } from '../types';
import { fetchExecutionEvents, getStreamUrl } from '../api';

type ConnectionStatus = 'disconnected' | 'connecting' | 'connected';

interface ExecutionContextValue {
    events: ExecutionEvent[];
    nodeStatuses: Map<string, NodeStatus>;
    connectionStatus: ConnectionStatus;
    clearEvents: () => void;
}

const ExecutionContext = createContext<ExecutionContextValue | null>(null);

interface Props {
    executionId: string | null;
    children: ReactNode;
}

export function ExecutionProvider({ executionId, children }: Props) {
    const [events, setEvents] = useState<ExecutionEvent[]>([]);
    const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('disconnected');

    const clearEvents = useCallback(() => {
        setEvents([]);
    }, []);

    // Derive node statuses from events
    const nodeStatuses = new Map<string, NodeStatus>();
    events.forEach((evt) => {
        if (!evt.node_id) return;
        if (evt.type.includes('scheduled')) {
            nodeStatuses.set(evt.node_id, 'scheduled');
        } else if (evt.type.includes('started')) {
            nodeStatuses.set(evt.node_id, 'running');
        } else if (evt.type.includes('completed')) {
            nodeStatuses.set(evt.node_id, 'completed');
        } else if (evt.type.includes('failed')) {
            nodeStatuses.set(evt.node_id, 'failed');
        }
    });

    useEffect(() => {
        if (!executionId) {
            setEvents([]);
            setConnectionStatus('disconnected');
            return;
        }

        let ws: WebSocket | null = null;
        let reconnectTimeout: ReturnType<typeof setTimeout>;

        const connect = () => {
            setConnectionStatus('connecting');

            ws = new WebSocket(getStreamUrl(executionId));

            ws.onopen = () => {
                setConnectionStatus('connected');
            };

            ws.onmessage = (msg) => {
                try {
                    const data: StreamMessage = JSON.parse(msg.data);
                    if (data.type === 'event' && data.event) {
                        setEvents((prev) => {
                            // Avoid duplicates
                            if (prev.some((e) => e.id === data.event!.id)) {
                                return prev;
                            }
                            return [...prev, data.event!];
                        });
                    }
                } catch (err) {
                    console.error('Failed to parse WebSocket message:', err);
                }
            };

            ws.onclose = () => {
                setConnectionStatus('disconnected');
                // Reconnect after 2 seconds
                reconnectTimeout = setTimeout(connect, 2000);
            };

            ws.onerror = (err) => {
                console.error('WebSocket error:', err);
                ws?.close();
            };
        };

        // Load existing events first, then connect
        fetchExecutionEvents(executionId)
            .then((existingEvents) => {
                setEvents(existingEvents);
                connect();
            })
            .catch((err) => {
                console.error('Failed to load initial events:', err);
                connect();
            });

        return () => {
            clearTimeout(reconnectTimeout);
            ws?.close();
        };
    }, [executionId]);

    return (
        <ExecutionContext.Provider
            value={{
                events,
                nodeStatuses,
                connectionStatus,
                clearEvents,
            }}
        >
            {children}
        </ExecutionContext.Provider>
    );
}

export function useExecution() {
    const context = useContext(ExecutionContext);
    if (!context) {
        throw new Error('useExecution must be used within an ExecutionProvider');
    }
    return context;
}
