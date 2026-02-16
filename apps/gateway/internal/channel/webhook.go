package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWebhookMethod  = http.MethodPost
	defaultWebhookTimeout = 5 * time.Second
)

type WebhookChannel struct{}

func NewWebhookChannel() *WebhookChannel {
	return &WebhookChannel{}
}

func (c *WebhookChannel) Name() string {
	return "webhook"
}

func (c *WebhookChannel) SendText(ctx context.Context, userID, sessionID, text string, cfg map[string]interface{}) error {
	url := strings.TrimSpace(toString(cfg["url"]))
	if url == "" {
		return fmt.Errorf("channel webhook requires config.url")
	}

	method := strings.ToUpper(strings.TrimSpace(toString(cfg["method"])))
	if method == "" {
		method = defaultWebhookMethod
	}

	payload := map[string]interface{}{
		"user_id":    userID,
		"session_id": sessionID,
		"text":       text,
		"sent_at":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload failed: %w", err)
	}

	timeout := toDurationSeconds(cfg["timeout_seconds"], defaultWebhookTimeout)
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range toStringMap(cfg["headers"]) {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func toString(input interface{}) string {
	switch v := input.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func toDurationSeconds(raw interface{}, fallback time.Duration) time.Duration {
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			return time.Duration(v * float64(time.Second))
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && parsed > 0 {
			return time.Duration(parsed) * time.Second
		}
	}
	return fallback
}

func toStringMap(raw interface{}) map[string]string {
	out := map[string]string{}
	switch v := raw.(type) {
	case map[string]interface{}:
		for key, value := range v {
			text := strings.TrimSpace(toString(value))
			if text != "" {
				out[key] = text
			}
		}
	case map[string]string:
		for key, value := range v {
			value = strings.TrimSpace(value)
			if value != "" {
				out[key] = value
			}
		}
	}
	return out
}
