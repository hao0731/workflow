import { useWorkflow } from '../../context/WorkflowContext';
import { useExecution } from '../../context/ExecutionContext';

export default function Header() {
    const {
        workflows,
        currentWorkflowId,
        setCurrentWorkflowId,
        executions,
        currentExecutionId,
        setCurrentExecutionId,
    } = useWorkflow();
    const { connectionStatus } = useExecution();

    return (
        <header className="header">
            <h1>Workflow Dashboard</h1>

            <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
                <div className="workflow-selector">
                    <label>Workflow:</label>
                    <select
                        value={currentWorkflowId || ''}
                        onChange={(e) => setCurrentWorkflowId(e.target.value || null)}
                    >
                        <option value="">Select workflow...</option>
                        {workflows.map((wf) => (
                            <option key={wf.id} value={wf.id}>
                                {wf.id}
                            </option>
                        ))}
                    </select>
                </div>

                <div className="workflow-selector">
                    <label>Execution:</label>
                    <select
                        value={currentExecutionId || ''}
                        onChange={(e) => setCurrentExecutionId(e.target.value || null)}
                        disabled={executions.length === 0}
                    >
                        <option value="">Select execution...</option>
                        {executions.map((exec) => (
                            <option key={exec.id} value={exec.id}>
                                {exec.id.slice(0, 12)}... ({exec.status})
                            </option>
                        ))}
                    </select>
                </div>

                {currentExecutionId && (
                    <span
                        style={{
                            fontSize: '0.75rem',
                            padding: '4px 8px',
                            borderRadius: '4px',
                            background:
                                connectionStatus === 'connected'
                                    ? '#166534'
                                    : connectionStatus === 'connecting'
                                        ? '#1e40af'
                                        : '#991b1b',
                            color: '#fff',
                        }}
                    >
                        {connectionStatus === 'connected'
                            ? 'Live'
                            : connectionStatus === 'connecting'
                                ? 'Connecting...'
                                : 'Disconnected'}
                    </span>
                )}
            </div>
        </header>
    );
}
