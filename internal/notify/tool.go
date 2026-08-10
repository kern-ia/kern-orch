package notify

import (
	"context"
	"fmt"

	"github.com/yoann/kern-orch/internal/graph"
)

// messageKey is the state key a graph sets before routing into a notify node — the
// text an agent decided a human should read.
const messageKey = "message"

// Tool returns a graph.ToolFunc that sends the state's "message" key as a Telegram
// message. client nil (no credentials configured) fails the node rather than silently
// dropping the message: a graph author who wired a notify node meant for it to fire.
func Tool(client *Client) graph.ToolFunc {
	return func(ctx context.Context, s *graph.State) error {
		if client == nil {
			return fmt.Errorf("notify: telegram not configured (set %s and %s)", "KERN_TELEGRAM_BOT_TOKEN", "KERN_TELEGRAM_CHAT_ID")
		}
		v, ok := s.Get(messageKey)
		if !ok {
			return fmt.Errorf("notify: state has no %q key to send", messageKey)
		}
		text, ok := v.(string)
		if !ok || text == "" {
			return fmt.Errorf("notify: state key %q is not a non-empty string", messageKey)
		}
		return client.Send(ctx, text)
	}
}
