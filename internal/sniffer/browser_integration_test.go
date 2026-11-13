package sniffer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestLightpandaIntegration(t *testing.T) {
	if os.Getenv("LIGHTPANDA_INTEGRATION") != "1" {
		t.Skip("set LIGHTPANDA_INTEGRATION=1 to run the Lightpanda integration test")
	}
	browserURL := os.Getenv("LIGHTPANDA_WS_URL")
	if browserURL == "" {
		t.Fatal("LIGHTPANDA_WS_URL is required")
	}

	userAgents := make(chan string, 2)
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(writer http.ResponseWriter, request *http.Request) {
		userAgents <- request.UserAgent()
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte(`<!doctype html><html><body><script>fetch("/media/master.m3u8").then(function(r) { return r.text(); }).catch(function() {});</script></body></html>`))
	})
	mux.HandleFunc("/media/master.m3u8", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = writer.Write([]byte("#EXTM3U\n#EXT-X-VERSION:3\n"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	browser, err := NewBrowser(BrowserConfig{
		BrowserURL:     browserURL,
		ParserTemplate: server.URL + "/page?source={url}",
		AttemptWait:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBrowser() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got, err := browser.Sniff(ctx, "https://example.com/watch?v=1")
	if err != nil {
		t.Fatalf("Sniff() error = %v", err)
	}
	want := server.URL + "/media/master.m3u8"
	if got != want {
		t.Fatalf("Sniff() = %q, want %q", got, want)
	}
	select {
	case userAgent := <-userAgents:
		if userAgent != DefaultUserAgent {
			t.Fatalf("User-Agent = %q, want %q", userAgent, DefaultUserAgent)
		}
	default:
		t.Fatal("parser page was not requested")
	}
}
