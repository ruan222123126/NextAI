package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLoadHTTPRuntimeConfigDefaults(t *testing.T) {
	unsetEnvForTest(t, envHTTPReadHeaderTimeoutSeconds)
	unsetEnvForTest(t, envHTTPReadTimeoutSeconds)
	unsetEnvForTest(t, envHTTPWriteTimeoutSeconds)
	unsetEnvForTest(t, envHTTPIdleTimeoutSeconds)
	unsetEnvForTest(t, envHTTPShutdownTimeoutSeconds)

	cfg := loadHTTPRuntimeConfig()
	if cfg.readHeaderTimeout != defaultHTTPReadHeaderTimeout {
		t.Fatalf("readHeaderTimeout=%s want=%s", cfg.readHeaderTimeout, defaultHTTPReadHeaderTimeout)
	}
	if cfg.readTimeout != defaultHTTPReadTimeout {
		t.Fatalf("readTimeout=%s want=%s", cfg.readTimeout, defaultHTTPReadTimeout)
	}
	if cfg.writeTimeout != defaultHTTPWriteTimeout {
		t.Fatalf("writeTimeout=%s want=%s", cfg.writeTimeout, defaultHTTPWriteTimeout)
	}
	if cfg.idleTimeout != defaultHTTPIdleTimeout {
		t.Fatalf("idleTimeout=%s want=%s", cfg.idleTimeout, defaultHTTPIdleTimeout)
	}
	if cfg.shutdownTimeout != defaultHTTPShutdownTimeout {
		t.Fatalf("shutdownTimeout=%s want=%s", cfg.shutdownTimeout, defaultHTTPShutdownTimeout)
	}
}

func TestLoadHTTPRuntimeConfigFromEnv(t *testing.T) {
	t.Setenv(envHTTPReadHeaderTimeoutSeconds, "5")
	t.Setenv(envHTTPReadTimeoutSeconds, "60")
	t.Setenv(envHTTPWriteTimeoutSeconds, "300")
	t.Setenv(envHTTPIdleTimeoutSeconds, "90")
	t.Setenv(envHTTPShutdownTimeoutSeconds, "15")

	cfg := loadHTTPRuntimeConfig()
	if cfg.readHeaderTimeout != 5*time.Second {
		t.Fatalf("readHeaderTimeout=%s want=%s", cfg.readHeaderTimeout, 5*time.Second)
	}
	if cfg.readTimeout != 60*time.Second {
		t.Fatalf("readTimeout=%s want=%s", cfg.readTimeout, 60*time.Second)
	}
	if cfg.writeTimeout != 300*time.Second {
		t.Fatalf("writeTimeout=%s want=%s", cfg.writeTimeout, 300*time.Second)
	}
	if cfg.idleTimeout != 90*time.Second {
		t.Fatalf("idleTimeout=%s want=%s", cfg.idleTimeout, 90*time.Second)
	}
	if cfg.shutdownTimeout != 15*time.Second {
		t.Fatalf("shutdownTimeout=%s want=%s", cfg.shutdownTimeout, 15*time.Second)
	}
}

func TestShutdownHTTPServerDrainsInflightRequest(t *testing.T) {
	started := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		time.Sleep(120 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	})

	httpServer, baseURL, serveDone := startTestHTTPServer(t, handler)

	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.Get(baseURL)
		if err != nil {
			clientDone <- err
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			clientDone <- err
			return
		}
		if resp.StatusCode != http.StatusOK {
			clientDone <- fmt.Errorf("status=%d", resp.StatusCode)
			return
		}
		if strings.TrimSpace(string(body)) != "ok" {
			clientDone <- fmt.Errorf("body=%q", string(body))
			return
		}
		clientDone <- nil
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start in time")
	}

	timedOut, err := shutdownHTTPServer(httpServer, 2*time.Second)
	if err != nil {
		t.Fatalf("shutdownHTTPServer returned error: %v", err)
	}
	if timedOut {
		t.Fatalf("expected graceful shutdown without timeout")
	}

	if clientErr := <-clientDone; clientErr != nil {
		t.Fatalf("in-flight request failed: %v", clientErr)
	}

	serveErr := <-serveDone
	if !errors.Is(serveErr, http.ErrServerClosed) {
		t.Fatalf("Serve returned err=%v want=%v", serveErr, http.ErrServerClosed)
	}
}

func TestShutdownHTTPServerTimeoutFallsBackToForceClose(t *testing.T) {
	started := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("slow"))
	})

	httpServer, baseURL, serveDone := startTestHTTPServer(t, handler)
	go func() {
		_, _ = http.Get(baseURL)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start in time")
	}

	timedOut, err := shutdownHTTPServer(httpServer, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("shutdownHTTPServer returned error: %v", err)
	}
	if !timedOut {
		t.Fatalf("expected timeout fallback to force close")
	}

	serveErr := <-serveDone
	if !errors.Is(serveErr, http.ErrServerClosed) {
		t.Fatalf("Serve returned err=%v want=%v", serveErr, http.ErrServerClosed)
	}
}

func startTestHTTPServer(t *testing.T, handler http.Handler) (*http.Server, string, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	httpServer := &http.Server{Handler: handler}
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- httpServer.Serve(listener)
	}()

	return httpServer, "http://" + listener.Addr().String(), serveDone
}
