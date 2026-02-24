# Workflow UI Design Document

**Date:** 2026-01-19
**Status:** Approved
**Primary Use Case:** Development/Debugging

---

## 1. Overview

A single-page workflow dashboard UI for developers to query, inspect, and debug workflows. Built on the existing React + TypeScript + Vite foundation with React Flow for visualization.

### Goals
- Display workflow DAG with proper auto-layout
- List and browse all workflows
- Deep inspection of workflow definitions, nodes, and connections
- Browse registered node types from the Node Registry
- Trace execution progress in real-time with event timeline

### Non-Goals
- Workflow editing/creation (read-only for now)
- User authentication (assumed handled elsewhere)
- Mobile responsiveness (desktop-first)

---

## 2. Architecture

### 2.1 Overall Layout

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Header: Logo | Workflow Selector | Execution Selector | Actions        │
├─────────┬───────────────────────────────────────────────┬───────────────┤
│         │                                               │               │
│  Left   │           Workflow Graph Canvas               │    Right      │
│  Panel  │           (React Flow + Dagre)                │    Panel      │
│ (280px) │                                               │   (400px)     │
│         │    ┌─────┐                                    │               │
│  - List │    │Start│──►[Process]──►[Join]──►[End]      │  - Node       │
│  - Tree │    └─────┘       │          ▲                 │    Inspector  │
│  - Info │                  ▼          │                 │  - Definition │
│         │              [Branch]───────┘                 │  - Registry   │
│         │                                               │               │
├─────────┴───────────────────────────────────────────────┴───────────────┤
│  Bottom Panel (200px): Execution Timeline (collapsible)                 │
│  [14:32:01 Started] → [14:32:02 Node A scheduled] → [14:32:03 ...]     │
└─────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Panel Behavior
- **Left Panel** (280px): Workflow list, node tree view, quick stats
- **Right Panel** (400px): Context-sensitive details for selected item
- **Bottom Panel** (200px): Execution timeline, expandable for payloads
- All panels are collapsible via toggle buttons
- Graph canvas fills remaining space and auto-fits on resize

---

## 3. Component Structure

```
src/
├── components/
│   ├── layout/
│   │   ├── Header.tsx           # Workflow/execution selectors, actions
│   │   ├── LeftPanel.tsx        # Collapsible container
│   │   ├── RightPanel.tsx       # Collapsible container
│   │   └── BottomPanel.tsx      # Collapsible timeline container
│   │
│   ├── graph/
│   │   ├── WorkflowCanvas.tsx   # React Flow wrapper with Dagre layout
│   │   ├── CustomNode.tsx       # Node with status, type icon, badges
│   │   └── CustomEdge.tsx       # Edge with port labels, animation
│   │
│   ├── panels/
│   │   ├── WorkflowList.tsx     # List all workflows with search/filter
│   │   ├── NodeTree.tsx         # Hierarchical view of workflow nodes
│   │   ├── NodeInspector.tsx    # Selected node details + parameters
│   │   ├── WorkflowDefinition.tsx  # YAML source + metadata
│   │   ├── NodeRegistryBrowser.tsx # Browse registered node types
│   │   └── ExecutionTimeline.tsx   # Event log with data inspection
│   │
│   └── common/
│       ├── JsonViewer.tsx       # Collapsible JSON tree for payloads
│       ├── StatusBadge.tsx      # Reusable status indicator
│       └── SearchInput.tsx      # Filtered search component
│
├── hooks/
│   ├── useExecutionStream.ts    # WebSocket connection for live events
│   ├── useWorkflow.ts           # Fetch and cache workflow data
│   └── useDagreLayout.ts        # Dagre layout calculation
│
├── context/
│   ├── WorkflowContext.tsx      # Current workflow, nodes, connections
│   ├── ExecutionContext.tsx     # Current execution, events, WS connection
│   └── UIContext.tsx            # Panel visibility, selections, preferences
│
├── api.ts                       # API client (enhanced)
└── App.tsx                      # Main layout composition
```

### 3.1 State Management

React Context + useReducer (no external library needed):

- **WorkflowContext**: Current workflow, nodes, connections
- **ExecutionContext**: Current execution, events, WebSocket connection
- **UIContext**: Panel visibility, selected node/event, view preferences

