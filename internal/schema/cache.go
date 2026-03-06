package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Cache stores schema lookups in-memory for a bounded TTL.
type Cache struct {
	lookupClient LookupClient
	ttl          time.Duration
	clock        func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	document  *EventSchema
	expiresAt time.Time
}

// CacheOption configures the schema cache.
type CacheOption func(*Cache)

// WithClock overrides the time source, primarily for tests.
func WithClock(clock func() time.Time) CacheOption {
	return func(c *Cache) {
		if clock != nil {
			c.clock = clock
		}
	}
}

// NewCache creates a TTL cache that wraps a schema registry lookup client.
func NewCache(lookupClient LookupClient, ttl time.Duration, opts ...CacheOption) *Cache {
	cache := &Cache{
		lookupClient: lookupClient,
		ttl:          ttl,
		clock:        time.Now,
		entries:      make(map[string]cacheEntry),
	}
	for _, opt := range opts {
		opt(cache)
	}
	if cache.ttl <= 0 {
		cache.ttl = time.Minute
	}

	return cache
}

// NewCachedClient creates an HTTP schema client and wraps it with a TTL cache.
func NewCachedClient(baseURL string, ttl time.Duration, opts ...ClientOption) (*Cache, error) {
	client, err := NewClient(baseURL, opts...)
	if err != nil {
		return nil, err
	}

	return NewCache(client, ttl), nil
}

// GetEventSchema returns a cached schema or refreshes it from the registry when needed.
func (c *Cache) GetEventSchema(ctx context.Context, eventType, version string) (*EventSchema, error) {
	if c.lookupClient == nil {
		return nil, fmt.Errorf("schema lookup client is required")
	}

	key := cacheKey(eventType, version)
	now := c.clock()

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if ok && now.Before(entry.expiresAt) {
		return cloneEventSchema(entry.document), nil
	}

	ifNoneMatch := ""
	if ok && entry.document != nil {
		ifNoneMatch = entry.document.ETag
	}

	document, notModified, err := c.lookupClient.LookupEventSchema(ctx, eventType, version, ifNoneMatch)
	if err != nil {
		return nil, err
	}
	if notModified {
		if !ok || entry.document == nil {
			return nil, fmt.Errorf("schema lookup returned not modified without cached entry")
		}

		entry.expiresAt = now.Add(c.ttl)
		c.entries[key] = entry
		return cloneEventSchema(entry.document), nil
	}

	c.entries[key] = cacheEntry{
		document:  cloneEventSchema(document),
		expiresAt: now.Add(c.ttl),
	}
	return cloneEventSchema(document), nil
}

func cacheKey(eventType, version string) string {
	return eventType + "@" + version
}

func cloneEventSchema(document *EventSchema) *EventSchema {
	if document == nil {
		return nil
	}

	cloned := *document
	cloned.Schema = append(json.RawMessage(nil), document.Schema...)
	return &cloned
}
