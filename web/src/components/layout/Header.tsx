import { useWorkflow } from '../../context/WorkflowContext';
import { useUI } from '../../context/UIContext';

export default function Header() {
    const {
        workflows,
        currentWorkflowId,
        setCurrentWorkflowId,
        executions,
        currentExecutionId,
        setCurrentExecutionId,
    } = useWorkflow();
    const { state, dispatch } = useUI();

    const isWorkflowView = state.viewMode === 'workflow';

    return (
        <header className="header">
            <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
                <h1>{isWorkflowView ? 'Workflow Dashboard' : 'Event Marketplace'}</h1>

                <div className="view-toggle">
                    <button
                        className={isWorkflowView ? 'active' : ''}
                        onClick={() => dispatch({ type: 'SET_VIEW_MODE', mode: 'workflow' })}
                    >
                        📊 Workflows
                    </button>
                    <button
                        className={!isWorkflowView ? 'active' : ''}
                        onClick={() => dispatch({ type: 'SET_VIEW_MODE', mode: 'marketplace' })}
                    >
                        🏪 Marketplace
                    </button>
                </div>
            </div>

            {isWorkflowView && (
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
                </div>
            )}
        </header>
    );
}