---

## 4. Workflow Graph Visualization

### 4.1 Dagre Auto-Layout

```typescript
import Dagre from '@dagrejs/dagre';

const layoutWorkflow = (nodes: Node[], edges: Edge[]): LayoutResult => {
  const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
  g.setGraph({
    rankdir: 'LR',      // Left-to-right flow
    nodesep: 80,        // Horizontal spacing
    ranksep: 120,       // Vertical spacing between ranks
    marginx: 40,
    marginy: 40
  });

  nodes.forEach(node => {
    g.setNode(node.id, { width: 180, height: 60 });
  });

  edges.forEach(edge => {
    g.setEdge(edge.source, edge.target);
  });

  Dagre.layout(g);

  return {
    nodes: nodes.map(node => {
      const pos = g.node(node.id);
      return { ...node, position: { x: pos.x - 90, y: pos.y - 30 } };
    }),
    edges
  };
};
```

### 4.2 Custom Node Design

```
┌──────────────────────────────────┐
│ [Icon]  Node Name                │  ← Header with type indicator
│──────────────────────────────────│
│ http-request@v1                  │  ← Node type (dimmed)
│                                  │
│ ● success  ○ failure             │  ← Output ports (if multiple)
└──────────────────────────────────┘
```

### 4.3 Status Colors

| Status | Color | Effect |
|--------|-------|--------|
| Pending | `#6b7280` (gray) | None |
| Scheduled | `#3b82f6` (blue) | Pulsing glow |
| Running | `#3b82f6` (blue) | Animated glow |
| Completed | `#22c55e` (green) | None |
| Failed | `#ef4444` (red) | None |

### 4.4 Edge Features
- Animated dashes during execution
- Port labels shown on hover
- Highlight path when node selected

---

## 5. Detail Panels

### 5.1 Node Inspector (Right Panel)

Shown when a node is selected in the graph:

```
┌─────────────────────────────────────┐
│ NODE INSPECTOR              [×]     │
├─────────────────────────────────────┤
│ ▼ Basic Info                        │
│   ID: process-payment               │
│   Type: payment-processor@v1        │
│   Name: Process Payment             │
│   Status: ● Completed               │
├─────────────────────────────────────┤
│ ▼ Parameters                        │
│   { "retry_count": 3, "timeout": "30s" }
├─────────────────────────────────────┤
│ ▼ Connections                       │
│   Inputs from: validate-order       │
│   Outputs to: send-confirmation     │
├─────────────────────────────────────┤
│ ▼ Registered Node Type              │
│   [View in Registry →]              │
│   Output Ports: success, failure    │
└─────────────────────────────────────┘
```

### 5.2 Workflow Definition (Right Panel Tab)

```
┌─────────────────────────────────────┐
│ WORKFLOW DEFINITION         [×]     │
├─────────────────────────────────────┤
│ ▼ Metadata                          │
│   ID: order-processing              │
│   Name: Order Processing Pipeline   │
│   Version: 1.2.0                    │
│   Description: Handles new orders   │
├─────────────────────────────────────┤
│ ▼ YAML Source          [Copy] [Raw] │
│   id: order-processing              │
│   name: "Order Processing..."       │
│   nodes:                            │
│     - id: start                     │
│       type: StartNode               │
├─────────────────────────────────────┤
│ ▼ Statistics                        │
│   Nodes: 6 | Connections: 7         │
│   Has Event Trigger: Yes            │
└─────────────────────────────────────┘
```

### 5.3 Node Registry Browser (Left Panel Tab)

```
┌─────────────────────────────────┐
│ [Workflows] [Registry]          │
├─────────────────────────────────┤
│ 🔍 Search nodes...              │
├─────────────────────────────────┤
│ ▼ Actions (4)                   │
│   ├─ http-request@v1      ● 3   │
│   ├─ send-email@v1        ● 1   │
│   ├─ data-transform@v1    ○ 0   │
│   └─ slack-notify@v1      ● 2   │
│ ▼ Conditions (2)                │
│   ├─ condition-check@v1   ● 5   │
│   └─ rate-limiter@v1      ● 1   │
│ ▼ System (3)                    │
│   ├─ StartNode            ━     │
│   ├─ JoinNode             ━     │
│   └─ PublishEvent         ━     │
└─────────────────────────────────┘
```

