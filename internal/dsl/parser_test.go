package dsl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: Parse valid YAML returns WorkflowDefinition (Happy Path)
func TestYAMLParser_Parse_ReturnsWorkflowDefinition(t *testing.T) {
	input := []byte(`
id: test-workflow
name: Test Workflow
description: A test workflow
version: "1.0.0"
nodes:
  - id: start
    type: StartNode
    name: Start
  - id: action
    type: http-request@v1
    name: Make Request
    parameters:
      url: "https://example.com"
      method: POST
connections:
  - from: start
    to: action
`)

	parser := NewYAMLParser()
	def, err := parser.Parse(input)

	require.NoError(t, err)
	assert.Equal(t, "test-workflow", def.ID)
	assert.Equal(t, "Test Workflow", def.Name)
	assert.Equal(t, "A test workflow", def.Description)
	assert.Equal(t, "1.0.0", def.Version)
	assert.Len(t, def.Nodes, 2)
	assert.Len(t, def.Connections, 1)

	// Verify first node
	assert.Equal(t, "start", def.Nodes[0].ID)
	assert.Equal(t, "StartNode", def.Nodes[0].Type)

	// Verify second node with parameters
	assert.Equal(t, "action", def.Nodes[1].ID)
	assert.Equal(t, "http-request@v1", def.Nodes[1].Type)
	assert.Equal(t, "https://example.com", def.Nodes[1].Parameters["url"])

	// Verify connection
	assert.Equal(t, "start", def.Connections[0].From)
	assert.Equal(t, "action", def.Connections[0].To)
}

// Test: Parse YAML with event trigger
func TestYAMLParser_Parse_ParsesEventTrigger(t *testing.T) {
	input := []byte(`
id: event-workflow
nodes:
  - id: start
    type: StartNode
    trigger:
      type: event
      criteria:
        event_name: order_created
        domain: ecommerce
      input_map:
        order_id: "{{.event.order_id}}"
connections: []
`)

	parser := NewYAMLParser()
	def, err := parser.Parse(input)

	require.NoError(t, err)
	require.NotNil(t, def.Nodes[0].Trigger)
	assert.Equal(t, "event", def.Nodes[0].Trigger.Type)
	assert.Equal(t, "order_created", def.Nodes[0].Trigger.Criteria["event_name"])
	assert.Equal(t, "ecommerce", def.Nodes[0].Trigger.Criteria["domain"])
	assert.Equal(t, "{{.event.order_id}}", def.Nodes[0].Trigger.InputMap["order_id"])
}

// Test: Parse YAML with events section
func TestYAMLParser_Parse_ParsesEventsSection(t *testing.T) {
	input := []byte(`
id: publisher-workflow
nodes:
  - id: start
    type: StartNode
connections: []
events:
  - name: order_created
    domain: ecommerce
    description: Fired when order is created
    schema:
      type: object
`)

	parser := NewYAMLParser()
	def, err := parser.Parse(input)

	require.NoError(t, err)
	require.Len(t, def.Events, 1)
	assert.Equal(t, "order_created", def.Events[0].Name)
	assert.Equal(t, "ecommerce", def.Events[0].Domain)
}

// Test: Parse returns error for invalid YAML (Exception case)
func TestYAMLParser_Parse_ReturnsErrorForInvalidYAML(t *testing.T) {
	input := []byte(`
id: test
nodes:
  - id: start
    invalid yaml here {{ broken
`)

	parser := NewYAMLParser()
	_, err := parser.Parse(input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

// Test: Parse returns error for empty input (Zero case)
func TestYAMLParser_Parse_ReturnsErrorForEmptyInput(t *testing.T) {
	parser := NewYAMLParser()
	_, err := parser.Parse([]byte{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty input")
}

// Test: Parse YAML with connection ports
func TestYAMLParser_Parse_ParsesConnectionPorts(t *testing.T) {
	input := []byte(`
id: conditional-workflow
nodes:
  - id: start
    type: StartNode
  - id: check
    type: condition-check@v1
  - id: on-true
    type: action@v1
  - id: on-false
    type: action@v1
connections:
  - from: start
    to: check
  - from: check
    from_port: "true"
    to: on-true
  - from: check
    from_port: "false"
    to: on-false
`)

	parser := NewYAMLParser()
	def, err := parser.Parse(input)

	require.NoError(t, err)
	assert.Len(t, def.Connections, 3)
	assert.Equal(t, "true", def.Connections[1].FromPort)
	assert.Equal(t, "false", def.Connections[2].FromPort)
}

// Test: Parse YAML with JoinNode to_port
func TestYAMLParser_Parse_ParsesJoinNodeToPorts(t *testing.T) {
	input := []byte(`
id: join-workflow
nodes:
  - id: start
    type: StartNode
  - id: fetch-user
    type: user-service@v1
  - id: fetch-orders
    type: order-service@v1
  - id: merge
    type: JoinNode
    parameters:
      operator: all
      inputs:
        - user
        - orders
connections:
  - from: start
    to: fetch-user
  - from: start
    to: fetch-orders
  - from: fetch-user
    to: merge
    to_port: user
  - from: fetch-orders
    to: merge
    to_port: orders
`)

	parser := NewYAMLParser()
	def, err := parser.Parse(input)

	require.NoError(t, err)

	// Find merge connection
	var userConn, ordersConn *ConnectionDef
	for i := range def.Connections {
		if def.Connections[i].To == "merge" {
			if def.Connections[i].ToPort == "user" {
				userConn = &def.Connections[i]
			} else if def.Connections[i].ToPort == "orders" {
				ordersConn = &def.Connections[i]
			}
		}
	}

	require.NotNil(t, userConn)
	require.NotNil(t, ordersConn)
	assert.Equal(t, "fetch-user", userConn.From)
	assert.Equal(t, "fetch-orders", ordersConn.From)
}
