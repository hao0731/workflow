// Workflow types
export interface WorkflowNode {
  id: string;
  type: string;
  name: string;
  parameters?: Record<string, unknown>;
}

export interface Connection {
  from_node: string;
  from_port?: string;
  to_node: string;
  to_port?: string;
}

export interface Workflow {
  id: string;
  name?: string;
  description?: string;
  version?: string;
  nodes: WorkflowNode[];
  connections: Connection[];
}

export interface WorkflowStats {
  node_count: number;
  connection_count: number;
  has_event_trigger: boolean;
  trigger_event?: string;
}

export interface WorkflowWithStats extends Workflow {
  stats: WorkflowStats;
}

// Execution types
export type ExecutionStatus = 'running' | 'completed' | 'failed';
export type NodeStatus = 'pending' | 'scheduled' | 'running' | 'completed' | 'failed';

export interface Execution {
  id: string;
  workflow_id: string;
  status: ExecutionStatus;
  started_at: string;
  ended_at?: string;
  duration_ms?: number;
}

export interface ExecutionEvent {
  id: string;
  type: string;
  node_id?: string;
  data?: Record<string, unknown>;
  timestamp: string;
}

export interface StreamMessage {
  type: 'event' | 'heartbeat' | 'error';
  event?: ExecutionEvent;
  timestamp: string;
  error?: string;
}

// Node Registry types
export interface NodeRegistration {
  full_type: string;
  display_name: string;
  description?: string;
  output_ports: string[];
  enabled: boolean;
  worker_count: number;
  last_heartbeat?: string;
}

// UI State types
export interface PanelState {
  left: boolean;
  right: boolean;
  bottom: boolean;
}

export type RightPanelView = 'inspector' | 'definition' | 'registry';
export type LeftPanelView = 'workflows' | 'registry';