---

## 6. Execution Timeline (Bottom Panel)

### 6.1 Layout

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ EXECUTION TIMELINE                                    [▼ Collapse] [Clear] │
├─────────────────────────────────────────────────────────────────────────────┤
│ ● Live    Execution: exec-abc123    Started: 14:32:01    Duration: 2.3s    │
├─────────────────────────────────────────────────────────────────────────────┤
│  14:32:01.000  ●──── Execution Started                                     │
│                      workflow_id: order-processing                          │
│                                                                             │
│  14:32:01.012  ●──── Node Scheduled: start                                 │
│                      ▶ Input Data  { "order_id": "123", "amount": 99 }     │
│                                                                             │
│  14:32:01.045  ●──── Node Completed: start                    [+33ms]      │
│                      Port: default                                          │
│                      ▶ Output Data { "order_id": "123", "validated": true } │
│                                                                             │
│  14:32:01.240  ○──── Node Scheduled: process-payment          ← Current    │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.2 Features
- Click event row to highlight corresponding node in graph
- Expandable JSON viewers for input/output data
- Time deltas between events (`[+184ms]`)
- Auto-scroll to latest event (toggleable)
- Filter by event type: All | Scheduled | Completed | Failed
- Search within payload data

### 6.3 Visual Indicators
- `●` Filled circle: Completed event
- `○` Empty circle: In-progress
- Red circle: Failed events (with error message)

---

## 7. Real-Time Streaming

### 7.1 Architecture

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Browser   │◄──WSS──►│  API Server │◄──NATS──│   Engine    │
│  (React)    │         │   (Echo)    │         │(Orchestrator)│
└─────────────┘         └─────────────┘         └─────────────┘
```

### 7.2 WebSocket Endpoint

```go
// GET /api/executions/:id/stream
// Upgrades to WebSocket, streams events for this execution

type StreamMessage struct {
    Type      string    `json:"type"`      // "event" | "heartbeat" | "error"
    Event     *Event    `json:"event,omitempty"`
    Timestamp time.Time `json:"timestamp"`
}
```

### 7.3 Frontend Hook

```typescript
function useExecutionStream(executionId: string | null) {
  const [events, setEvents] = useState<ExecutionEvent[]>([]);
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected'>();

  useEffect(() => {
    if (!executionId) return;

    const ws = new WebSocket(`ws://localhost:8081/api/executions/${executionId}/stream`);

    ws.onmessage = (msg) => {
      const data = JSON.parse(msg.data);
      if (data.type === 'event') {
        setEvents(prev => [...prev, data.event]);
      }
    };

    return () => ws.close();
  }, [executionId]);

  return { events, status };
}
```

### 7.4 Fallback
If WebSocket unavailable, fall back to polling every 2 seconds:
`GET /api/executions/:id/events?since=<timestamp>`

---

## 8. Backend API Additions

### 8.1 New Endpoints

```
# Workflow Definition (YAML source)
GET  /api/workflows/:id/source   # Return original YAML + metadata

# Executions
GET  /api/workflows/:id/executions          # List executions for workflow
GET  /api/executions/:id                    # Get execution details
GET  /api/executions/:id/events             # Get all events (with ?since=)
WS   /api/executions/:id/stream             # WebSocket stream
POST /api/workflows/:id/execute             # Trigger new execution

# Node Registry (enhanced)
GET  /nodes/:fullType/usage                 # Workflows using this node type
```

### 8.2 Enhanced Response Models

```go
type WorkflowDetailResponse struct {
    ID          string               `json:"id"`
    Name        string               `json:"name"`
    Description string               `json:"description"`
    Version     string               `json:"version"`
    Nodes       []NodeResponse       `json:"nodes"`
    Connections []ConnectionResponse `json:"connections"`
    Stats       WorkflowStats        `json:"stats"`
    CreatedAt   time.Time            `json:"created_at"`
}

type WorkflowStats struct {
    NodeCount       int    `json:"node_count"`
    ConnectionCount int    `json:"connection_count"`
    HasEventTrigger bool   `json:"has_event_trigger"`
    TriggerEvent    string `json:"trigger_event,omitempty"`
}

