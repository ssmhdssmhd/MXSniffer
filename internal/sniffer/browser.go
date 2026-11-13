package sniffer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const DefaultUserAgent = "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var (
	ErrInvalidURL = errors.New("a valid HTTP(S) URL parameter is required")
	ErrNotFound   = errors.New("failed to extract video URL")
)

type BrowserConfig struct {
	BrowserURL     string
	ParserTemplate string
	UserAgent      string
	AttemptWait    time.Duration
}

type Browser struct {
	browserURL     string
	parserTemplate string
	userAgent      string
	attemptWait    time.Duration
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
	if cfg.UserAgent == "" {
		cfg.UserAgent = DefaultUserAgent
	}

	browser := &Browser{
		browserURL:     cfg.BrowserURL,
		parserTemplate: cfg.ParserTemplate,
		userAgent:      cfg.UserAgent,
		attemptWait:    cfg.AttemptWait,
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
	if isMediaURL(normalized, "") {
		return normalized, nil
	}

	targetURL, err := b.targetURL(normalized)
	if err != nil {
		return "", err
	}

	allocatorContext, cancelAllocator := chromedp.NewRemoteAllocator(ctx, b.browserURL)
	defer cancelAllocator()
	browserContext, cancelBrowser := chromedp.NewContext(allocatorContext, chromedp.WithNewBrowserContext())
	defer cancelBrowser()

	mediaURLs := make(chan string, 1)
	chromedp.ListenTarget(browserContext, func(event any) {
		candidate, mimeType := mediaEvent(event)
		if candidate == "" || !isMediaURL(candidate, mimeType) {
			return
		}
		select {
		case mediaURLs <- candidate:
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
		select {
		case mediaURL := <-mediaURLs:
			timer.Stop()
			return mediaURL, nil
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		}
	}

	select {
	case mediaURL := <-mediaURLs:
		return mediaURL, nil
	default:
		return "", ErrNotFound
	}
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
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, ".mp4") {
		return true
	}

	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch mimeType {
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "application/mpegurl", "audio/mpegurl", "audio/x-mpegurl", "video/mp4", "application/mp4":
		return true
	default:
		return false
	}
}
