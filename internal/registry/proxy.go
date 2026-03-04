package registry

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/nats-io/nats.go"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // In production, validate origin
	},
}

// ProxyMessage is the message format between worker and proxy.
type ProxyMessage struct {
	Type string          `json:"type"` // "event" or "result"
	Data json.RawMessage `json:"data"`
}

// Proxy bridges WebSocket connections to NATS.
type Proxy struct {
	js            nats.JetStreamContext
	service       *Service
	resultSubject string
	logger        *slog.Logger
	connections   map[string]*workerConn
	mu            sync.RWMutex
}

type workerConn struct {
	ws       *websocket.Conn
	fullType string
	subject  string
	cancel   context.CancelFunc
}

// ProxyOption is a functional option for Proxy.
type ProxyOption func(*Proxy)

// WithProxyLogger sets a custom logger.
func WithProxyLogger(logger *slog.Logger) ProxyOption {
	return func(p *Proxy) {
		p.logger = logger
	}
}

// NewProxy creates a new WebSocket-to-NATS proxy.
func NewProxy(js nats.JetStreamContext, service *Service, resultSubject string, opts ...ProxyOption) *Proxy {
	p := &Proxy{
		js:            js,
		service:       service,
		resultSubject: resultSubject,
		logger:        slog.Default(),
		connections:   make(map[string]*workerConn),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// HandleWebSocket handles WebSocket connections from workers.
func (p *Proxy) HandleWebSocket(c echo.Context) error {
	// Extract and validate JWT token
	token := c.QueryParam("token")
	if token == "" {
		token = c.Request().Header.Get("Authorization")
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
	}

	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing token"})
	}

	reg, err := p.service.ValidateAndGetNode(c.Request().Context(), token)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
	}

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		p.logger.Error("websocket upgrade failed", slog.Any("error", err))
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	conn := &workerConn{
		ws:       ws,
		fullType: reg.FullType,
		subject:  reg.Subject,
		cancel:   cancel,
	}

	p.mu.Lock()
	p.connections[reg.ID] = conn
	p.mu.Unlock()

	p.logger.Info("worker connected",
		slog.String("full_type", reg.FullType),
		slog.String("subject", reg.Subject),
	)

	// Start goroutines for bidirectional communication
	go p.subscribeAndForward(ctx, conn)
	go p.readFromWorker(ctx, conn, reg.ID)

	return nil
}

// subscribeAndForward subscribes to NATS and forwards events to WebSocket.
func (p *Proxy) subscribeAndForward(ctx context.Context, conn *workerConn) {
	sub, err := p.js.PullSubscribe(conn.subject, "proxy-"+conn.fullType)
	if err != nil {
		p.logger.Error("NATS subscribe failed", slog.Any("error", err))
		return
	}
	defer func() { _ = sub.Unsubscribe() }()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msgs, err := sub.Fetch(1, nats.MaxWait(5*time.Second))
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				continue
			}

			for _, msg := range msgs {
				event := cloudevents.NewEvent()
				if err := event.UnmarshalJSON(msg.Data); err != nil {
					p.logger.Warn("invalid event", slog.Any("error", err))
					_ = msg.Ack()
					continue
				}

				proxyMsg := ProxyMessage{
					Type: "event",
					Data: msg.Data,
				}

				_ = conn.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.ws.WriteJSON(proxyMsg); err != nil {
					p.logger.Warn("websocket write failed", slog.Any("error", err))
					_ = msg.Nak()
					continue
				}

				_ = msg.Ack()
			}
		}
	}
}

// readFromWorker reads results from WebSocket and publishes to NATS.
func (p *Proxy) readFromWorker(ctx context.Context, conn *workerConn, regID string) {
	defer func() {
		conn.cancel()
		conn.ws.Close() //nolint:errcheck // best-effort cleanup in defer
		p.mu.Lock()
		delete(p.connections, regID)
		p.mu.Unlock()
		p.logger.Info("worker disconnected", slog.String("full_type", conn.fullType))
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var proxyMsg ProxyMessage
			if err := conn.ws.ReadJSON(&proxyMsg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					p.logger.Warn("websocket read error", slog.Any("error", err))
				}
				return
			}

			if proxyMsg.Type == "result" {
				// Forward result to NATS
				_, err := p.js.Publish(p.resultSubject, proxyMsg.Data)
				if err != nil {
					p.logger.Error("failed to publish result", slog.Any("error", err))
				}
			}
		}
	}
}

// Close shuts down all connections.
func (p *Proxy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.connections {
		conn.cancel()
		conn.ws.Close() //nolint:errcheck // best-effort cleanup in Close
	}
	p.connections = make(map[string]*workerConn)
}
