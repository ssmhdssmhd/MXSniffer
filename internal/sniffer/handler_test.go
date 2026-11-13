package sniffer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type finderFunc func(context.Context, string) (string, error)

func (f finderFunc) Sniff(ctx context.Context, rawURL string) (string, error) {
	return f(ctx, rawURL)
}

func TestSniffHandler(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		query      string
		finder     mediaFinder
		wantStatus int
		wantURL    string
	}{
		{
			name:  "success",
			query: "?url=https%3A%2F%2Fexample.com%2Fwatch",
			finder: finderFunc(func(context.Context, string) (string, error) {
				return "https://cdn.example.com/master.m3u8", nil
			}),
			wantStatus: http.StatusOK,
			wantURL:    "https://cdn.example.com/master.m3u8",
		},
		{
			name:       "missing URL",
			finder:     finderFunc(func(context.Context, string) (string, error) { return "", nil }),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "invalid URL",
			query: "?url=not-a-url",
			finder: finderFunc(func(context.Context, string) (string, error) {
				return "", ErrInvalidURL
			}),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "not found",
			query: "?url=https%3A%2F%2Fexample.com%2Fwatch",
			finder: finderFunc(func(context.Context, string) (string, error) {
				return "", ErrNotFound
			}),
			wantStatus: http.StatusNotFound,
		},
		{
			name:  "browser failure",
			query: "?url=https%3A%2F%2Fexample.com%2Fwatch",
			finder: finderFunc(func(context.Context, string) (string, error) {
				return "", errors.New("connection refused")
			}),
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := NewHandler(test.finder, time.Second, 1)
			request := httptest.NewRequest(http.MethodGet, "/node.js"+test.query, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			var body apiResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Code != test.wantStatus || body.URL != test.wantURL {
				t.Fatalf("response = %+v, want code %d and URL %q", body, test.wantStatus, test.wantURL)
			}
		})
	}
}

func TestSniffHandlerTimeout(t *testing.T) {
	t.Parallel()
	finder := finderFunc(func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	handler := NewHandler(finder, 5*time.Millisecond, 1)
	request := httptest.NewRequest(http.MethodGet, "/node.js?url=https%3A%2F%2Fexample.com%2Fwatch", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusGatewayTimeout)
	}
}

func TestHealthHandler(t *testing.T) {
	t.Parallel()
	handler := NewHandler(finderFunc(func(context.Context, string) (string, error) { return "", nil }), time.Second, 1)
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}
