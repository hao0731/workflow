# Event Marketplace UI Design

**Date:** 2026-01-28
**Status:** Approved

## Overview

Add a web UI page for browsing and discovering event definitions in the marketplace. Users can explore available events organized by domain, view metadata, and see example payloads.

## Goals

- Browse & discover event definitions in a catalog view
- Events grouped by domain with expandable sections
- View basic metadata (name, domain, description, owner, timestamps)
- View example payload generated from JSON schema

## Non-Goals (for this iteration)

- Workflow connection listings (which workflows publish/subscribe)
- Event management (create, edit, delete)
- Real-time event monitoring

---

## Backend API

### New Endpoints

Add to Workflow API server (`cmd/workflow-api`, port 8083):

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/events` | List all event definitions, grouped by domain |
| `GET` | `/api/events/:domain/:name` | Get a single event definition |

### Response Shapes

**`GET /api/events`**

Optional query param: `?domain=` to filter by domain.

```json
{
  "domains": [
    {
      "domain": "ecommerce",
      "events": [
        {
          "id": "...",
          "name": "order_created",
          "domain": "ecommerce",
          "full_name": "ecommerce.order_created",
          "description": "Fired when a new order is placed",
          "schema": { ... },
          "owner": "checkout-team",
          "created_at": "2026-01-15T10:00:00Z",
          "updated_at": "2026-01-15T10:00:00Z"
        }
      ]
    }
  ]
}
```

**`GET /api/events/:domain/:name`**

Returns the single `EventDefinition` object directly.

### Implementation

New file: `internal/api/marketplace_handler.go`

```go
type MarketplaceHandler struct {
    registry marketplace.EventRegistry
}

func NewMarketplaceHandler(registry marketplace.EventRegistry) *MarketplaceHandler

func (h *MarketplaceHandler) RegisterRoutes(g *echo.Group)
func (h *MarketplaceHandler) List(c echo.Context) error
func (h *MarketplaceHandler) Get(c echo.Context) error
```

Wire up in `cmd/workflow-api/main.go`:
- Create `EventRegistry` (MongoDB or in-memory based on config)
- Instantiate `MarketplaceHandler`
- Register routes under `/api/events`

---

## Frontend Architecture

### New Files

```
web/src/
  contexts/
    MarketplaceContext.tsx    # State management
  api/
    marketplace.ts            # API client
  utils/
    schemaExample.ts          # JSON Schema → example generator
  components/
    marketplace/
      MarketplacePage.tsx     # Page container
      DomainList.tsx          # Left panel - domain groups
      EventDetail.tsx         # Right panel - event details
      ExamplePayload.tsx      # Code block with copy button
```

### Modified Files

| File | Change |
|------|--------|
| `web/src/components/Header.tsx` | Add view toggle (Workflows / Marketplace) |
| `web/src/App.tsx` | Add MarketplaceContext provider, conditional view rendering |

### MarketplaceContext

```typescript
interface MarketplaceState {
  domains: Domain[];
  selectedEvent: EventDefinition | null;
  loading: boolean;
  error: string | null;
}

interface MarketplaceActions {
  fetchDomains: () => Promise<void>;
  selectEvent: (domain: string, name: string) => void;
  clearSelection: () => void;
}
```

### Page Layout

Two-panel layout:

```
┌─────────────────────────────────────────────────────┐
│  Header  [Workflows] [Marketplace]                  │
├─────────────────┬───────────────────────────────────┤
│                 │                                   │
│  Domain List    │  Event Detail                     │
│                 │                                   │
│  ▼ ecommerce    │  Name: order_created              │
│    order_created│  Domain: ecommerce                │
│    order_shipped│  Full name: ecommerce.order_created
│  ▶ notifications│  Description: ...                 │
│  ▶ inventory    │  Owner: checkout-team             │
│                 │  Created: 2026-01-15              │
│                 │                                   │
│                 │  Example Payload         [Copy]   │
│                 │  ┌─────────────────────────────┐  │
│                 │  │ {                           │  │
│                 │  │   "order_id": "ord_12345",  │  │
│                 │  │   "amount": 0               │  │
│                 │  │ }                           │  │
│                 │  └─────────────────────────────┘  │
└─────────────────┴───────────────────────────────────┘
```

### Domain List Behavior

- Domains displayed as collapsible sections
- Click domain header to expand/collapse
- Click event name to select and show details
- Selected event visually highlighted
- Loading spinner while fetching
- Error message on API failure
- "No events registered" empty state

### Example Payload Generation

Utility function `generateExampleFromSchema(schema)`:

- Recursively walks JSON Schema
- Uses `example` field if present
- Default values by type:
  - `string` → `"string"`
  - `number` / `integer` → `0`
  - `boolean` → `true`
  - `object` → recurses into `properties`
  - `array` → one example item from `items`

Example input:
```json
{
  "type": "object",
  "properties": {
    "order_id": { "type": "string", "example": "ord_12345" },
    "amount": { "type": "number" },
    "items": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}
```

Example output:
```json
{
  "order_id": "ord_12345",
  "amount": 0,
  "items": ["string"]
}
```

### Event Detail View

Displays when an event is selected:

**Metadata section:**
- Name
- Domain
- Full name (with copy button)
- Description
- Owner
- Created timestamp
- Updated timestamp

**Example payload section:**
- Header: "Example Payload"
- Generated JSON in monospace code block
- Copy button

**Empty state:**
- "Select an event to view details"

---

## File Summary

| Layer | File | Type |
|-------|------|------|
| Backend | `internal/api/marketplace_handler.go` | New |
| Backend | `cmd/workflow-api/main.go` | Modified |
| Frontend | `web/src/contexts/MarketplaceContext.tsx` | New |
| Frontend | `web/src/api/marketplace.ts` | New |
| Frontend | `web/src/utils/schemaExample.ts` | New |
| Frontend | `web/src/components/marketplace/MarketplacePage.tsx` | New |
| Frontend | `web/src/components/marketplace/DomainList.tsx` | New |
| Frontend | `web/src/components/marketplace/EventDetail.tsx` | New |
| Frontend | `web/src/components/marketplace/ExamplePayload.tsx` | New |
| Frontend | `web/src/components/Header.tsx` | Modified |
| Frontend | `web/src/App.tsx` | Modified |

**Total: 2 backend files, 9 frontend files**
