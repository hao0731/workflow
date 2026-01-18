package dsl

import (
	"testing"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: Convert simple workflow (Happy Path)
func TestDefaultConverter_Convert_SimpleWorkflow(t *testing.T) {
	def := &WorkflowDefinition{
		ID:          "test-workflow",
		Name:        "Test Workflow",
		Description: "A test workflow",
		Version:     "1.0.0",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode", Name: "Start"},
			{ID: "action", Type: "http-request@v1", Name: "Make Request", Parameters: map[string]any{"url": "https://example.com"}},
		},
		Connections: []ConnectionDef{
			{From: "start", To: "action"},
		},
	}

	converter := NewDefaultConverter()
	wf, err := converter.Convert(def)

	require.NoError(t, err)
	assert.Equal(t, "test-workflow", wf.ID)
	assert.Len(t, wf.Nodes, 2)
	assert.Len(t, wf.Connections, 1)

	// Verify StartNode
	startNode := wf.GetNode("start")
	require.NotNil(t, startNode)
	assert.Equal(t, engine.StartNode, startNode.Type)
	assert.Equal(t, "Start", startNode.Name)

	// Verify action node
	actionNode := wf.GetNode("action")
	require.NotNil(t, actionNode)
	assert.Equal(t, engine.ActionNode, actionNode.Type) // Third-party nodes map to ActionNode
	assert.Equal(t, "https://example.com", actionNode.Parameters["url"])
	assert.Equal(t, "http-request@v1", actionNode.Parameters["full_type"]) // Original type stored

	// Verify connection
	assert.Equal(t, "start", wf.Connections[0].FromNode)
	assert.Equal(t, "action", wf.Connections[0].ToNode)
}

// Test: Convert JoinNode
func TestDefaultConverter_Convert_JoinNode(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "join-workflow",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
			{ID: "merge", Type: "JoinNode", Parameters: map[string]any{
				"operator": "all",
				"inputs":   []any{"user", "orders"},
			}},
		},
		Connections: []ConnectionDef{
			{From: "start", To: "merge", ToPort: "user"},
		},
	}

	converter := NewDefaultConverter()
	wf, err := converter.Convert(def)

	require.NoError(t, err)

	joinNode := wf.GetNode("merge")
	require.NotNil(t, joinNode)
	assert.Equal(t, engine.JoinNode, joinNode.Type)

	op, inputs := joinNode.JoinConfig()
	assert.Equal(t, engine.JoinAll, op)
	assert.ElementsMatch(t, []string{"user", "orders"}, inputs)
}

// Test: Convert PublishEvent node
func TestDefaultConverter_Convert_PublishEventNode(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "publisher-workflow",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
			{ID: "publish", Type: "PublishEvent", Parameters: map[string]any{
				"event_name": "order_created",
				"domain":     "ecommerce",
			}},
		},
		Connections: []ConnectionDef{
			{From: "start", To: "publish"},
		},
	}

	converter := NewDefaultConverter()
	wf, err := converter.Convert(def)

	require.NoError(t, err)

	publishNode := wf.GetNode("publish")
	require.NotNil(t, publishNode)
	assert.Equal(t, engine.PublishEvent, publishNode.Type)
	assert.Equal(t, "order_created", publishNode.Parameters["event_name"])
}

// Test: Convert StartNode with event trigger
func TestDefaultConverter_Convert_EventTrigger(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "event-workflow",
		Nodes: []NodeDefinition{
			{
				ID:   "start",
				Type: "StartNode",
				Trigger: &TriggerDef{
					Type: "event",
					Criteria: map[string]any{
						"event_name": "order_created",
						"domain":     "ecommerce",
					},
					InputMap: map[string]any{
						"order_id": "{{.event.order_id}}",
					},
				},
			},
		},
		Connections: []ConnectionDef{},
	}

	converter := NewDefaultConverter()
	wf, err := converter.Convert(def)

	require.NoError(t, err)

	startNode := wf.GetStartNode()
	require.NotNil(t, startNode)
	require.NotNil(t, startNode.Trigger)
	assert.Equal(t, engine.TriggerEvent, startNode.Trigger.Type)
	assert.Equal(t, "order_created", startNode.Trigger.Criteria["event_name"])
	assert.Equal(t, "{{.event.order_id}}", startNode.Trigger.InputMap["order_id"])
}

// Test: Convert connection ports
func TestDefaultConverter_Convert_ConnectionPorts(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "conditional-workflow",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
			{ID: "check", Type: "condition@v1"},
			{ID: "on-true", Type: "action@v1"},
		},
		Connections: []ConnectionDef{
			{From: "start", To: "check"},
			{From: "check", FromPort: "true", To: "on-true"},
		},
	}

	converter := NewDefaultConverter()
	wf, err := converter.Convert(def)

	require.NoError(t, err)

	// Find connection with port
	var portConn *engine.Connection
	for i := range wf.Connections {
		if wf.Connections[i].FromPort == "true" {
			portConn = &wf.Connections[i]
			break
		}
	}

	require.NotNil(t, portConn)
	assert.Equal(t, "check", portConn.FromNode)
	assert.Equal(t, "on-true", portConn.ToNode)
}

// Test: Convert nil definition returns error
func TestDefaultConverter_Convert_NilDefinitionReturnsError(t *testing.T) {
	converter := NewDefaultConverter()
	_, err := converter.Convert(nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "definition is nil")
}
