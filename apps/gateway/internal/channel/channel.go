package channel

import (
	"context"
	"log"
)

type ConsoleChannel struct{}

func NewConsoleChannel() *ConsoleChannel {
	return &ConsoleChannel{}
}

func (c *ConsoleChannel) Name() string {
	return "console"
}

func (c *ConsoleChannel) SendText(_ context.Context, userID, sessionID, text string, _ map[string]interface{}) error {
	log.Printf("[console] user=%s session=%s text=%s", userID, sessionID, text)
	return nil
}
