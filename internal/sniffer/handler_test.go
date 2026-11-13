package sniffer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestCORSHeaders(t *testing.T) {
	t.Parallel()
	handler := NewHandler(finderFunc(func(context.Context, string) (string, error) { return "", nil }), time.Second, 1)

	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, httptest.NewRequest(http.MethodOptions, "/node.js?url=https%3A%2F%2Fexample.com%2Fwatch", nil))
	if preflight.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", preflight.Code, http.StatusNoContent)
	}
	if got := preflight.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin = %q", got)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestSniffHandlerProxiesBilibiliMedia(t *testing.T) {
	t.Parallel()
	handler := NewHandler(finderFunc(func(context.Context, string) (string, error) {
		return "https://cn-sh.bilivideo.com/video/1.mp4?deadline=1", nil
	}), time.Second, 1)
	request := httptest.NewRequest(http.MethodGet, "/node.js?url=https%3A%2F%2Fwww.bilibili.com%2Fvideo%2FBV1xx411c7mD", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var body apiResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "http://example.com/play?url=https%3A%2F%2Fcn-sh.bilivideo.com%2Fvideo%2F1.mp4%3Fdeadline%3D1"
	if body.URL != want {
		t.Fatalf("url = %q, want %q", body.URL, want)
	}
}

func TestSniffHandlerKeepsRawURLOptOut(t *testing.T) {
	t.Parallel()
	handler := NewHandler(finderFunc(func(context.Context, string) (string, error) {
		return "https://cn-sh.bilivideo.com/video/1.mp4", nil
	}), time.Second, 1)
	request := httptest.NewRequest(http.MethodGet, "/node.js?proxy=0&url=https%3A%2F%2Fwww.bilibili.com%2Fvideo%2FBV1xx411c7mD", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var body apiResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.URL != "https://cn-sh.bilivideo.com/video/1.mp4" {
		t.Fatalf("url = %q", body.URL)
	}
}

func TestPlayProxyStreamsMedia(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("Accept-Ranges", "bytes")
		if request.Header.Get("Range") == "bytes=0-3" {
			writer.Header().Set("Content-Range", "bytes 0-3/10")
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write([]byte("ABCD"))
			return
		}
		_, _ = writer.Write([]byte("0123456789"))
	}))
	defer upstream.Close()

	handler := newHandler(
		finderFunc(func(context.Context, string) (string, error) { return "", nil }),
		time.Second, 1,
		&mediaProxy{client: upstream.Client(), referer: "https://www.bilibili.com/", userAgent: DefaultUserAgent, allowPrivate: true},
	)
	request := httptest.NewRequest(http.MethodGet, "/play?url="+url.QueryEscape(upstream.URL+"/video.mp4"), nil)
	request.Header.Set("Range", "bytes=0-3")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusPartialContent)
	}
	if response.Body.String() != "ABCD" {
		t.Fatalf("body = %q", response.Body.String())
	}
	if response.Header().Get("Content-Range") != "bytes 0-3/10" {
		t.Fatalf("content range = %q", response.Header().Get("Content-Range"))
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing CORS header")
	}
}

func TestPlayProxyBlocksPrivateHosts(t *testing.T) {
	t.Parallel()
	handler := NewHandler(finderFunc(func(context.Context, string) (string, error) { return "", nil }), time.Second, 1)
	request := httptest.NewRequest(http.MethodGet, "/play?url="+url.QueryEscape("http://127.0.0.1:8080/video.mp4"), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
