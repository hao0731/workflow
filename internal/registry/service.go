package registry

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrNodeDisabled = errors.New("node type is disabled")
)

// Service handles node registration business logic.
type Service struct {
	repo      Repository
	jwtConfig JWTConfig
	logger    *slog.Logger
}

// ServiceOption is a functional option for Service.
type ServiceOption func(*Service)

// WithServiceLogger sets a custom logger.
func WithServiceLogger(logger *slog.Logger) ServiceOption {
	return func(s *Service) {
		s.logger = logger
	}
}

// NewService creates a new registry service.
func NewService(repo Repository, jwtConfig JWTConfig, opts ...ServiceOption) *Service {
	s := &Service{
		repo:      repo,
		jwtConfig: jwtConfig,
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register creates a new node type registration.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	version := req.Version
	if version == "" {
		version = "v1"
	}

	now := time.Now()
	reg := &NodeRegistration{
		ID:           uuid.New().String(),
		NodeType:     req.NodeType,
		Version:      version,
		FullType:     BuildFullType(req.NodeType, version),
		DisplayName:  req.DisplayName,
		Description:  req.Description,
		InputSchema:  req.InputSchema,
		OutputPorts:  req.OutputPorts,
		Owner:        req.Owner,
		Enabled:      true,
		Subject:      BuildSubject(req.NodeType, version),
		ConsumerName: BuildConsumerName(req.NodeType, version),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Generate token
	token, err := GenerateToken(s.jwtConfig, reg)
	if err != nil {
		return nil, err
	}
	reg.TokenHash = HashToken(token)

	if err := s.repo.Create(ctx, reg); err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "node registered",
		slog.String("full_type", reg.FullType),
		slog.String("owner", reg.Owner),
	)

	return &RegisterResponse{
		ID:           reg.ID,
		FullType:     reg.FullType,
		Subject:      reg.Subject,
		ConsumerName: reg.ConsumerName,
		APIToken:     token,
		CreatedAt:    reg.CreatedAt.Format(time.RFC3339),
	}, nil
}

// GetByFullType retrieves a node registration by its full type.
func (s *Service) GetByFullType(ctx context.Context, fullType string) (*NodeRegistration, error) {
	return s.repo.GetByFullType(ctx, fullType)
}

// ValidateAndGetNode validates a token and returns the node registration.
func (s *Service) ValidateAndGetNode(ctx context.Context, tokenString string) (*NodeRegistration, error) {
	claims, err := ValidateToken(s.jwtConfig, tokenString)
	if err != nil {
		return nil, ErrInvalidToken
	}

	reg, err := s.repo.GetByID(ctx, claims.ID)
	if err != nil {
		return nil, err
	}

	if !reg.Enabled {
		return nil, ErrNodeDisabled
	}

	return reg, nil
}

// Connect validates token and returns NATS connection info.
func (s *Service) Connect(ctx context.Context, tokenString string) (*ConnectResponse, error) {
	reg, err := s.ValidateAndGetNode(ctx, tokenString)
	if err != nil {
		return nil, err
	}

	s.logger.DebugContext(ctx, "worker connected",
		slog.String("full_type", reg.FullType),
	)

	return &ConnectResponse{
		ProxyURL:      "/proxy/nats", // Would be the actual proxy URL
		Subject:       reg.Subject,
		ConsumerName:  reg.ConsumerName,
		ResultSubject: "workflow.events.results",
	}, nil
}

// Heartbeat updates the worker health status.
func (s *Service) Heartbeat(ctx context.Context, fullType string, req HealthRequest) (*HealthResponse, error) {
	if err := s.repo.UpdateHeartbeat(ctx, fullType, req.WorkerID); err != nil {
		return nil, err
	}

	return &HealthResponse{
		Acknowledged: true,
		ServerTime:   time.Now().Format(time.RFC3339),
	}, nil
}

// List returns all enabled node registrations.
func (s *Service) List(ctx context.Context) ([]NodeInfo, error) {
	registrations, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	infos := make([]NodeInfo, len(registrations))
	for i, reg := range registrations {
		var lastHB *string
		if reg.LastHeartbeat != nil {
			t := reg.LastHeartbeat.Format(time.RFC3339)
			lastHB = &t
		}

		infos[i] = NodeInfo{
			FullType:      reg.FullType,
			DisplayName:   reg.DisplayName,
			Description:   reg.Description,
			OutputPorts:   reg.OutputPorts,
			Enabled:       reg.Enabled,
			WorkerCount:   reg.WorkerCount,
			LastHeartbeat: lastHB,
		}
	}
	return infos, nil
}

// Exists checks if a node type is registered and enabled.
func (s *Service) Exists(ctx context.Context, fullType string) bool {
	reg, err := s.repo.GetByFullType(ctx, fullType)
	if err != nil {
		return false
	}
	return reg.Enabled
}
