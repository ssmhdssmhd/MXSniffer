package sniffer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestMediaPlaylistDuration(t *testing.T) {
	t.Parallel()
	body := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10.5,\nseg0.ts\n#EXTINF:9.5,\nseg1.ts\n#EXT-X-ENDLIST\n")
	total, live, known := mediaPlaylistDuration(body)
	if !known {
		t.Fatal("expected known duration")
	}
	if live {
		t.Fatal("expected VOD playlist")
	}
	if total != 20*time.Second {
		t.Fatalf("total = %v, want 20s", total)
	}
}

func TestMediaPlaylistDurationLive(t *testing.T) {
	t.Parallel()
	body := []byte("#EXTM3U\n#EXTINF:6,\nseg0.ts\n#EXTINF:6,\nseg1.ts\n")
	_, live, known := mediaPlaylistDuration(body)
	if !known || !live {
		t.Fatalf("known = %v, live = %v, want true/true", known, live)
	}
}

func TestMediaPlaylistDurationUnknown(t *testing.T) {
	t.Parallel()
	_, _, known := mediaPlaylistDuration([]byte("#EXTM3U\n#EXT-X-ENDLIST\n"))
	if known {
		t.Fatal("expected unknown duration without EXTINF")
	}
}

func TestMasterVariantPrefersHighestBandwidth(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://cdn.example.com/path/master.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("#EXTM3U\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360\nlow/index.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720\nhigh/index.m3u8\n")
	variant, ok := masterVariant(body, base)
	if !ok {
		t.Fatal("expected a variant")
	}
	if variant != "https://cdn.example.com/path/high/index.m3u8" {
		t.Fatalf("variant = %q", variant)
	}
}

func TestHLSDurationFollowsMasterPlaylist(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		switch request.URL.Path {
		case "/master.m3u8":
			fmt.Fprint(writer, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000\nvariant/index.m3u8\n")
		case "/variant/index.m3u8":
			fmt.Fprint(writer, "#EXTM3U\n#EXTINF:40,\na.ts\n#EXTINF:40,\nb.ts\n#EXTINF:40,\nc.ts\n#EXT-X-ENDLIST\n")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	total, determined := hlsDuration(context.Background(), server.URL+"/master.m3u8", DefaultUserAgent)
	if !determined {
		t.Fatal("expected duration to be determined")
	}
	if total != 120*time.Second {
		t.Fatalf("total = %v, want 2m", total)
	}
}

func TestAcceptsDuration(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/short.m3u8":
			fmt.Fprint(writer, "#EXTM3U\n#EXTINF:4,\na.ts\n#EXTINF:4,\nb.ts\n#EXT-X-ENDLIST\n")
		case "/long.m3u8":
			fmt.Fprint(writer, "#EXTM3U\n#EXTINF:40,\na.ts\n#EXTINF:40,\nb.ts\n#EXT-X-ENDLIST\n")
		case "/live.m3u8":
			fmt.Fprint(writer, "#EXTM3U\n#EXTINF:4,\na.ts\n#EXTINF:4,\nb.ts\n")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	browser := &Browser{minDuration: time.Minute, userAgent: DefaultUserAgent}
	if browser.acceptsDuration(context.Background(), server.URL+"/short.m3u8") {
		t.Fatal("short playlist should be rejected")
	}
	if !browser.acceptsDuration(context.Background(), server.URL+"/long.m3u8") {
		t.Fatal("long playlist should be accepted")
	}
	if !browser.acceptsDuration(context.Background(), server.URL+"/live.m3u8") {
		t.Fatal("live playlist should be accepted")
	}
	if !browser.acceptsDuration(context.Background(), "https://cdn.example.com/movie.mp4") {
		t.Fatal("non-HLS media should be accepted without duration check")
	}
}

func TestSourcePlaylistURL(t *testing.T) {
	t.Parallel()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/movie/hls/index.m3u8" {
			http.NotFound(writer, request)
			return
		}
		fmt.Fprint(writer, "#EXTM3U\n#EXTINF:4,\nsegment0.ts\n#EXT-X-ENDLIST\n")
	}))
	defer origin.Close()

	wrapper := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/wrapped.m3u8":
			fmt.Fprintf(writer, "#EXTM3U\n#EXTINF:4,\n%s/movie/hls/segment0.ts\n#EXT-X-ENDLIST\n", origin.URL)
		case "/missing.m3u8":
			fmt.Fprintf(writer, "#EXTM3U\n#EXTINF:4,\n%s/missing/segment0.ts\n#EXT-X-ENDLIST\n", origin.URL)
		case "/mismatch.m3u8":
			fmt.Fprintf(writer, "#EXTM3U\n#EXTINF:4,\n%s/movie/hls/other.ts\n#EXT-X-ENDLIST\n", origin.URL)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer wrapper.Close()

	browser := &Browser{userAgent: DefaultUserAgent}
	if got := browser.sourcePlaylistURL(context.Background(), wrapper.URL+"/wrapped.m3u8"); got != origin.URL+"/movie/hls/index.m3u8" {
		t.Fatalf("sourcePlaylistURL() = %q, want origin index", got)
	}
	for _, path := range []string{"/missing.m3u8", "/mismatch.m3u8"} {
		rawURL := wrapper.URL + path
		if got := browser.sourcePlaylistURL(context.Background(), rawURL); got != rawURL {
			t.Fatalf("sourcePlaylistURL(%q) = %q, want original wrapper", path, got)
		}
	}
}

func TestSourcePlaylistURLLeavesOtherFormats(t *testing.T) {
	t.Parallel()
	browser := &Browser{userAgent: DefaultUserAgent}
	rawURL := "https://cdn.example.com/movie.mp4"
	if got := browser.sourcePlaylistURL(context.Background(), rawURL); got != rawURL {
		t.Fatalf("sourcePlaylistURL() = %q, want %q", got, rawURL)
	}
}
