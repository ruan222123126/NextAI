package channel

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestConsoleChannelSendTextLogsWithoutMessageBody(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()
	originalPrefix := log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
		log.SetPrefix(originalPrefix)
	})

	ch := NewConsoleChannel()
	secret := "my-secret-token-123"
	if err := ch.SendText(context.Background(), "u1", "s1", secret, nil); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}

	logText := buf.String()
	if strings.Contains(logText, secret) {
		t.Fatalf("expected log to hide message body, got=%q", logText)
	}
	if !strings.Contains(logText, "chars=") {
		t.Fatalf("expected redacted metric in log, got=%q", logText)
	}
}
