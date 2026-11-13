package sniffer

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// maxUnwrapDepth bounds nested base64 wrappers so malformed input cannot loop.
const maxUnwrapDepth = 3

// unwrapTargetURL resolves web player wrappers whose real target is carried in
// a base64 query parameter. It currently recognises ssmhd.com/player/?url=...
// which embeds the decoded address in an iframe that a headless browser may
// not navigate to.
func unwrapTargetURL(rawURL string) string {
	for depth := 0; depth < maxUnwrapDepth; depth++ {
		target, ok := decodePlayerWrapper(rawURL)
		if !ok {
			return rawURL
		}
		rawURL = target
	}
	return rawURL
}

func decodePlayerWrapper(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "ssmhd.com" && host != "www.ssmhd.com" {
		return "", false
	}
	if strings.TrimSuffix(parsed.Path, "/") != "/player" {
		return "", false
	}
	value := parsed.Query().Get("url")
	if value == "" {
		return "", false
	}
	decoded, ok := decodeBase64(value)
	if !ok {
		return "", false
	}
	if !strings.HasPrefix(decoded, "http://") && !strings.HasPrefix(decoded, "https://") {
		return "", false
	}
	return decoded, true
}

func decodeBase64(value string) (string, bool) {
	// A literal '+' in a query string arrives as a space, so restore it before
	// trying the standard alphabet.
	candidates := []string{
		strings.ReplaceAll(value, " ", "+"),
		value,
	}
	for _, candidate := range candidates {
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			decoded, err := encoding.DecodeString(candidate)
			if err == nil && len(decoded) > 0 {
				return string(decoded), true
			}
		}
	}
	return "", false
}
