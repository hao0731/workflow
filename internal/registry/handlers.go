package registry

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Handler provides HTTP handlers for the registry API.
type Handler struct {
	service *Service
}

// NewHandler creates a new API handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts all registry routes on the Echo instance.
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	g := e.Group("/nodes")
	g.POST("", h.Register)
	g.GET("", h.List)
	g.GET("/:fullType", h.Get)
	g.POST("/:fullType/connect", h.Connect)
	g.POST("/:fullType/health", h.Health)
}

// Register handles POST /nodes.
func (h *Handler) Register(c echo.Context) error {
	var req RegisterRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.NodeType == "" || req.DisplayName == "" || req.Owner == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "node_type, display_name, and owner are required"})
	}

	resp, err := h.service.Register(c.Request().Context(), req)
	if err != nil {
		if errors.Is(err, ErrNodeAlreadyExists) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "node type already exists"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, resp)
}

// List handles GET /nodes.
func (h *Handler) List(c echo.Context) error {
	nodes, err := h.service.List(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, nodes)
}

// Get handles GET /nodes/:fullType.
func (h *Handler) Get(c echo.Context) error {
	fullType := c.Param("fullType")

	reg, err := h.service.GetByFullType(c.Request().Context(), fullType)
	if err != nil {
		if errors.Is(err, ErrNodeNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "node type not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, NodeInfo{
		FullType:    reg.FullType,
		DisplayName: reg.DisplayName,
		Description: reg.Description,
		OutputPorts: reg.OutputPorts,
		Enabled:     reg.Enabled,
		WorkerCount: reg.WorkerCount,
	})
}

// Connect handles POST /nodes/:fullType/connect.
func (h *Handler) Connect(c echo.Context) error {
	token := extractBearerToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing authorization token"})
	}

	resp, err := h.service.Connect(c.Request().Context(), token)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrNodeDisabled) {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, resp)
}

// Health handles POST /nodes/:fullType/health.
func (h *Handler) Health(c echo.Context) error {
	fullType := c.Param("fullType")

	token := extractBearerToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing authorization token"})
	}

	// Validate token first
	if _, err := h.service.ValidateAndGetNode(c.Request().Context(), token); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
	}

	var req HealthRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	resp, err := h.service.Heartbeat(c.Request().Context(), fullType, req)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, resp)
}

func extractBearerToken(c echo.Context) string {
	auth := c.Request().Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
