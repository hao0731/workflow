package schema

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetEventSchemaByTypeAndVersion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/schemas/events/workflow.event.node.executed/versions/v1", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", "\"etag-v1\"")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"eventType": "workflow.event.node.executed",
			"version":   "v1",
			"schema": map[string]any{
				"type":     "object",
				"required": []string{"nodeid"},
			},
		}))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	require.NoError(t, err)

	doc, err := client.GetEventSchema(context.Background(), "workflow.event.node.executed", "v1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "workflow.event.node.executed", doc.EventType)
	assert.Equal(t, "v1", doc.Version)
	assert.Equal(t, "\"etag-v1\"", doc.ETag)
	assert.JSONEq(t, `{"type":"object","required":["nodeid"]}`, string(doc.Schema))
}

func TestCache_UsesTTLAndConditionalRefresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 6, 15, 0, 0, 0, time.UTC)
	lookup := &stubLookupClient{
		responses: []stubLookupResponse{
			{
				document: &EventSchema{
					EventType: "workflow.event.node.executed",
					Version:   "v1",
					Schema:    json.RawMessage(`{"required":["nodeid"]}`),
					ETag:      "\"etag-v1\"",
				},
			},
			{
				notModified: true,
			},
		},
	}

	cache := NewCache(
		lookup,
		2*time.Minute,
		WithClock(func() time.Time { return now }),
	)

	first, err := cache.GetEventSchema(context.Background(), "workflow.event.node.executed", "v1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"required":["nodeid"]}`, string(first.Schema))
	require.Len(t, lookup.calls, 1)
	assert.Equal(t, "", lookup.calls[0].ifNoneMatch)

	second, err := cache.GetEventSchema(context.Background(), "workflow.event.node.executed", "v1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"required":["nodeid"]}`, string(second.Schema))
	require.Len(t, lookup.calls, 1)

	now = now.Add(3 * time.Minute)

	third, err := cache.GetEventSchema(context.Background(), "workflow.event.node.executed", "v1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"required":["nodeid"]}`, string(third.Schema))
	require.Len(t, lookup.calls, 2)
	assert.Equal(t, "\"etag-v1\"", lookup.calls[1].ifNoneMatch)
}

func TestCache_RefreshesOnVersionOrETagChange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 6, 16, 0, 0, 0, time.UTC)
	lookup := &stubLookupClient{
		responses: []stubLookupResponse{
			{
				document: &EventSchema{
					EventType: "workflow.event.node.executed",
					Version:   "v1",
					Schema:    json.RawMessage(`{"required":["nodeid"]}`),
					ETag:      "\"etag-v1\"",
				},
			},
			{
				document: &EventSchema{
					EventType: "workflow.event.node.executed",
					Version:   "v2",
					Schema:    json.RawMessage(`{"required":["nodeid","attempt"]}`),
					ETag:      "\"etag-v2\"",
				},
			},
			{
				document: &EventSchema{
					EventType: "workflow.event.node.executed",
					Version:   "v1",
					Schema:    json.RawMessage(`{"required":["nodeid","outputPort"]}`),
					ETag:      "\"etag-v1b\"",
				},
			},
		},
	}

	cache := NewCache(
		lookup,
		2*time.Minute,
		WithClock(func() time.Time { return now }),
	)

	v1, err := cache.GetEventSchema(context.Background(), "workflow.event.node.executed", "v1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"required":["nodeid"]}`, string(v1.Schema))

	v2, err := cache.GetEventSchema(context.Background(), "workflow.event.node.executed", "v2")
	require.NoError(t, err)
	assert.JSONEq(t, `{"required":["nodeid","attempt"]}`, string(v2.Schema))
	require.Len(t, lookup.calls, 2)
	assert.Equal(t, "v1", lookup.calls[0].version)
	assert.Equal(t, "v2", lookup.calls[1].version)

	now = now.Add(3 * time.Minute)

	refreshed, err := cache.GetEventSchema(context.Background(), "workflow.event.node.executed", "v1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"required":["nodeid","outputPort"]}`, string(refreshed.Schema))
	require.Len(t, lookup.calls, 3)
	assert.Equal(t, "\"etag-v1\"", lookup.calls[2].ifNoneMatch)
}

type stubLookupClient struct {
	calls     []stubLookupCall
	responses []stubLookupResponse
}

type stubLookupCall struct {
	eventType   string
	version     string
	ifNoneMatch string
}

type stubLookupResponse struct {
	document    *EventSchema
	notModified bool
	err         error
}

func (s *stubLookupClient) LookupEventSchema(_ context.Context, eventType, version, ifNoneMatch string) (*EventSchema, bool, error) {
	s.calls = append(s.calls, stubLookupCall{
		eventType:   eventType,
		version:     version,
		ifNoneMatch: ifNoneMatch,
	})

	if len(s.responses) == 0 {
		return nil, false, nil
	}

	response := s.responses[0]
	s.responses = s.responses[1:]
	return response.document, response.notModified, response.err
}
