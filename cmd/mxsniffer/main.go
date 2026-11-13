package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ssmhdssmhd/MXSniffer/internal/sniffer"
)

const version = "0.0.11"

type config struct {
	listenAddr     string
	browserURL     string
	parserTemplate string
	userAgent      string
	requestTimeout time.Duration
	attemptWait    time.Duration
	minDuration    time.Duration
	cacheTTL       time.Duration
	maxConcurrent  int
}

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	browser, err := sniffer.NewBrowser(sniffer.BrowserConfig{
		BrowserURL:     cfg.browserURL,
		ParserTemplate: cfg.parserTemplate,
		UserAgent:      cfg.userAgent,
		AttemptWait:    cfg.attemptWait,
		MinDuration:    cfg.minDuration,
		CacheTTL:       cfg.cacheTTL,
	})
	if err != nil {
		return fmt.Errorf("configure browser: %w", err)
	}

	server := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           sniffer.NewHandler(browser, cfg.requestTimeout, cfg.maxConcurrent),
		ReadHeaderTimeout: 5 * time.Second,
		// Media proxying streams large files, so writes must not be capped.
		WriteTimeout:   0,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("MXSniffer started", "version", version, "address", cfg.listenAddr)
		serverErrors <- server.ListenAndServe()
	}()

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-signalContext.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	return nil
}

func loadConfig() (config, error) {
	cfg := config{
		listenAddr:     ":1314",
		browserURL:     "ws://127.0.0.1:9222",
		parserTemplate: "https://bfq.txnp.cn/player?url={url}",
		userAgent:      sniffer.DefaultUserAgent,
		requestTimeout: 45 * time.Second,
		attemptWait:    8 * time.Second,
		minDuration:    60 * time.Second,
		cacheTTL:       2 * time.Minute,
		maxConcurrent:  2,
	}

	cfg.listenAddr = environmentString("LISTEN_ADDR", cfg.listenAddr)
	cfg.browserURL = environmentString("LIGHTPANDA_WS_URL", cfg.browserURL)
	cfg.parserTemplate = environmentString("PARSER_URL_TEMPLATE", cfg.parserTemplate)
	cfg.userAgent = environmentString("USER_AGENT", cfg.userAgent)

	var err error
	if cfg.requestTimeout, err = environmentDuration("REQUEST_TIMEOUT", cfg.requestTimeout); err != nil {
		return config{}, err
	}
	if cfg.attemptWait, err = environmentDuration("ATTEMPT_WAIT", cfg.attemptWait); err != nil {
		return config{}, err
	}
	if cfg.minDuration, err = environmentOptionalDuration("MIN_MEDIA_DURATION", cfg.minDuration); err != nil {
		return config{}, err
	}
	if cfg.cacheTTL, err = environmentOptionalDuration("CACHE_TTL", cfg.cacheTTL); err != nil {
		return config{}, err
	}
	if cfg.maxConcurrent, err = environmentInt("MAX_CONCURRENT_REQUESTS", cfg.maxConcurrent); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func environmentString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func environmentDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}

func environmentOptionalDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative duration", key)
	}
	return parsed, nil
}

func environmentInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}
