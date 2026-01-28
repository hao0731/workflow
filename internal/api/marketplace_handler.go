package api

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/cheriehsieh/orchestration/internal/marketplace"
)

// MarketplaceHandler handles HTTP requests for event marketplace browsing.
type MarketplaceHandler struct {
	registry marketplace.EventRegistry
	logger   *slog.Logger
}

// NewMarketplaceHandler creates a new MarketplaceHandler.
func NewMarketplaceHandler(registry marketplace.EventRegistry, logger *slog.Logger) *MarketplaceHandler {
	return &MarketplaceHandler{
		registry: registry,
		logger:   logger,
	}
}

// RegisterRoutes registers marketplace API routes.
func (h *MarketplaceHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/events", h.List)
	g.GET("/events/:domain/:name", h.Get)
}

// EventResponse is the API response for an event definition.
type EventResponse struct {
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Schema      any    `json:"schema,omitempty"`
	Owner       string `json:"owner,omitempty"`
}

// DomainGroup groups events by domain.
type DomainGroup struct {
	Domain string          `json:"domain"`
	Events []EventResponse `json:"events"`
}

// List handles GET /events
// Query param: ?domain=xxx to filter by domain
func (h *MarketplaceHandler) List(c echo.Context) error {
	domain := c.QueryParam("domain")

	events, err := h.registry.List(c.Request().Context(), domain)
	if err != nil {
		h.logger.Error("failed to list events", slog.String("error", err.Error()))
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list events"})
	}

	// Group events by domain
	domainMap := make(map[string][]EventResponse)
	for _, ev := range events {
		resp := EventResponse{
			Name:        ev.Name,
			Domain:      ev.Domain,
			FullName:    ev.FullName,
			Description: ev.Description,
			Schema:      ev.Schema,
			Owner:       ev.Owner,
		}
		domainMap[ev.Domain] = append(domainMap[ev.Domain], resp)
	}

	// Convert to array of groups
	groups := make([]DomainGroup, 0, len(domainMap))
	for domain, events := range domainMap {
		groups = append(groups, DomainGroup{
			Domain: domain,
			Events: events,
		})
	}

	return c.JSON(http.StatusOK, groups)
}

// Get handles GET /events/:domain/:name
func (h *MarketplaceHandler) Get(c echo.Context) error {
	domain := c.Param("domain")
	name := c.Param("name")

	event, err := h.registry.Get(c.Request().Context(), domain, name)
	if err != nil {
		if err == marketplace.ErrEventNotFound {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "event not found"})
		}
		h.logger.Error("failed to get event", slog.String("error", err.Error()))
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get event"})
	}

	return c.JSON(http.StatusOK, EventResponse{
		Name:        event.Name,
		Domain:      event.Domain,
		FullName:    event.FullName,
		Description: event.Description,
		Schema:      event.Schema,
		Owner:       event.Owner,
	})
}
