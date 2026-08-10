// Package notify sends a message to a Telegram chat: the outbound half of an agent's
// ability to reach a human. It is not the same as C12's kern-notify relay — that pushes
// state-change events from the outside, whereas this is a tool a graph node calls when
// the agent itself, mid-run, decides a human needs to hear something.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// defaultBaseURL is Telegram's own API host. Overridable on Client so tests can point it
// at a fake server; never overridable outside a test.
const defaultBaseURL = "https://api.telegram.org"

// Client sends messages as one bot to one chat.
type Client struct {
	BaseURL string // defaults to Telegram's real API; set for tests only
	Token   string
	ChatID  string
	HTTP    *http.Client // defaults to http.DefaultClient
}

// New returns a client targeting the real Telegram API.
func New(token, chatID string) *Client {
	return &Client{Token: token, ChatID: chatID}
}

// Send posts text to the configured chat.
//
// Telegram answers 200 even for a refused request — a bad token, an unknown chat — with
// `{"ok": false, ...}` in the body, so the HTTP status alone never tells the whole story.
func (c *Client) Send(ctx context.Context, text string) error {
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL(), c.Token)
	body := strings.NewReader(url.Values{
		"chat_id": {c.ChatID},
		"text":    {text},
	}.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("notify: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("notify: send: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("notify: decode response (status %s): %w", resp.Status, err)
	}
	if resp.StatusCode != http.StatusOK || !result.OK {
		return fmt.Errorf("notify: telegram refused (status %s): %s", resp.Status, result.Description)
	}
	return nil
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
