package sniffer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

func newTestResolver(server *httptest.Server) *officialResolver {
	jar, _ := cookiejar.New(nil)
	return &officialResolver{
		client:    &http.Client{Jar: jar},
		homeURL:   server.URL + "/",
		viewAPI:   server.URL + "/view",
		playAPI:   server.URL + "/play",
		userAgent: DefaultUserAgent,
	}
}

func TestOfficialResolverUsesBilibiliAPI(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie("buvid3")
		switch request.URL.Path {
		case "/":
			http.SetCookie(writer, &http.Cookie{Name: "buvid3", Value: "test", Path: "/"})
			fmt.Fprint(writer, "<html></html>")
		case "/view":
			if err != nil || cookie.Value != "test" {
				fmt.Fprint(writer, `{"code":-412,"message":"blocked"}`)
				return
			}
			fmt.Fprint(writer, `{"code":0,"data":{"bvid":"BV1xx411c7mD","cid":62131}}`)
		case "/play":
			if got := request.URL.Query().Get("cid"); got != "62131" {
				fmt.Fprintf(writer, `{"code":-400,"message":"cid %s"}`, got)
				return
			}
			fmt.Fprint(writer, `{"code":0,"data":{"durl":[{"url":"https://cdn.example.com/official.mp4"}]}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	mediaURL, err := newTestResolver(server).resolveBvid(context.Background(), "BV1xx411c7mD")
	if err != nil {
		t.Fatalf("resolveBvid: %v", err)
	}
	if mediaURL != "https://cdn.example.com/official.mp4" {
		t.Fatalf("mediaURL = %q", mediaURL)
	}
}

func TestOfficialResolverFallsBackToDash(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			fmt.Fprint(writer, "<html></html>")
		case "/view":
			fmt.Fprint(writer, `{"code":0,"data":{"cid":1}}`)
		case "/play":
			fmt.Fprint(writer, `{"code":0,"data":{"durl":[],"dash":{"video":[{"baseUrl":"https://cdn.example.com/dash.m4s"}]}}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	mediaURL, err := newTestResolver(server).resolveBvid(context.Background(), "BV1xx411c7mD")
	if err != nil {
		t.Fatalf("resolveBvid: %v", err)
	}
	if mediaURL != "https://cdn.example.com/dash.m4s" {
		t.Fatalf("mediaURL = %q", mediaURL)
	}
}

func TestOfficialResolverRejectsUnsupportedHost(t *testing.T) {
	t.Parallel()
	_, err := (&officialResolver{}).resolve(context.Background(), "https://example.com/video/123")
	if !errors.Is(err, ErrOfficialUnsupported) {
		t.Fatalf("err = %v, want ErrOfficialUnsupported", err)
	}
}
