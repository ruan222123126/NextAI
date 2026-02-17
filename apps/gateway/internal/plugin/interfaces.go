package plugin

import "context"

type ChannelPlugin interface {
	Name() string
	SendText(ctx context.Context, userID, sessionID, text string, cfg map[string]interface{}) error
}

type ToolPlugin interface {
	Name() string
	Invoke(input map[string]interface{}) (map[string]interface{}, error)
}
