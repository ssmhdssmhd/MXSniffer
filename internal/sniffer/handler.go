package sniffer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type mediaFinder interface {
	Sniff(context.Context, string) (string, error)
}

type api struct {
	finder         mediaFinder
	requestTimeout time.Duration
	slots          chan struct{}
}

type apiResponse struct {
	Code    int    `json:"code"`
	URL     string `json:"url,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

func NewHandler(finder mediaFinder, requestTimeout time.Duration, maxConcurrent int) http.Handler {
	handler := &api{
		finder:         finder,
		requestTimeout: requestTimeout,
		slots:          make(chan struct{}, maxConcurrent),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /node.js", handler.sniff)
	mux.HandleFunc("GET /healthz", handler.health)
	return mux
}

func (a *api) sniff(writer http.ResponseWriter, request *http.Request) {
	rawURL := request.URL.Query().Get("url")
	if rawURL == "" {
		writeJSON(writer, http.StatusBadRequest, apiResponse{
			Code:    http.StatusBadRequest,
			Message: "URL parameter is required",
		})
		return
	}

	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	case <-request.Context().Done():
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), a.requestTimeout)
	defer cancel()
	mediaURL, err := a.finder.Sniff(ctx, rawURL)
	if err == nil {
		writeJSON(writer, http.StatusOK, apiResponse{Code: http.StatusOK, URL: mediaURL})
		return
	}
	if request.Context().Err() != nil {
		return
	}

	status := http.StatusBadGateway
	message := "Browser extraction failed"
	switch {
	case errors.Is(err, ErrInvalidURL):
		status = http.StatusBadRequest
		message = "A valid HTTP(S) URL parameter is required"
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		message = "Failed to extract video URL"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
		message = "Sniffing timed out"
	default:
		slog.Error("media extraction failed", "error", err)
	}
	writeJSON(writer, status, apiResponse{Code: status, Message: message})
}

func (a *api) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, apiResponse{Code: http.StatusOK, Status: "ok"})
}

func writeJSON(writer http.ResponseWriter, status int, response apiResponse) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(response)
}
