import type {
    Workflow,
    WorkflowWithStats,
    Execution,
    ExecutionEvent,
    NodeRegistration,
    DomainGroup,
    EventDefinition,
} from './types';

const API_BASE = 'http://localhost:8083/api';
const REGISTRY_BASE = 'http://localhost:8082';

// Workflow APIs
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

export async function fetchWorkflowSource(id: string): Promise<WorkflowWithStats | null> {
    const response = await fetch(`${API_BASE}/workflows/${encodeURIComponent(id)}/source`);
    if (response.status === 404) {
        return null;
    }
    if (!response.ok) {
        throw new Error(`Failed to fetch workflow source: ${response.statusText}`);
    }
    return response.json();
}

// Execution APIs
export async function fetchExecutions(workflowId: string): Promise<Execution[]> {
    const response = await fetch(`${API_BASE}/workflows/${encodeURIComponent(workflowId)}/executions`);
    if (!response.ok) {
        throw new Error(`Failed to fetch executions: ${response.statusText}`);
    }
    return response.json();
}

export async function fetchExecution(executionId: string): Promise<Execution | null> {
    const response = await fetch(`${API_BASE}/executions/${encodeURIComponent(executionId)}`);
    if (response.status === 404) {
        return null;
    }
    if (!response.ok) {
        throw new Error(`Failed to fetch execution: ${response.statusText}`);
    }
    return response.json();
}

export async function fetchExecutionEvents(
    executionId: string,
    since?: string
): Promise<ExecutionEvent[]> {
    let url = `${API_BASE}/executions/${encodeURIComponent(executionId)}/events`;
    if (since) {
        url += `?since=${encodeURIComponent(since)}`;
    }
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(`Failed to fetch execution events: ${response.statusText}`);
    }
    return response.json();
}

// Node Registry APIs
export async function fetchNodeTypes(): Promise<NodeRegistration[]> {
    const response = await fetch(`${REGISTRY_BASE}/nodes`);
    if (!response.ok) {
        throw new Error(`Failed to fetch node types: ${response.statusText}`);
    }
    return response.json();
}

export async function fetchNodeType(fullType: string): Promise<NodeRegistration | null> {
    const response = await fetch(`${REGISTRY_BASE}/nodes/${encodeURIComponent(fullType)}`);
    if (response.status === 404) {
        return null;
    }
    if (!response.ok) {
        throw new Error(`Failed to fetch node type: ${response.statusText}`);
    }
    return response.json();
}

// WebSocket stream URL builder
export function getStreamUrl(executionId: string): string {
    return `ws://localhost:8083/api/executions/${encodeURIComponent(executionId)}/stream`;
}

// Marketplace APIs
export async function fetchMarketplaceEvents(domain?: string): Promise<DomainGroup[]> {
    let url = `${API_BASE}/events`;
    if (domain) {
        url += `?domain=${encodeURIComponent(domain)}`;
    }
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(`Failed to fetch marketplace events: ${response.statusText}`);
    }
    return response.json();
}

export async function fetchMarketplaceEvent(
    domain: string,
    name: string
): Promise<EventDefinition | null> {
    const response = await fetch(
        `${API_BASE}/events/${encodeURIComponent(domain)}/${encodeURIComponent(name)}`
    );
    if (response.status === 404) {
        return null;
    }
    if (!response.ok) {
        throw new Error(`Failed to fetch marketplace event: ${response.statusText}`);
    }
    return response.json();
}
