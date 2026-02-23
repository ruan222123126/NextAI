package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"nextai/apps/gateway/internal/app"
	"nextai/apps/gateway/internal/config"
)

const (
	envHTTPReadHeaderTimeoutSeconds = "NEXTAI_HTTP_READ_HEADER_TIMEOUT_SECONDS"
	envHTTPReadTimeoutSeconds       = "NEXTAI_HTTP_READ_TIMEOUT_SECONDS"
	envHTTPWriteTimeoutSeconds      = "NEXTAI_HTTP_WRITE_TIMEOUT_SECONDS"
	envHTTPIdleTimeoutSeconds       = "NEXTAI_HTTP_IDLE_TIMEOUT_SECONDS"
	envHTTPShutdownTimeoutSeconds   = "NEXTAI_HTTP_SHUTDOWN_TIMEOUT_SECONDS"
)

var (
	defaultHTTPReadHeaderTimeout = 10 * time.Second
	defaultHTTPReadTimeout       = 120 * time.Second
	defaultHTTPWriteTimeout      = 0 * time.Second
	defaultHTTPIdleTimeout       = 120 * time.Second
	defaultHTTPShutdownTimeout   = 30 * time.Second
)

type httpRuntimeConfig struct {
	readHeaderTimeout time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	shutdownTimeout   time.Duration
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("gateway exited with error: %v", err)
	}
}

func run() error {
	if path, loaded, err := loadEnvFile(); err != nil {
		log.Printf("load env file failed: path=%s err=%v", path, err)
	} else if loaded > 0 {
		log.Printf("loaded %d env values from %s", loaded, path)
	}

	cfg := config.Load()
	srv, err := app.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("init server failed: %w", err)
	}
	defer srv.Close()

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	runtimeCfg := loadHTTPRuntimeConfig()
	httpServer := newHTTPServer(addr, srv.Handler(), runtimeCfg)

	errCh := make(chan error, 1)
	go func() {
		if listenErr := httpServer.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- listenErr
			return
		}
		errCh <- nil
	}()

	log.Printf(
		"gateway listening on %s (read_header_timeout=%s read_timeout=%s write_timeout=%s idle_timeout=%s shutdown_timeout=%s)",
		addr,
		runtimeCfg.readHeaderTimeout,
		runtimeCfg.readTimeout,
		runtimeCfg.writeTimeout,
		runtimeCfg.idleTimeout,
		runtimeCfg.shutdownTimeout,
	)

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case listenErr := <-errCh:
		if listenErr != nil {
			return fmt.Errorf("listen failed: %w", listenErr)
		}
		return nil
	case <-signalCtx.Done():
		log.Printf("shutdown signal received, draining in-flight requests (timeout=%s)", runtimeCfg.shutdownTimeout)
	}

	timedOut, shutdownErr := shutdownHTTPServer(httpServer, runtimeCfg.shutdownTimeout)
	if shutdownErr != nil {
		return shutdownErr
	}
	if timedOut {
		log.Printf("gateway shutdown degraded: in-flight requests exceeded timeout=%s, forced close", runtimeCfg.shutdownTimeout)
	} else {
		log.Printf("gateway shutdown complete")
	}

	if listenErr := <-errCh; listenErr != nil {
		return fmt.Errorf("listen failed during shutdown: %w", listenErr)
	}
	return nil
}

func loadHTTPRuntimeConfig() httpRuntimeConfig {
	return httpRuntimeConfig{
		readHeaderTimeout: readDurationSecondsEnv(envHTTPReadHeaderTimeoutSeconds, defaultHTTPReadHeaderTimeout, false),
		readTimeout:       readDurationSecondsEnv(envHTTPReadTimeoutSeconds, defaultHTTPReadTimeout, false),
		writeTimeout:      readDurationSecondsEnv(envHTTPWriteTimeoutSeconds, defaultHTTPWriteTimeout, true),
		idleTimeout:       readDurationSecondsEnv(envHTTPIdleTimeoutSeconds, defaultHTTPIdleTimeout, false),
		shutdownTimeout:   readDurationSecondsEnv(envHTTPShutdownTimeoutSeconds, defaultHTTPShutdownTimeout, false),
	}
}

func newHTTPServer(addr string, handler http.Handler, runtimeCfg httpRuntimeConfig) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: runtimeCfg.readHeaderTimeout,
		ReadTimeout:       runtimeCfg.readTimeout,
		WriteTimeout:      runtimeCfg.writeTimeout,
		IdleTimeout:       runtimeCfg.idleTimeout,
	}
}

func shutdownHTTPServer(httpServer *http.Server, timeout time.Duration) (bool, error) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			if closeErr := httpServer.Close(); closeErr != nil {
				return true, fmt.Errorf("force close failed after shutdown timeout: %w", closeErr)
			}
			return true, nil
		}
		return false, fmt.Errorf("shutdown failed: %w", err)
	}
	return false, nil
}

func readDurationSecondsEnv(key string, fallback time.Duration, allowZero bool) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("invalid %s=%q, fallback to %s", key, raw, fallback)
		return fallback
	}
	if seconds < 0 {
		log.Printf("invalid %s=%q, fallback to %s", key, raw, fallback)
		return fallback
	}
	if seconds == 0 && !allowZero {
		log.Printf("invalid %s=%q, fallback to %s", key, raw, fallback)
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
