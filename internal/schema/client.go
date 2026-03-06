package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultHTTPTimeout = 5 * time.Second

// EventSchema is a versioned event schema document fetched from the registry.
type EventSchema struct {
	EventType string
	Version   string
	Schema    json.RawMessage
	ETag      string
}

// LookupClient resolves schemas and supports conditional requests with ETags.
type LookupClient interface {
	LookupEventSchema(ctx context.Context, eventType, version, ifNoneMatch string) (*EventSchema, bool, error)
}

// Client fetches event schemas from the schema registry over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// ClientOption configures the schema registry client.
type ClientOption func(*Client)

type eventSchemaResponse struct {
	EventType string          `json:"eventType"`
	Version   string          `json:"version"`
	Schema    json.RawMessage `json:"schema"`
}

// WithHTTPClient injects the HTTP client used for registry lookups.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// NewClient creates a schema registry client for the given base URL.
func NewClient(baseURL string, opts ...ClientOption) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("schema registry base url is required")
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse schema registry base url: %w", err)
	}

	client := &Client{
		baseURL: strings.TrimRight(parsedURL.String(), "/"),
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

// GetEventSchema fetches a schema document without conditional headers.
func (c *Client) GetEventSchema(ctx context.Context, eventType, version string) (*EventSchema, error) {
	document, _, err := c.LookupEventSchema(ctx, eventType, version, "")
	if err != nil {
		return nil, err
	}

	return document, nil
}

// LookupEventSchema fetches a schema document and optionally sends an If-None-Match header.
func (c *Client) LookupEventSchema(ctx context.Context, eventType, version, ifNoneMatch string) (*EventSchema, bool, error) {
	if strings.TrimSpace(eventType) == "" {
		return nil, false, fmt.Errorf("event type is required")
	}
	if strings.TrimSpace(version) == "" {
		return nil, false, fmt.Errorf("version is required")
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/schemas/events/%s/versions/%s", c.baseURL, url.PathEscape(eventType), url.PathEscape(version)),
		nil,
	)
	if err != nil {
		return nil, false, fmt.Errorf("create schema lookup request: %w", err)
	}
	if ifNoneMatch != "" {
		request.Header.Set("If-None-Match", ifNoneMatch)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("schema lookup request failed: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusNotModified {
		return nil, true, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("schema lookup returned status %d", response.StatusCode)
	}

	var payload eventSchemaResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("decode schema lookup response: %w", err)
	}

	if payload.EventType == "" {
		payload.EventType = eventType
	}
	if payload.Version == "" {
		payload.Version = version
	}

	return &EventSchema{
		EventType: payload.EventType,
		Version:   payload.Version,
		Schema:    append(json.RawMessage(nil), payload.Schema...),
		ETag:      response.Header.Get("ETag"),
	}, false, nil
}
