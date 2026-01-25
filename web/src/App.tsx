import { UIProvider, useUI } from './context/UIContext';
import { WorkflowProvider, useWorkflow } from './context/WorkflowContext';
import { ExecutionProvider } from './context/ExecutionContext';

import Header from './components/layout/Header';
import LeftPanel from './components/layout/LeftPanel';
import RightPanel from './components/layout/RightPanel';
import BottomPanel from './components/layout/BottomPanel';
import WorkflowCanvas from './components/graph/WorkflowCanvas';
import WorkflowList from './components/panels/WorkflowList';
import NodeInspector from './components/panels/NodeInspector';
import ExecutionTimeline from './components/panels/ExecutionTimeline';

import './App.css';

function AppContent() {
  const { currentExecutionId } = useWorkflow();
  const { state } = useUI();

  return (
    <ExecutionProvider executionId={currentExecutionId}>
      <div className="app">
        <Header />

        <div className="main">
          <LeftPanel>
            <WorkflowList />
          </LeftPanel>

          <div className="graph-container">
            <WorkflowCanvas />
          </div>

          <RightPanel>
            {state.rightPanelView === 'inspector' && <NodeInspector />}
          </RightPanel>
        </div>

        <BottomPanel>
          <ExecutionTimeline />
        </BottomPanel>
      </div>
    </ExecutionProvider>
  );
}

export default function App() {
  return (
    <UIProvider>
      <WorkflowProvider>
        <AppContent />
      </WorkflowProvider>
    </UIProvider>
  );
}
