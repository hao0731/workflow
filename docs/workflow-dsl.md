# Workflow YAML DSL Specification

This document defines a YAML-based Domain Specific Language (DSL) for declaring workflows in the Orchestration Engine. Using this DSL, developers can define workflows declaratively instead of writing Go code.

---

## 1. Overview

The Workflow YAML DSL provides a human-readable, version-controllable format for defining workflows. It maps directly to the internal `engine.Workflow` data structures.

### Benefits

- **Version Control Friendly**: YAML files can be tracked in Git
- **No Code Changes**: Define new workflows without recompiling
- **Validation**: Schema-based validation before deployment
- **Portability**: Easy to share and replicate across environments

---

## 2. DSL Schema

### 2.1 Top-Level Structure

```yaml
# Metadata
id: <workflow-id>           # Required. Unique identifier for the workflow
name: <display-name>        # Optional. Human-readable name
description: <text>         # Optional. Workflow description
version: <semver>           # Optional. e.g., "1.0.0"

# Workflow Definition
nodes:
  - <node-definition>
  - ...

connections:
  - <connection-definition>
  - ...

# Optional: Event Marketplace Registration
events:
  - <event-definition>
  - ...
```

### 2.2 Node Definition

Each node represents a step in the workflow DAG.

```yaml
nodes:
  - id: <node-id>                    # Required. Unique within workflow
    type: <node-type>                # Required. See Node Types below
    name: <display-name>             # Optional. Human-readable label
    
    # Node-specific configuration
    parameters: <map>                # Optional. Key-value parameters
    trigger: <trigger-config>        # Optional. Only for StartNode
```

### 2.3 Node Types

Node types fall into two categories:

#### System Control Nodes (Platform-Provided)

These are built-in nodes provided by the platform for workflow control flow:

| Type | Description | Required Parameters |
|------|-------------|---------------------|
| `StartNode` | Entry point of the workflow | `trigger` (optional) |
| `JoinNode` | Waits for multiple inputs | `operator`, `inputs` |
| `PublishEvent` | Publishes to Event Marketplace | `event_name`, `domain`, `payload` |

#### Registered Node Types (Third-Party)

Custom node types are **registered by third parties** via the Node Registry API. They are referenced by their `full_type` identifier:

```
<node_type>@<version>
```

Examples:
- `http-request@v1`
- `send-email@v2`
- `data-transform@v1`

