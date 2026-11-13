package sniffer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	proxyBufferSize    = 32 * 1024
	proxyDialTimeout   = 10 * time.Second
	proxyHeaderTimeout = 20 * time.Second
	proxyMaxRedirects  = 5
)

var (
	errProxyURLInvalid = errors.New("a valid HTTP(S) media URL is required")
	errProxyBlocked    = errors.New("media host is not allowed")
)

// mediaProxy streams upstream media so browsers can play hotlink-protected
// links without hardcoding a Referer or hitting CORS restrictions.
type mediaProxy struct {
	client       *http.Client
	referer      string
	userAgent    string
	allowPrivate bool
}

func newMediaProxy(userAgent string) *mediaProxy {
	dialer := &net.Dialer{Timeout: proxyDialTimeout, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = safeDialContext(dialer, envProxyHosts())
	transport.ResponseHeaderTimeout = proxyHeaderTimeout
	transport.DisableCompression = true

	proxy := &mediaProxy{
		client: &http.Client{
			Transport: transport,
		},
		referer:   "https://www.bilibili.com/",
		userAgent: userAgent,
	}
	proxy.client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= proxyMaxRedirects {
			return errors.New("too many redirects")
		}
		return validateMediaURL(request.URL)
	}
	return proxy
}

func (p *mediaProxy) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	rawURL := request.URL.Query().Get("url")
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(writer, errProxyURLInvalid.Error(), http.StatusBadRequest)
		return
	}
	if !p.allowPrivate {
		if err := validateMediaURL(parsed); err != nil {
			http.Error(writer, errProxyBlocked.Error(), http.StatusForbidden)
			return
		}
	}

	proxyRequest, err := http.NewRequestWithContext(request.Context(), http.MethodGet, parsed.String(), nil)
	if err != nil {
		http.Error(writer, errProxyURLInvalid.Error(), http.StatusBadRequest)
		return
	}
	proxyRequest.Header.Set("User-Agent", p.userAgent)
	for _, header := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if value := request.Header.Get(header); value != "" {
			proxyRequest.Header.Set(header, value)
		}
	}
	if isHotlinkProtectedHost(parsed.Hostname()) {
		proxyRequest.Header.Set("Referer", p.referer)
	}

	response, err := p.client.Do(proxyRequest)
	if err != nil {
		http.Error(writer, "media request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	for _, header := range []string{
		"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges",
		"Last-Modified", "ETag", "Cache-Control",
	} {
		if value := response.Header.Get(header); value != "" {
			writer.Header().Set(header, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.CopyBuffer(writer, response.Body, make([]byte, proxyBufferSize))
}

func validateMediaURL(parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errProxyURLInvalid
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errProxyBlocked
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return errProxyBlocked
	}
	return nil
}

func safeDialContext(dialer *net.Dialer, allowed map[string]bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		// An egress proxy configured through the environment is expected to be
		// a private address, so it bypasses the SSRF checks.
		if allowed[strings.ToLower(host)] {
			return dialer.DialContext(ctx, network, address)
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, candidate := range addresses {
			if isBlockedIP(candidate.IP) {
				continue
			}
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if err == nil {
				return connection, nil
			}
		}
		return nil, fmt.Errorf("no allowed address for %s", host)
	}
}

func envProxyHosts() map[string]bool {
	hosts := make(map[string]bool)
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
			hosts[strings.ToLower(parsed.Hostname())] = true
		}
	}
	return hosts
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	// Carrier-grade NAT 100.64.0.0/10.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true
	}
	return false
}

func isHotlinkProtectedHost(host string) bool {
	host = strings.ToLower(host)
	return host == "bilivideo.com" || strings.HasSuffix(host, ".bilivideo.com") ||
		host == "bilibili.com" || strings.HasSuffix(host, ".bilibili.com")
}

// proxyMediaURL wraps a playback URL with this service so the browser can
// stream it with CORS and the required Referer.
func proxyMediaURL(baseURL, mediaURL string) string {
	if baseURL == "" {
		return mediaURL
	}
	return baseURL + "/play?url=" + url.QueryEscape(mediaURL)
}

func requestBaseURL(request *http.Request) string {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = strings.ToLower(strings.TrimSpace(strings.Split(forwarded, ",")[0]))
	}
	host := request.Host
	if forwarded := request.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}
