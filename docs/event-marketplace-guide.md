# Event Marketplace Developer Guide

This guide explains how to use the Event Marketplace to enable inter-workflow communication through public events.

---

## Overview

The Event Marketplace is a platform feature that allows workflows to:
- **Publish events** that other workflows can consume
- **Subscribe to events** to trigger workflow execution automatically
- **Discover available events** through a centralized registry

```
┌─────────────────┐                    ┌─────────────────┐
│  Publisher      │                    │  Subscriber     │
│  Workflow       │                    │  Workflow       │
│                 │    Event           │                 │
│ [PublishEvent]──┼───Marketplace────► │ [StartNode]     │
│                 │    Bus             │  (Event Trigger)│
└─────────────────┘                    └─────────────────┘
```

---

## Part 1: Publishing Events

### Step 1: Define Your Event Schema

First, define the event in your workflow YAML under the `events` section:

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

| Field | Description |
|-------|-------------|
| `name` | Unique event name within domain (e.g., `order_created`) |
| `domain` | Logical grouping (e.g., `ecommerce`, `payments`, `shipping`) |
| `description` | Human-readable description for the registry |
| `schema` | JSON Schema defining the payload structure |

### Step 2: Submit for Platform Approval

When you deploy your workflow, the event definition is submitted for **Platform Admin review**.

**Approval Criteria:**
- Schema follows naming conventions
- Event has clear documentation
- Complies with platform policies

**Check Status via API:**
```bash
# List your pending events
curl -H "Authorization: Bearer $TOKEN" \
  https://api.platform.io/events?owner_id=your-id&status=PENDING
```

**Statuses:**
| Status | Meaning |
|--------|---------|
| `PENDING` | Awaiting platform admin review |
| `APPROVED` | Can be published to marketplace |
| `REJECTED` | Review feedback provided |

### Step 3: Add PublishEvent Node to Workflow

Once approved, add a `PublishEvent` node to your workflow:

```yaml
nodes:
  - id: create-order
    type: order-creator@v1
    name: "Create Order"
    parameters:
      product_id: "{{.input.product_id}}"

  - id: publish-event
    type: PublishEvent
    name: "Publish Order Created"
    parameters:
      event_name: order_created
      domain: ecommerce
      payload:
        order_id: "{{.input.order_id}}"
        total: "{{.input.total}}"
        customer_id: "{{.input.customer_id}}"

connections:
  - from: create-order
    to: publish-event
```

### Complete Publisher Example

```yaml
id: order-service
name: "Order Service"
description: "Creates orders and publishes events to the marketplace"
version: "1.0.0"

# Register event for marketplace
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
    name: "Create Order in Database"
    parameters:
      validate: true

  - id: publish-order-created
    type: PublishEvent
    name: "Publish Order Created Event"
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
    to: publish-order-created
```

---

## Part 2: Browsing Available Events

### Discover Events via API

Browse the Event Marketplace to find events you can subscribe to:

```bash
# List all approved events
curl -H "Authorization: Bearer $TOKEN" \
  https://api.platform.io/events

# Filter by domain
curl -H "Authorization: Bearer $TOKEN" \
  https://api.platform.io/events?domain=ecommerce

# Get specific event details
curl -H "Authorization: Bearer $TOKEN" \
  https://api.platform.io/events/order_created
```

### Example Response

```json
{
  "events": [
    {
      "id": "evt_001",
      "name": "order_created",
      "domain": "ecommerce",
      "description": "Fired when a new order is placed",
      "owner_id": "team-payments",
      "status": "APPROVED",
      "schema": {
        "type": "object",
        "properties": {
          "order_id": { "type": "string" },
          "total": { "type": "number" }
        }
      }
    },
    {
      "id": "evt_002",
      "name": "payment_completed",
      "domain": "payments",
      "description": "Fired when payment is successfully processed",
      "owner_id": "team-payments",
      "status": "APPROVED"
    }
  ]
}
```

---

## Part 3: Subscribing to Events

### Step 1: Request Subscription

Submit a subscription request to the event owner:

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  https://api.platform.io/events/order_created/subscribe \
  -d '{
    "workflow_id": "shipping-service",
    "justification": "Need to create shipments when orders are placed"
  }'