Third-party workers connect via WebSocket and execute these node types. See [NodeRegistration in technical-design.md](./technical-design.md#32-node-registration) for the registration model.

| Field | Description |
|-------|-------------|
| `node_type` | Base type name (e.g., `http-request`) |
| `version` | Semantic version (e.g., `v1`) |
| `full_type` | Combined identifier (e.g., `http-request@v1`) |
| `input_schema` | JSON Schema for node input validation |
| `output_ports` | Available output ports (e.g., `["success", "failure"]`) |

### 2.4 Connection Definition

Connections define the edges of the workflow DAG.

```yaml
connections:
  - from: <node-id>                  # Required. Source node
    from_port: <port-name>           # Optional. Default: "default"
    to: <node-id>                    # Required. Target node
    to_port: <port-name>             # Optional. For JoinNode inputs
```

### 2.5 Port Names

Standard port names for conditional routing:

| Port | Usage |
|------|-------|
| `default` | Default output port |
| `success` | Successful execution |
| `failure` | Failed execution |
| `true` | Condition evaluated to true |
| `false` | Condition evaluated to false |

---

## 3. Node Configuration Reference

### 3.1 StartNode

Entry point of workflow execution. Can be triggered manually or by marketplace events.

**Manual Trigger (default):**
```yaml
nodes:
  - id: start
    type: StartNode
    name: "Start Workflow"
```

**Event Trigger:**
```yaml
nodes:
  - id: start
    type: StartNode
    name: "Order Event Trigger"
    trigger:
      type: event
      criteria:
        event_name: order_created
        domain: ecommerce
      input_map:                     # Optional. Map event fields to inputs
        order_id: "{{.event.order_id}}"
        customer: "{{.event.customer_data}}"
```

### 3.2 Registered Node Types

Registered node types are provided by **third-party workers** via the Node Registry API. Use the `full_type` identifier (e.g., `http-request@v1`) to reference them.

**Basic Usage:**
```yaml
nodes:
  - id: process-order
    type: http-request@v1           # Registered node type
    name: "Call Payment API"
    parameters:                      # Must match the node's input_schema
      url: "https://api.payments.com/charge"
      method: POST
      body:
        amount: "{{.input.total}}"
        customer_id: "{{.input.customer_id}}"
```

**Conditional Node (with output ports):**

Third-party nodes can define custom output ports. For example, a `condition-check@v1` node might define `true` and `false` ports:

```yaml
nodes:
  - id: check-premium
    type: condition-check@v1
    name: "Is Premium Customer?"
    parameters:
      expression: "{{.input.customer_tier == 'premium'}}"
```

**Connection from conditional node:**
```yaml
connections:
  - from: check-premium
    from_port: "true"
    to: apply-discount
  - from: check-premium
    from_port: "false"
    to: standard-processing
```

> **Note:** The available `output_ports` are defined in the node's registration. Check the Node Registry for available ports.


### 3.3 JoinNode

Waits for multiple predecessor nodes to complete before continuing.

**Join Operators:**

| Operator | Behavior |
|----------|----------|
| `all` | Wait for ALL predecessors (AND logic) - default |
| `any` | Wait for ANY predecessor (OR logic) |

```yaml
nodes:
  - id: merge-data
    type: JoinNode
    name: "Merge User and Orders"
    parameters:
      operator: all
      inputs:
        - user
        - orders
```

**Connections to JoinNode (using `to_port`):**
```yaml
connections:
  - from: fetch-user
    to: merge-data
    to_port: user
  - from: fetch-orders
    to: merge-data
    to_port: orders
```

The JoinNode receives combined input:
```json
{
  "user": { "name": "Alice", "email": "alice@example.com" },
  "orders": [{ "id": 123, "total": 99.99 }]
}
```

### 3.4 PublishEvent

Publishes an event to the Event Marketplace for other workflows to consume.

```yaml
nodes:
  - id: publish-order-created
    type: PublishEvent
    name: "Publish Order Created Event"
    parameters:
      event_name: order_created
      domain: ecommerce
      payload:
        order_id: "{{.input.order_id}}"
        total: "{{.input.total}}"
        customer_id: "{{.input.customer_id}}"
```

---

## 4. Event Marketplace Registration

Optionally register public events that your workflow publishes. This enables discoverability and schema validation.

```yaml
events:
  - name: order_created
    domain: ecommerce
    description: "Fired when a new order is placed"
    schema:
      type: object
      properties:
        order_id:
          type: string
        total:
          type: number
        customer_id:
          type: string
      required:
        - order_id
        - total
```

---

## 5. Complete Examples

### 5.1 Simple Linear Workflow

```yaml
id: simple-order-processing
name: "Simple Order Processing"
description: "A basic workflow that processes orders sequentially"
version: "1.0.0"

nodes:
  - id: start
    type: StartNode
    name: "Start"

  - id: validate-order
    type: order-validator@v1
    name: "Validate Order"
    parameters:
      strict_mode: true

  - id: process-payment
    type: payment-processor@v1
    name: "Process Payment"
    parameters:
      retry_count: 3
      timeout: 30s

  - id: send-confirmation
    type: email-sender@v1
    name: "Send Confirmation Email"
    parameters:
      template: order_confirmation

connections:
  - from: start
    to: validate-order
  - from: validate-order
    to: process-payment
  - from: process-payment
    to: send-confirmation
```

### 5.2 Conditional Branching Workflow

```yaml
id: premium-order-processing
name: "Premium Order Processing"
description: "Applies discount for premium customers"
version: "1.0.0"

nodes:
  - id: start
    type: StartNode
    name: "Start"

  - id: check-tier
    type: condition-check@v1
    name: "Is Premium Customer?"
    parameters:
      expression: "{{.input.tier == 'premium'}}"

  - id: apply-discount
    type: discount-calculator@v1
    name: "Apply 20% Discount"
    parameters:
      discount_percent: 20

  - id: standard-price
    type: price-calculator@v1
    name: "Standard Pricing"

  - id: complete-order
    type: order-finalizer@v1
    name: "Complete Order"

connections:
  - from: start
    to: check-tier
  - from: check-tier
    from_port: "true"
    to: apply-discount
  - from: check-tier
    from_port: "false"
    to: standard-price
  - from: apply-discount
    to: complete-order
  - from: standard-price
    to: complete-order
```

### 5.3 Parallel Execution with Join

```yaml
id: order-fulfillment
name: "Order Fulfillment"
description: "Fetches user and order data in parallel, then sends email"
version: "1.0.0"

nodes:
  - id: start
    type: StartNode
    name: "Start"

  - id: fetch-user
    type: user-service@v1
    name: "Fetch User Details"
    parameters:
      include_profile: true

  - id: fetch-orders
    type: order-service@v1
    name: "Fetch Order History"
    parameters:
      limit: 10

  - id: merge
    type: JoinNode
    name: "Merge Data"
    parameters:
      operator: all
      inputs:
        - user
        - orders

  - id: send-email
    type: email-sender@v1
    name: "Send Summary Email"
    parameters:
      template: order_summary

connections:
  # Fan-out from start to parallel nodes
  - from: start
    to: fetch-user
  - from: start
    to: fetch-orders

  # Fan-in to join node with named ports
  - from: fetch-user
    to: merge
    to_port: user
  - from: fetch-orders
    to: merge
    to_port: orders

  # Continue after join
  - from: merge
    to: send-email
```

### 5.4 Event-Driven Workflow (Subscriber)

```yaml
id: shipping-service
name: "Shipping Service"
description: "Triggered by order_created events from the marketplace"
version: "1.0.0"

nodes:
  - id: start
    type: StartNode
    name: "Order Created Trigger"
    trigger:
      type: event
      criteria:
        event_name: order_created
        domain: ecommerce
      input_map:
        order_id: "{{.event.order_id}}"
        total: "{{.event.total}}"

  - id: prepare-shipment
    type: shipment-creator@v1
    name: "Prepare Shipment"
    parameters:
      carrier: auto

  - id: notify-customer
    type: notification-sender@v1
    name: "Notify Customer"
    parameters:
      channel: sms

connections:
  - from: start
    to: prepare-shipment
  - from: prepare-shipment
    to: notify-customer
```

### 5.5 Publisher Workflow with Event Registration

```yaml
id: order-service
name: "Order Service"
description: "Creates orders and publishes events to the marketplace"
version: "1.0.0"

# Register the events this workflow publishes
events:
  - name: order_created
    domain: ecommerce
    description: "Fired when a new order is placed"
    schema:
      type: object
      properties:
        order_id:
          type: string
        total:
          type: number
      required:
        - order_id
        - total

nodes:
  - id: start
    type: StartNode
    name: "Start"

  - id: create-order
    type: order-creator@v1
    name: "Create Order"
    parameters:
      validate: true

  - id: publish-event
    type: PublishEvent
    name: "Publish Order Created"
    parameters:
      event_name: order_created
      domain: ecommerce
      payload:
        order_id: "{{.input.order_id}}"
        total: "{{.input.total}}"

connections:
  - from: start
    to: create-order
  - from: create-order
    to: publish-event
```

---

## 6. Template Expressions

The DSL supports simple template expressions for dynamic values:

| Syntax | Description | Example |
|--------|-------------|---------|
| `{{.input.<field>}}` | Access workflow input | `{{.input.order_id}}` |
| `{{.event.<field>}}` | Access event payload (for triggers) | `{{.event.customer_id}}` |
| `{{.node.<id>.<field>}}` | Access output from previous node | `{{.node.fetch-user.email}}` |

---

## 7. Validation Rules

When loading a YAML workflow, the engine validates:

1. **Required Fields**
   - `id` must be present and unique
   - Each node must have `id` and `type`
   - Each connection must have `from` and `to`

2. **Node References**
   - All `from` and `to` in connections must reference existing node IDs
   - JoinNode `inputs` must match incoming `to_port` values

3. **DAG Structure**
   - Exactly one `StartNode` per workflow
   - No cycles in the connection graph

4. **Event Triggers**
   - `criteria` must include `event_name` and `domain`

---

## 8. File Organization

Recommended directory structure for workflow files:

```
workflows/
├── domain-a/
│   ├── workflow-1.yaml
│   └── workflow-2.yaml
├── domain-b/
│   └── workflow-3.yaml
└── shared/
    └── common-events.yaml    # Shared event definitions
```

---

## 9. Developer Guide: Using the DSL

### 9.1 Creating Your First Workflow

**Step 1: Create a YAML file**

Create a new file `workflows/my-first-workflow.yaml`:

```yaml
id: my-first-workflow
name: "My First Workflow"
description: "A simple demonstration workflow"
version: "1.0.0"

nodes:
  - id: start
    type: StartNode
    name: "Begin"

  - id: greet
    type: hello-world@v1
    name: "Say Hello"
    parameters:
      message: "Hello, World!"

connections:
  - from: start
    to: greet
```

**Step 2: Load the workflow**

Use the workflow loader (implementation pending):

```go
loader := workflow.NewYAMLLoader()
wf, err := loader.LoadFile("workflows/my-first-workflow.yaml")
if err != nil {
    log.Fatal(err)
}
```

**Step 3: Register and execute**

```go
repo.Register(wf)
engine.Execute(ctx, wf.ID, inputData)
```

### 9.2 Adding Conditional Logic

Use a registered conditional node (e.g., `condition-check@v1`) for branching based on input data:

```yaml
nodes:
  - id: check-amount
    type: condition-check@v1
    name: "High Value Order?"
    parameters:
      expression: "{{.input.total > 100}}"
```

Connect with explicit ports:

```yaml
connections:
  - from: check-amount
    from_port: "true"
    to: high-value-processing
  - from: check-amount
    from_port: "false"
    to: standard-processing
```

### 9.3 Parallel Execution Pattern

To run nodes in parallel:

1. Connect multiple nodes from the same source
2. Use a `JoinNode` to merge results

```yaml
# Fan-out
connections:
  - from: start
    to: task-a
  - from: start
    to: task-b
  - from: start
    to: task-c

# Fan-in with named ports
  - from: task-a
    to: merge
    to_port: result_a
  - from: task-b
    to: merge
    to_port: result_b
  - from: task-c
    to: merge
    to_port: result_c
```

### 9.4 Event-Driven Workflows

**Publishing Events:**

Add a `PublishEvent` node to emit events to the marketplace:

```yaml
nodes:
  - id: notify-marketplace
    type: PublishEvent
    parameters:
      event_name: task_completed
      domain: notifications
      payload:
        task_id: "{{.input.task_id}}"
        status: completed
```

**Subscribing to Events:**

Configure your `StartNode` with an event trigger:

```yaml
nodes:
  - id: start
    type: StartNode
    trigger:
      type: event
      criteria:
        event_name: task_completed
        domain: notifications
```

### 9.5 Best Practices

1. **Use Descriptive IDs**: Choose node IDs that describe their purpose
   ```yaml
   # Good
   id: validate-customer-email
   
   # Avoid
   id: step1
   ```

2. **Document Your Workflows**: Add `name` and `description` fields
   ```yaml
   id: order-processing
   name: "Order Processing Pipeline"
   description: |
     Handles new orders from the web store.
     Validates payment, reserves inventory, and sends confirmation.
   ```

3. **Version Your Workflows**: Use semantic versioning
   ```yaml
   version: "2.1.0"
   ```

4. **Register Events**: If publishing to the marketplace, declare your events
   ```yaml
   events:
     - name: order_shipped
       domain: logistics
       description: "Emitted when an order leaves the warehouse"
   ```

5. **Validate Locally**: Use the CLI validator before deploying
   ```bash
   orchestration validate workflows/my-workflow.yaml
   ```

### 9.6 Troubleshooting

| Issue | Solution |
|-------|----------|
| "Node not found" error | Check that all `from`/`to` references match existing node IDs |
| "Multiple StartNodes" error | Ensure only one node has `type: StartNode` |
| Event trigger not firing | Verify `event_name` and `domain` match the publisher exactly |
| JoinNode never completes | Check that all `to_port` values match the `inputs` parameter |
| Template not resolving | Ensure correct syntax: `{{.input.field}}` not `${input.field}` |

---

## 10. Future Enhancements

- **JSON Schema Validation**: Runtime validation of YAML against JSON Schema
- **Hot Reload**: Watch workflow files and reload on change
- **Import/Include**: Reference shared node definitions or sub-workflows
- **Variables**: Global and environment-specific variable substitution
- **Secrets**: Secure handling of sensitive configuration values
