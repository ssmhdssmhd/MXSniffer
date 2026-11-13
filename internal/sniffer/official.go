package sniffer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	bilibiliHome = "https://www.bilibili.com/"
	bilibiliView = "https://api.bilibili.com/x/web-interface/view"
	bilibiliPlay = "https://api.bilibili.com/x/player/playurl"
)

var (
	ErrOfficialUnsupported = errors.New("official resolver does not support this URL")
	bvidPattern            = regexp.MustCompile(`BV[0-9A-Za-z]{10}`)
)

type officialResolver struct {
	client    *http.Client
	homeURL   string
	viewAPI   string
	playAPI   string
	userAgent string
}

type bilibiliViewResponse struct {
	Code int `json:"code"`
	Data struct {
		Bvid string `json:"bvid"`
		Cid  int64  `json:"cid"`
	} `json:"data"`
}

type bilibiliPlayResponse struct {
	Code int `json:"code"`
	Data struct {
		Durl []struct {
			URL string `json:"url"`
		} `json:"durl"`
		Dash struct {
			Video []struct {
				BaseURL string `json:"baseUrl"`
			} `json:"video"`
		} `json:"dash"`
	} `json:"data"`
}

func newOfficialResolver(userAgent string) *officialResolver {
	jar, _ := cookiejar.New(nil)
	return &officialResolver{
		client:    &http.Client{Jar: jar, Timeout: 12 * time.Second},
		homeURL:   bilibiliHome,
		viewAPI:   bilibiliView,
		playAPI:   bilibiliPlay,
		userAgent: userAgent,
	}
}

// resolve returns an official playback URL for supported platforms. It is
// best-effort: callers fall back to the parser page on any error.
func (r *officialResolver) resolve(ctx context.Context, rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", ErrOfficialUnsupported
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "b23.tv" {
		parsed, err = r.followRedirect(ctx, rawURL)
		if err != nil {
			return "", err
		}
		host = strings.ToLower(parsed.Hostname())
	}
	if host != "bilibili.com" && !strings.HasSuffix(host, ".bilibili.com") {
		return "", ErrOfficialUnsupported
	}

	bvid := bvidPattern.FindString(parsed.Path)
	if bvid == "" {
		return "", ErrOfficialUnsupported
	}
	return r.resolveBvid(ctx, bvid)
}

func (r *officialResolver) resolveBvid(ctx context.Context, bvid string) (string, error) {
	// A first visit sets the buvid cookies required by the public APIs.
	if _, err := r.get(ctx, r.homeURL); err != nil {
		return "", fmt.Errorf("prime bilibili cookies: %w", err)
	}

	var view bilibiliViewResponse
	if err := r.getJSON(ctx, r.viewAPI+"?bvid="+url.QueryEscape(bvid), &view); err != nil {
		return "", err
	}
	if view.Code != 0 || view.Data.Cid == 0 {
		return "", fmt.Errorf("bilibili view api returned code %d", view.Code)
	}

	playURL := fmt.Sprintf("%s?bvid=%s&cid=%d&qn=64&fnval=1&fourk=1",
		r.playAPI, url.QueryEscape(bvid), view.Data.Cid)
	var play bilibiliPlayResponse
	if err := r.getJSON(ctx, playURL, &play); err != nil {
		return "", err
	}
	if play.Code != 0 {
		return "", fmt.Errorf("bilibili play api returned code %d", play.Code)
	}
	for _, segment := range play.Data.Durl {
		if segment.URL != "" {
			return segment.URL, nil
		}
	}
	for _, stream := range play.Data.Dash.Video {
		if stream.BaseURL != "" {
			return stream.BaseURL, nil
		}
	}
	return "", errors.New("bilibili play api returned no media URL")
}

func (r *officialResolver) followRedirect(ctx context.Context, rawURL string) (*url.URL, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", r.userAgent)
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return response.Request.URL, nil
}

func (r *officialResolver) get(ctx context.Context, rawURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", r.userAgent)
	request.Header.Set("Referer", bilibiliHome)

	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("request %s: status %d", rawURL, response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxPlaylistSize))
}

func (r *officialResolver) getJSON(ctx context.Context, rawURL string, target any) error {
	body, err := r.get(ctx, rawURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}