```

### Step 2: Wait for Owner Approval

The **Event Owner** (publisher) reviews your request and decides whether to approve.

**Check Status:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://api.platform.io/subscriptions?subscriber_id=your-id
```

**Response:**
```json
{
  "id": "sub_001",
  "event_id": "evt_001",
  "workflow_id": "shipping-service",
  "status": "PENDING",
  "justification": "Need to create shipments when orders are placed",
  "created_at": "2024-01-13T10:00:00Z"
}
```

**Statuses:**
| Status | Meaning |
|--------|---------|
| `PENDING` | Awaiting event owner approval |
| `APPROVED` | Events will be dispatched to your workflow |
| `REJECTED` | Owner declined (reason provided) |
| `REVOKED` | Previously approved, now revoked |

### Step 3: Configure Event Trigger in Workflow

Once approved, configure your workflow's `StartNode` to trigger on the event:

```yaml
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
```

| Field | Description |
|-------|-------------|
| `trigger.type` | Set to `event` for marketplace triggers |
| `trigger.criteria.event_name` | Name of the event to subscribe to |
| `trigger.criteria.domain` | Domain of the event |
| `trigger.input_map` | Map event payload fields to workflow inputs |

### Complete Subscriber Example

```yaml
id: shipping-service
name: "Shipping Service"
description: "Automatically creates shipments when orders are placed"
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
    name: "Create Shipment"
    parameters:
      carrier: auto
      order_id: "{{.input.order_id}}"

  - id: notify-customer
    type: notification-sender@v1
    name: "Send Shipping Notification"
    parameters:
      channel: sms
      message: "Your order {{.input.order_id}} is being shipped!"

connections:
  - from: start
    to: prepare-shipment
  - from: prepare-shipment
    to: notify-customer
```

---

## Part 4: How Event Triggering Works

### Execution Flow

```
1. Publisher workflow executes PublishEvent node
      │
      ▼
2. Event published to marketplace.<domain>.<event>
      │
      ▼
3. Event Router receives the event
      │
      ▼
4. Router queries approved subscriptions
      │
      ▼
5. For each approved subscription:
   └──► Spawn new workflow execution
        └──► Inject event payload as input
```

### Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              Event Flow                                  │
│                                                                          │
│  [Order Service]                     [Event Router]                      │
│       │                                   │                              │
│       │ 1. PublishEvent                   │                              │
│       │    order_created                  │                              │
│       └──────────────────────────────────►│                              │
│                                           │                              │
│                                           │ 2. Lookup approved subs      │
│                                           │                              │
│                                           │ 3. Found: shipping-service   │
│                                           │           inventory-service  │
│                                           │                              │
│                     ┌─────────────────────┴─────────────────────┐       │
│                     ▼                                           ▼       │
│            [Shipping Service]                          [Inventory Srv]  │
│            Execution Started                           Execution Started│
│            input: { order_id, total }                  input: { ... }   │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Quick Reference

### Publisher Checklist

- [ ] Define event schema in `events` section
- [ ] Deploy workflow to submit for approval
- [ ] Wait for Platform Admin approval (status: `APPROVED`)
- [ ] Add `PublishEvent` node to workflow
- [ ] Deploy updated workflow

### Subscriber Checklist

- [ ] Browse marketplace for available events
- [ ] Submit subscription request with justification
- [ ] Wait for Event Owner approval (status: `APPROVED`)
- [ ] Configure `StartNode` with event trigger
- [ ] Deploy workflow

### API Endpoints Summary

| Action | Method | Endpoint |
|--------|--------|----------|
| List events | `GET` | `/events` |
| Get event details | `GET` | `/events/:name` |
| Request subscription | `POST` | `/events/:name/subscribe` |
| Check my subscriptions | `GET` | `/subscriptions?subscriber_id=me` |

---

## Troubleshooting

### Event not triggering my workflow

1. **Check subscription status** — Must be `APPROVED`
2. **Verify event criteria** — `event_name` and `domain` must match exactly
3. **Check event status** — Publisher's event must be `APPROVED`

### PublishEvent node failing

1. **Check event approval** — Your event definition must be `APPROVED`
2. **Validate payload** — Must match the registered schema

### Subscription rejected

Contact the event owner to understand why. Common reasons:
- Insufficient justification
- Unknown subscriber
- Rate limiting concerns