type ExecutionResponse struct {
    ID         string    `json:"id"`
    WorkflowID string    `json:"workflow_id"`
    Status     string    `json:"status"`
    StartedAt  time.Time `json:"started_at"`
    EndedAt    *time.Time `json:"ended_at,omitempty"`
    Duration   *float64  `json:"duration_ms,omitempty"`
}

type ExecutionEventResponse struct {
    ID        string         `json:"id"`
    Type      string         `json:"type"`
    NodeID    string         `json:"node_id,omitempty"`
    Data      map[string]any `json:"data,omitempty"`
    Timestamp time.Time      `json:"timestamp"`
}
```

---

## 9. Error Handling

### 9.1 UI Error States

| Scenario | Handling |
|----------|----------|
| Workflow not found | Empty state with "Workflow not found" message |
| API connection failed | Toast notification + retry button in header |
| WebSocket disconnected | "Reconnecting..." badge, auto-retry with backoff |
| No executions yet | Empty state: "No executions. Click 'Run' to start." |
| Node type not in registry | Warning badge on node: "Unknown type" |
| Large payload data | Truncate JSON viewer with "Show full" option |

### 9.2 Loading States
- Spinner in graph area while loading workflow
- Skeleton placeholders in panels

### 9.3 Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Esc` | Close right panel / deselect node |
| `1` | Toggle left panel |
| `2` | Toggle right panel |
| `3` | Toggle bottom panel |
| `f` | Fit graph to view |
| `r` | Refresh current data |

---

## 10. Technology Stack

### 10.1 Frontend
- **Framework:** React 19 + TypeScript + Vite (existing)
- **Graph:** @xyflow/react + @dagrejs/dagre
- **Styling:** CSS (existing dark theme)
- **State:** React Context + useReducer
- **Real-time:** Native WebSocket API

### 10.2 New Dependencies

```json
{
  "@dagrejs/dagre": "^1.1.2"
}
```

### 10.3 Backend
- **Framework:** Echo v4 (existing)
- **WebSocket:** gorilla/websocket or Echo's built-in
- **Event Source:** NATS JetStream subscription

---

## 11. Implementation Order

1. **Backend API additions** - Executions, streaming, source endpoint
2. **Dagre layout integration** - Replace vertical list with proper DAG
3. **Enhanced custom nodes** - Status indicators, port display
4. **Left panel** - Workflow list + node registry browser
5. **Right panel** - Node inspector + workflow definition
6. **Bottom panel** - Execution timeline
7. **WebSocket streaming** - Real-time event updates
8. **Polish** - Keyboard shortcuts, error states, loading skeletons

---

## 12. File Changes Summary

### Backend (~6 files)
- `internal/api/execution_handler.go` - New execution endpoints
- `internal/api/stream_handler.go` - WebSocket streaming
- `internal/api/workflow_handler.go` - Enhanced responses
- `internal/api/models.go` - New response types
- `internal/dsl/api/handler.go` - Add source endpoint
- `internal/registry/handlers.go` - Add usage endpoint

### Frontend (~20 files)
- 6 layout components
- 3 graph components
- 6 panel components
- 3 common components
- 3 hooks
- 3 context providers
- Enhanced API client
- Updated App.tsx

---

## Appendix: Existing Code Reference

### Current Frontend Structure
```
web/src/
├── App.tsx              # Main component (to be refactored)
├── App.css              # Styles (dark theme)
├── api.ts               # API client (to be enhanced)
└── components/
    ├── WorkflowSelector.tsx
    ├── ExecutionList.tsx
    └── NodeDetails.tsx
```

### Current Backend APIs
```
GET  /api/workflows              # List workflows
GET  /api/workflows/:id          # Get workflow
POST /api/workflows              # Create workflow
PUT  /api/workflows/:id          # Update workflow
DELETE /api/workflows/:id        # Delete workflow

GET  /nodes                      # List node types
GET  /nodes/:fullType            # Get node type
POST /nodes                      # Register node type
POST /nodes/:fullType/connect    # Get NATS credentials
POST /nodes/:fullType/health     # Worker heartbeat
```
