package sniffer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const DefaultUserAgent = "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

const bestMediaScore = 3
const mediaCacheCapacity = 256

type mediaCandidate struct {
	url   string
	score int
}

type mediaCacheEntry struct {
	url       string
	expiresAt time.Time
}

var (
	ErrInvalidURL = errors.New("a valid HTTP(S) URL parameter is required")
	ErrNotFound   = errors.New("failed to extract video URL")
)

type BrowserConfig struct {
	BrowserURL     string
	ParserTemplate string
	UserAgent      string
	AttemptWait    time.Duration
	MinDuration    time.Duration
	CacheTTL       time.Duration
}

type Browser struct {
	browserURL     string
	parserTemplate string
	userAgent      string
	attemptWait    time.Duration
	minDuration    time.Duration
	cacheTTL       time.Duration
	cacheMutex     sync.Mutex
	cache          map[string]mediaCacheEntry
	official       *officialResolver
}

func NewBrowser(cfg BrowserConfig) (*Browser, error) {
	browserURL, err := url.Parse(cfg.BrowserURL)
	if err != nil || browserURL.Host == "" || (browserURL.Scheme != "ws" && browserURL.Scheme != "wss") {
		return nil, errors.New("LIGHTPANDA_WS_URL must be a valid ws:// or wss:// URL")
	}
	if !strings.Contains(cfg.ParserTemplate, "{url}") {
		return nil, errors.New("PARSER_URL_TEMPLATE must contain {url}")
	}
	if cfg.AttemptWait <= 0 {
		return nil, errors.New("ATTEMPT_WAIT must be positive")
	}
	if cfg.MinDuration < 0 {
		return nil, errors.New("MIN_MEDIA_DURATION must not be negative")
	}
	if cfg.CacheTTL < 0 {
		return nil, errors.New("CACHE_TTL must not be negative")
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = DefaultUserAgent
	}

	browser := &Browser{
		browserURL:     cfg.BrowserURL,
		parserTemplate: cfg.ParserTemplate,
		userAgent:      cfg.UserAgent,
		attemptWait:    cfg.AttemptWait,
		minDuration:    cfg.MinDuration,
		cacheTTL:       cfg.CacheTTL,
		cache:          make(map[string]mediaCacheEntry),
		official:       newOfficialResolver(cfg.UserAgent),
	}
	if _, err := browser.targetURL("https://example.com/video"); err != nil {
		return nil, err
	}
	return browser, nil
}

func (b *Browser) Sniff(ctx context.Context, rawURL string) (string, error) {
	normalized, err := normalizeHTTPURL(rawURL)
	if err != nil {
		return "", err
	}
	normalized = unwrapTargetURL(normalized)
	if isMediaURL(normalized, "") {
		return normalized, nil
	}
	if cached := b.cachedMediaURL(normalized); cached != "" {
		return cached, nil
	}

	ctx = withPlaylistCache(ctx)
	mediaURL, err := b.sniffNormalized(ctx, normalized)
	if err == nil {
		b.storeMediaURL(normalized, mediaURL)
	}
	return mediaURL, err
}

func (b *Browser) sniffNormalized(ctx context.Context, normalized string) (string, error) {
	if mediaURL, err := b.official.resolve(ctx, normalized); err == nil {
		return mediaURL, nil
	}

	targetURL, err := b.targetURL(normalized)
	if err != nil {
		return "", err
	}

	allocatorContext, cancelAllocator := chromedp.NewRemoteAllocator(ctx, b.browserURL)
	defer cancelAllocator()
	browserContext, cancelBrowser := chromedp.NewContext(allocatorContext, chromedp.WithNewBrowserContext())
	defer cancelBrowser()

	mediaURLs := make(chan mediaCandidate, 16)
	chromedp.ListenTarget(browserContext, func(event any) {
		candidate, mimeType := mediaEvent(event)
		score := mediaScore(candidate, mimeType)
		if score == 0 {
			return
		}
		select {
		case mediaURLs <- mediaCandidate{url: candidate, score: score}:
		default:
		}
	})

	if err := chromedp.Run(browserContext,
		network.Enable(),
		page.Enable(),
		emulation.SetUserAgentOverride(b.userAgent),
	); err != nil {
		return "", fmt.Errorf("initialize Lightpanda session: %w", err)
	}

	var pending []mediaCandidate
	seen := make(map[string]bool)
	rejected := make(map[string]bool)

	tryCandidate := func(candidate mediaCandidate) (string, bool) {
		if candidate.url == "" || seen[candidate.url] {
			return "", false
		}
		seen[candidate.url] = true
		// A single TS/fMP4 segment plays only a few seconds, so it is never a
		// valid answer while a minimum duration is enforced.
		if candidate.score < 2 && b.minDuration > 0 {
			slog.Debug("skip media segment", "url", candidate.url)
			return "", false
		}
		pending = append(pending, candidate)
		if candidate.score < bestMediaScore {
			return "", false
		}
		if b.acceptsDuration(ctx, candidate.url) {
			return b.sourcePlaylistURL(ctx, candidate.url), true
		}
		rejected[candidate.url] = true
		slog.Warn("media candidate rejected", "url", candidate.url, "reason", "duration")
		return "", false
	}

	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 0 {
			err = chromedp.Run(browserContext,
				chromedp.Navigate(targetURL),
				chromedp.WaitReady("body", chromedp.ByQuery),
			)
		} else {
			err = chromedp.Run(browserContext,
				chromedp.Reload(),
				chromedp.WaitReady("body", chromedp.ByQuery),
			)
		}
		if err != nil {
			return "", fmt.Errorf("load parser page: %w", err)
		}

		timer := time.NewTimer(b.attemptWait)
		waiting := true
		for waiting {
			select {
			case candidate := <-mediaURLs:
				if foundURL, ok := tryCandidate(candidate); ok {
					timer.Stop()
					return foundURL, nil
				}
			case <-timer.C:
				waiting = false
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			}
		}
	}

	sort.SliceStable(pending, func(i, j int) bool {
		return pending[i].score > pending[j].score
	})
	for _, candidate := range pending {
		if rejected[candidate.url] {
			continue
		}
		if b.acceptsDuration(ctx, candidate.url) {
			return b.sourcePlaylistURL(ctx, candidate.url), nil
		}
	}
	return "", ErrNotFound
}

