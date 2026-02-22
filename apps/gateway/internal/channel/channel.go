package channel

import (
	"context"
	"log"
	"unicode/utf8"
)

type ConsoleChannel struct{}

func NewConsoleChannel() *ConsoleChannel {
	return &ConsoleChannel{}
}

func (c *ConsoleChannel) Name() string {
	return "console"
}

func (c *ConsoleChannel) SendText(_ context.Context, _ string, _ string, text string, _ map[string]interface{}) error {
	log.Printf("[console] outbound message delivered chars=%d", utf8.RuneCountInString(text))
	return nil
}
