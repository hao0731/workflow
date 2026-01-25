import { createContext, useContext, useState, useEffect, type ReactNode } from 'react';
import type { Workflow, Execution } from '../types';
import { fetchWorkflow, fetchWorkflows, fetchExecutions } from '../api';

interface WorkflowState {
    workflows: Workflow[];
    currentWorkflow: Workflow | null;
    currentWorkflowId: string | null;
    executions: Execution[];
    currentExecutionId: string | null;
    loading: boolean;
    error: string | null;
}

interface WorkflowContextValue extends WorkflowState {
    setCurrentWorkflowId: (id: string | null) => void;
    setCurrentExecutionId: (id: string | null) => void;
    refreshWorkflows: () => Promise<void>;
    refreshExecutions: () => Promise<void>;
}

const WorkflowContext = createContext<WorkflowContextValue | null>(null);

export function WorkflowProvider({ children }: { children: ReactNode }) {
    const [workflows, setWorkflows] = useState<Workflow[]>([]);
    const [currentWorkflow, setCurrentWorkflow] = useState<Workflow | null>(null);
    const [currentWorkflowId, setCurrentWorkflowId] = useState<string | null>(null);
    const [executions, setExecutions] = useState<Execution[]>([]);
    const [currentExecutionId, setCurrentExecutionId] = useState<string | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    // Load workflows list
    const refreshWorkflows = async () => {
        try {
            setLoading(true);
            setError(null);
            const wfs = await fetchWorkflows();
            setWorkflows(wfs);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load workflows');
        } finally {
            setLoading(false);
        }
    };

    // Load executions for current workflow
    const refreshExecutions = async () => {
        if (!currentWorkflowId) {
            setExecutions([]);
            return;
        }
        try {
            const execs = await fetchExecutions(currentWorkflowId);
            setExecutions(execs);
        } catch (err) {
            console.error('Failed to load executions:', err);
            setExecutions([]);
        }
    };

    // Load workflow details when ID changes
    useEffect(() => {
        if (!currentWorkflowId) {
            setCurrentWorkflow(null);
            setExecutions([]);
            setCurrentExecutionId(null);
            return;
        }

        const loadWorkflow = async () => {
            try {
                setLoading(true);
                const wf = await fetchWorkflow(currentWorkflowId);
                setCurrentWorkflow(wf);
                await refreshExecutions();
            } catch (err) {
                setError(err instanceof Error ? err.message : 'Failed to load workflow');
            } finally {
                setLoading(false);
            }
        };

        loadWorkflow();
    }, [currentWorkflowId]);

    // Initial load
    useEffect(() => {
        refreshWorkflows();
    }, []);

    // Auto-select first workflow
    useEffect(() => {
        if (workflows.length > 0 && !currentWorkflowId) {
            setCurrentWorkflowId(workflows[0].id);
        }
    }, [workflows, currentWorkflowId]);

    return (
        <WorkflowContext.Provider
            value={{
                workflows,
                currentWorkflow,
                currentWorkflowId,
                executions,
                currentExecutionId,
                loading,
                error,
                setCurrentWorkflowId,
                setCurrentExecutionId,
                refreshWorkflows,
                refreshExecutions,
            }}
        >
            {children}
        </WorkflowContext.Provider>
    );
}

export function useWorkflow() {
    const context = useContext(WorkflowContext);
    if (!context) {
        throw new Error('useWorkflow must be used within a WorkflowProvider');
    }
    return context;
}