func (b *Browser) cachedMediaURL(key string) string {
	if b.cacheTTL <= 0 {
		return ""
	}
	b.cacheMutex.Lock()
	defer b.cacheMutex.Unlock()
	entry, ok := b.cache[key]
	if !ok {
		return ""
	}
	if time.Now().After(entry.expiresAt) {
		delete(b.cache, key)
		return ""
	}
	return entry.url
}

func (b *Browser) storeMediaURL(key, mediaURL string) {
	if b.cacheTTL <= 0 || mediaURL == "" {
		return
	}
	now := time.Now()
	b.cacheMutex.Lock()
	defer b.cacheMutex.Unlock()
	for cachedKey, entry := range b.cache {
		if now.After(entry.expiresAt) {
			delete(b.cache, cachedKey)
		}
	}
	if len(b.cache) >= mediaCacheCapacity {
		for cachedKey := range b.cache {
			delete(b.cache, cachedKey)
			break
		}
	}
	b.cache[key] = mediaCacheEntry{url: mediaURL, expiresAt: now.Add(b.cacheTTL)}
}

func (b *Browser) targetURL(videoURL string) (string, error) {
	target := strings.ReplaceAll(b.parserTemplate, "{url}", url.QueryEscape(videoURL))
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("PARSER_URL_TEMPLATE must produce a valid HTTP(S) URL")
	}
	return target, nil
}

func normalizeHTTPURL(rawURL string) (string, error) {
	if len(rawURL) == 0 || len(rawURL) > 8192 {
		return "", ErrInvalidURL
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ErrInvalidURL
	}
	return parsed.String(), nil
}

func mediaEvent(event any) (string, string) {
	switch event := event.(type) {
	case *network.EventResponseReceived:
		return event.Response.URL, event.Response.MimeType
	case *network.EventRequestWillBeSent:
		return event.Request.URL, ""
	default:
		return "", ""
	}
}

func isMediaURL(rawURL, mimeType string) bool {
	return mediaScore(rawURL, mimeType) > 0
}

// mediaScore ranks playback candidates. Manifests score highest so a segment
// captured first cannot hide the real playlist; segments still match as a
// fallback when no manifest is available.
func mediaScore(rawURL, mimeType string) int {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return 0
	}

	ext := ""
	if index := strings.LastIndex(parsed.Path, "."); index >= 0 {
		ext = strings.ToLower(parsed.Path[index:])
	}
	switch ext {
	case ".m3u8", ".m3u", ".mpd", ".f4m", ".ism":
		return bestMediaScore
	case ".mp4", ".m4v", ".webm", ".mkv", ".flv", ".mov", ".avi", ".wmv", ".3gp", ".ogv", ".f4v", ".m2ts", ".mts",
		".m4a", ".aac", ".mp3", ".ogg", ".oga", ".opus", ".wav", ".flac":
		return 2
	case ".m4s", ".ts":
		return 1
	}

	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch {
	case mimeType == "application/vnd.apple.mpegurl",
		mimeType == "application/x-mpegurl",
		mimeType == "application/mpegurl",
		mimeType == "audio/mpegurl",
		mimeType == "audio/x-mpegurl",
		mimeType == "application/dash+xml",
		mimeType == "application/vnd.ms-sstr+xml",
		mimeType == "application/f4m",
		mimeType == "video/x-f4m":
		return bestMediaScore
	case strings.HasPrefix(mimeType, "video/"), strings.HasPrefix(mimeType, "audio/"):
		return 2
	default:
		return 0
	}
}
