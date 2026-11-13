package sniffer

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	maxPlaylistSize = 2 << 20
	playlistTimeout = 10 * time.Second
)

// acceptsDuration reports whether a captured playback URL is long enough.
// Only HLS manifests can be measured reliably; live streams and formats
// whose duration cannot be determined are allowed through.
func (b *Browser) acceptsDuration(ctx context.Context, rawURL string) bool {
	if b.minDuration <= 0 {
		return true
	}
	if parsed, err := url.Parse(rawURL); err == nil {
		ext := strings.ToLower(parsed.Path[strings.LastIndex(parsed.Path, ".")+1:])
		switch ext {
		case "mp4", "m4v", "webm", "mkv", "flv", "mov", "avi", "wmv", "3gp", "ogv", "f4v", "m2ts", "mts",
			"m4a", "aac", "mp3", "ogg", "oga", "opus", "wav", "flac", "m4s", "ts", "mpd", "f4m", "ism":
			return true
		}
	}
	total, determined := hlsDuration(ctx, rawURL, b.userAgent)
	if !determined {
		return true
	}
	return total >= b.minDuration
}

// sourcePlaylistURL replaces an expiring redirect wrapper with the origin HLS
// playlist when the standard index.m3u8 exists beside its segments. The origin
// playlist must reference the same segment, otherwise the wrapper is retained.
func (b *Browser) sourcePlaylistURL(ctx context.Context, rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	if ext != ".m3u8" && ext != ".m3u" {
		return rawURL
	}

	body, base, err := fetchPlaylist(ctx, rawURL, b.userAgent)
	if err != nil || !isHLSPlaylist(body) {
		return rawURL
	}
	if _, master := masterVariant(body, base); master {
		return rawURL
	}
	segment := firstPlaylistSegment(body)
	if segment == "" {
		return rawURL
	}
	segmentReference, err := url.Parse(segment)
	if err != nil {
		return rawURL
	}
	segmentURL := base.ResolveReference(segmentReference)
	if segmentURL.Host == base.Host {
		return rawURL
	}

	indexURL := *segmentURL
	indexURL.Path = path.Join(path.Dir(segmentURL.Path), "index.m3u8")
	indexURL.RawPath = ""
	originURL := indexURL.String()
	originBody, originBase, err := fetchPlaylist(ctx, originURL, b.userAgent)
	if err != nil || !isHLSPlaylist(originBody) {
		return rawURL
	}
	if !playlistHasSegment(originBody, originBase, segmentURL) {
		return rawURL
	}
	return originURL
}

func firstPlaylistSegment(body []byte) string {
	for _, line := range playlistLines(body) {
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

func playlistHasSegment(body []byte, base, wanted *url.URL) bool {
	for _, line := range playlistLines(body) {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		reference, err := url.Parse(line)
		if err != nil {
			continue
		}
		if base.ResolveReference(reference).Path == wanted.Path {
			return true
		}
	}
	return false
}

func hlsDuration(ctx context.Context, rawURL, userAgent string) (time.Duration, bool) {
	body, base, err := fetchPlaylist(ctx, rawURL, userAgent)
	if err != nil {
		return 0, false
	}

	for depth := 0; depth < 3; depth++ {
		if !isHLSPlaylist(body) {
			return 0, false
		}
		if variant, ok := masterVariant(body, base); ok {
			body, base, err = fetchPlaylist(ctx, variant, userAgent)
			if err != nil {
				return 0, false
			}
			continue
		}
		total, live, known := mediaPlaylistDuration(body)
		if !known || live {
			return 0, false
		}
		return total, true
	}
	return 0, false
}

func fetchPlaylist(ctx context.Context, rawURL, userAgent string) ([]byte, *url.URL, error) {
	requestContext, cancel := context.WithTimeout(ctx, playlistTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("User-Agent", userAgent)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxPlaylistSize))
	if err != nil {
		return nil, nil, err
	}
	return body, response.Request.URL, nil
}

func isHLSPlaylist(body []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(body), []byte("#EXTM3U"))
}

// masterVariant returns the highest-bandwidth variant declared by a master
// playlist, resolved against the playlist URL.
func masterVariant(body []byte, base *url.URL) (string, bool) {
	lines := playlistLines(body)
	bestBandwidth := -1
	bestVariant := ""

	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			continue
		}
		bandwidth := -1
		for _, attribute := range strings.Split(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"), ",") {
			key, value, found := strings.Cut(attribute, "=")
			if !found || strings.TrimSpace(key) != "BANDWIDTH" {
				continue
			}
			if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				bandwidth = parsed
			}
		}
		for next := index + 1; next < len(lines); next++ {
			variant := lines[next]
			if variant == "" || strings.HasPrefix(variant, "#") {
				continue
			}
			if bandwidth > bestBandwidth {
				bestBandwidth = bandwidth
				bestVariant = variant
			}
			break
		}
	}

	if bestVariant == "" {
		return "", false
	}
	parsed, err := url.Parse(bestVariant)
	if err != nil {
		return "", false
	}
	return base.ResolveReference(parsed).String(), true
}

// mediaPlaylistDuration sums EXTINF entries. known is false when the playlist
// has no duration information; live is true when it has no ENDLIST tag.
func mediaPlaylistDuration(body []byte) (total time.Duration, live, known bool) {
	seconds := 0.0
	segments := 0
	endList := false

	for _, line := range playlistLines(body) {
		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			value := strings.TrimPrefix(line, "#EXTINF:")
			if comma := strings.Index(value, ","); comma >= 0 {
				value = value[:comma]
			}
			if duration, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				seconds += duration
				segments++
			}
		case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
			endList = true
		}
	}

	if segments == 0 {
		return 0, !endList, false
	}
	return time.Duration(seconds * float64(time.Second)), !endList, true
}

func playlistLines(body []byte) []string {
	rawLines := strings.Split(string(body), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, strings.TrimSpace(strings.TrimSuffix(line, "\r")))
	}
	return lines
}
