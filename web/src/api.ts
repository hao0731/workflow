// API client for workflow engine backend

const API_BASE = 'http://localhost:8081/api';

export interface WorkflowNode {
    id: string;
    type: string;
    name: string;
}

export interface Connection {
    from_node: string;
    from_port?: string;
    to_node: string;
    to_port?: string;
}

export interface Workflow {
    id: string;
    nodes: WorkflowNode[];
    connections: Connection[];
}

export interface Execution {
    id: string;
    workflow_id: string;
    status: 'running' | 'completed' | 'failed';
    started_at: string;
}

export interface NodeEvent {
    id: string;
    type: string;
    node_id: string;
    timestamp: string;
}

export async function fetchWorkflows(): Promise<Workflow[]> {
    const response = await fetch(`${API_BASE}/workflows`);
    if (!response.ok) {
        throw new Error(`Failed to fetch workflows: ${response.statusText}`);
    }
    return response.json();
}

export async function fetchWorkflow(id: string): Promise<Workflow | null> {
    const response = await fetch(`${API_BASE}/workflows/${encodeURIComponent(id)}`);
    if (response.status === 404) {
        return null;
    }
    if (!response.ok) {
        throw new Error(`Failed to fetch workflow: ${response.statusText}`);
    }
    return response.json();
}

export async function fetchExecutions(workflowId: string): Promise<Execution[]> {
    const response = await fetch(`${API_BASE}/workflows/${encodeURIComponent(workflowId)}/executions`);
    if (!response.ok) {
        throw new Error(`Failed to fetch executions: ${response.statusText}`);
    }
    return response.json();
}

export async function fetchExecutionEvents(executionId: string): Promise<NodeEvent[]> {
    const response = await fetch(`${API_BASE}/executions/${encodeURIComponent(executionId)}/events`);
    if (!response.ok) {
        throw new Error(`Failed to fetch execution events: ${response.statusText}`);
    }
    return response.json();
}
