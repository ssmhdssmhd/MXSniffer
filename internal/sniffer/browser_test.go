package sniffer

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestIsMediaURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		rawURL   string
		mimeType string
		want     bool
	}{
		{name: "m3u8", rawURL: "https://cdn.example.com/video/master.m3u8?token=abc", want: true},
		{name: "uppercase extension", rawURL: "https://cdn.example.com/VIDEO.MP4", want: true},
		{name: "m3u8 mime", rawURL: "https://cdn.example.com/playlist?id=1", mimeType: "application/vnd.apple.mpegurl; charset=utf-8", want: true},
		{name: "mp4 mime", rawURL: "https://cdn.example.com/stream", mimeType: "video/mp4", want: true},
		{name: "dash manifest", rawURL: "https://cdn.example.com/stream.mpd", want: true},
		{name: "dash mime", rawURL: "https://cdn.example.com/manifest", mimeType: "application/dash+xml", want: true},
		{name: "flv", rawURL: "https://cdn.example.com/live.flv?token=1", want: true},
		{name: "webm mime", rawURL: "https://cdn.example.com/stream", mimeType: "video/webm", want: true},
		{name: "m4s segment", rawURL: "https://cdn.example.com/video-1.m4s", want: true},
		{name: "ts segment", rawURL: "https://cdn.example.com/seg-1.ts", want: true},
		{name: "audio", rawURL: "https://cdn.example.com/track.m4a", want: true},
		{name: "hls manifest without extension", rawURL: "https://cdn.example.com/play/index", mimeType: "application/x-mpegURL", want: true},
		{name: "extension only in query", rawURL: "https://parser.example.com/watch?url=video.m3u8", want: false},
		{name: "ordinary page", rawURL: "https://example.com/watch?v=1", mimeType: "text/html", want: false},
		{name: "html with dots", rawURL: "https://example.com/v1.2/index", mimeType: "text/html; charset=utf-8", want: false},
		{name: "unsupported scheme", rawURL: "file:///tmp/video.mp4", want: false},
		{name: "invalid URL", rawURL: "://video.mp4", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isMediaURL(test.rawURL, test.mimeType); got != test.want {
				t.Fatalf("isMediaURL() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMediaScorePriority(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rawURL string
		want   int
	}{
		{rawURL: "https://cdn.example.com/master.m3u8", want: bestMediaScore},
		{rawURL: "https://cdn.example.com/manifest.mpd", want: bestMediaScore},
		{rawURL: "https://cdn.example.com/movie.mp4", want: 2},
		{rawURL: "https://cdn.example.com/segment.m4s", want: 1},
		{rawURL: "https://cdn.example.com/index.html", want: 0},
	}
	for _, test := range tests {
		if got := mediaScore(test.rawURL, ""); got != test.want {
			t.Fatalf("mediaScore(%q) = %d, want %d", test.rawURL, got, test.want)
		}
	}
}

func TestTargetURL(t *testing.T) {
	t.Parallel()
	browser, err := NewBrowser(BrowserConfig{
		BrowserURL:     "ws://127.0.0.1:9222",
		ParserTemplate: "https://parser.example.com/watch?url={url}",
		AttemptWait:    time.Second,
	})
	if err != nil {
		t.Fatalf("NewBrowser() error = %v", err)
	}

	got, err := browser.targetURL("https://example.com/watch?v=1&t=2")
	if err != nil {
		t.Fatalf("targetURL() error = %v", err)
	}
	want := "https://parser.example.com/watch?url=https%3A%2F%2Fexample.com%2Fwatch%3Fv%3D1%26t%3D2"
	if got != want {
		t.Fatalf("targetURL() = %q, want %q", got, want)
	}
}

func TestNewBrowserRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	valid := BrowserConfig{
		BrowserURL:     "ws://127.0.0.1:9222",
		ParserTemplate: "https://parser.example.com/?url={url}",
		AttemptWait:    time.Second,
	}

	tests := []struct {
		name   string
		mutate func(*BrowserConfig)
	}{
		{name: "browser URL", mutate: func(cfg *BrowserConfig) { cfg.BrowserURL = "http://127.0.0.1:9222" }},
		{name: "missing placeholder", mutate: func(cfg *BrowserConfig) { cfg.ParserTemplate = "https://parser.example.com" }},
		{name: "invalid parser scheme", mutate: func(cfg *BrowserConfig) { cfg.ParserTemplate = "file:///tmp/{url}" }},
		{name: "attempt wait", mutate: func(cfg *BrowserConfig) { cfg.AttemptWait = 0 }},
		{name: "cache TTL", mutate: func(cfg *BrowserConfig) { cfg.CacheTTL = -time.Second }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := valid
			test.mutate(&cfg)
			if _, err := NewBrowser(cfg); err == nil {
				t.Fatal("NewBrowser() expected an error")
			}
		})
	}
}

func TestSniffReturnsDirectMediaWithoutBrowser(t *testing.T) {
	t.Parallel()
	browser, err := NewBrowser(BrowserConfig{
		BrowserURL:     "ws://127.0.0.1:1",
		ParserTemplate: "https://parser.example.com/?url={url}",
		AttemptWait:    time.Second,
	})
	if err != nil {
		t.Fatalf("NewBrowser() error = %v", err)
	}

	const mediaURL = "https://cdn.example.com/master.m3u8?token=abc"
	got, err := browser.Sniff(context.Background(), mediaURL)
	if err != nil {
		t.Fatalf("Sniff() error = %v", err)
	}
	if got != mediaURL {
		t.Fatalf("Sniff() = %q, want %q", got, mediaURL)
	}
}

func TestMediaCache(t *testing.T) {
	t.Parallel()
	browser := &Browser{cacheTTL: time.Minute, cache: make(map[string]mediaCacheEntry)}
	const key = "https://example.com/watch?v=1"
	const mediaURL = "https://cdn.example.com/master.m3u8"
	if got := browser.cachedMediaURL(key); got != "" {
		t.Fatalf("empty cache returned %q", got)
	}
	browser.storeMediaURL(key, mediaURL)
	if got := browser.cachedMediaURL(key); got != mediaURL {
		t.Fatalf("cachedMediaURL() = %q, want %q", got, mediaURL)
	}

	browser.cacheMutex.Lock()
	entry := browser.cache[key]
	entry.expiresAt = time.Now().Add(-time.Second)
	browser.cache[key] = entry
	browser.cacheMutex.Unlock()
	if got := browser.cachedMediaURL(key); got != "" {
		t.Fatalf("expired cache returned %q", got)
	}

	for index := 0; index < mediaCacheCapacity+10; index++ {
		browser.storeMediaURL(fmt.Sprintf("%s-%d", key, index), mediaURL)
	}
	if len(browser.cache) > mediaCacheCapacity {
		t.Fatalf("cache size = %d, want <= %d", len(browser.cache), mediaCacheCapacity)
	}

	disabled := &Browser{cache: make(map[string]mediaCacheEntry)}
	disabled.storeMediaURL(key, mediaURL)
	if got := disabled.cachedMediaURL(key); got != "" {
		t.Fatalf("disabled cache returned %q", got)
	}
}
