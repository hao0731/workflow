package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/marketplace"
)

func TestMarketplaceHandler_List(t *testing.T) {
	t.Run("empty registry returns empty array", func(t *testing.T) {
		registry := marketplace.NewInMemoryEventRegistry()
		handler := NewMarketplaceHandler(registry, slog.Default())

		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/events", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := handler.List(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var groups []DomainGroup
		err = json.Unmarshal(rec.Body.Bytes(), &groups)
		require.NoError(t, err)
		assert.Empty(t, groups)
	})

	t.Run("returns events grouped by domain", func(t *testing.T) {
		registry := marketplace.NewInMemoryEventRegistry()

		// Register events
		_ = registry.Register(context.Background(), &marketplace.EventDefinition{
			Name:        "order.created",
			Domain:      "orders",
			Description: "Emitted when an order is created",
		})
		_ = registry.Register(context.Background(), &marketplace.EventDefinition{
			Name:        "order.shipped",
			Domain:      "orders",
			Description: "Emitted when an order ships",
		})
		_ = registry.Register(context.Background(), &marketplace.EventDefinition{
			Name:        "user.registered",
			Domain:      "users",
			Description: "Emitted when a user registers",
		})

		handler := NewMarketplaceHandler(registry, slog.Default())

		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/events", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := handler.List(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var groups []DomainGroup
		err = json.Unmarshal(rec.Body.Bytes(), &groups)
		require.NoError(t, err)

		// Find groups by domain (order may vary)
		domainMap := make(map[string]DomainGroup)
		for _, g := range groups {
			domainMap[g.Domain] = g
		}

		assert.Len(t, groups, 2)
		assert.Contains(t, domainMap, "orders")
		assert.Contains(t, domainMap, "users")
		assert.Len(t, domainMap["orders"].Events, 2)
		assert.Len(t, domainMap["users"].Events, 1)
	})

	t.Run("filters by domain query param", func(t *testing.T) {
		registry := marketplace.NewInMemoryEventRegistry()

		_ = registry.Register(context.Background(), &marketplace.EventDefinition{
			Name:        "order.created",
			Domain:      "orders",
			Description: "Order created",
		})
		_ = registry.Register(context.Background(), &marketplace.EventDefinition{
			Name:        "user.registered",
			Domain:      "users",
			Description: "User registered",
		})

		handler := NewMarketplaceHandler(registry, slog.Default())

		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/events?domain=orders", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := handler.List(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var groups []DomainGroup
		err = json.Unmarshal(rec.Body.Bytes(), &groups)
		require.NoError(t, err)

		assert.Len(t, groups, 1)
		assert.Equal(t, "orders", groups[0].Domain)
		assert.Len(t, groups[0].Events, 1)
	})
}

func TestMarketplaceHandler_Get(t *testing.T) {
	t.Run("returns event by domain and name", func(t *testing.T) {
		registry := marketplace.NewInMemoryEventRegistry()

		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"order_id": map[string]any{"type": "string"},
			},
		}

		_ = registry.Register(context.Background(), &marketplace.EventDefinition{
			Name:        "order.created",
			Domain:      "orders",
			Description: "Emitted when an order is created",
			Schema:      schema,
			Owner:       "order-service",
		})

		handler := NewMarketplaceHandler(registry, slog.Default())

		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/events/orders/order.created", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("domain", "name")
		c.SetParamValues("orders", "order.created")

		err := handler.Get(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var resp EventResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, "order.created", resp.Name)
		assert.Equal(t, "orders", resp.Domain)
		assert.Equal(t, "orders.order.created", resp.FullName)
		assert.Equal(t, "Emitted when an order is created", resp.Description)
		assert.Equal(t, "order-service", resp.Owner)
		assert.NotNil(t, resp.Schema)
	})

	t.Run("returns 404 when event not found", func(t *testing.T) {
		registry := marketplace.NewInMemoryEventRegistry()
		handler := NewMarketplaceHandler(registry, slog.Default())

		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/events/nonexistent/event", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("domain", "name")
		c.SetParamValues("nonexistent", "event")

		err := handler.Get(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, rec.Code)

		var resp map[string]string
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "event not found", resp["error"])
	})
}
