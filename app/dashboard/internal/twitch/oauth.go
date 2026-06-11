// Package twitch wraps the two OAuth flows the dashboard runs against Twitch:
// a minimal identity login, and a separate consent carrying the bot scopes
// when a broadcaster enables the bot. Keeping them separate keeps the login
// consent screen clean and only asks for elevated scopes when needed.
package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/endpoints"
)

type Client struct {
	clientID string
	login    *oauth2.Config
	bot      *oauth2.Config
	app      *clientcredentials.Config

	mu       sync.Mutex
	appToken *oauth2.Token
}

type UserInfo struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
}

func New(clientID, clientSecret, baseURL, botScopes string) *Client {
	mk := func(callback string, scopes []string) *oauth2.Config {
		return &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     endpoints.Twitch,
			RedirectURL:  baseURL + callback,
			Scopes:       scopes,
		}
	}
	return &Client{
		clientID: clientID,
		login:    mk("/auth/callback", nil),
		bot:      mk("/auth/enable-bot/callback", splitScopes(botScopes)),
		app: &clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     endpoints.Twitch.TokenURL,
		},
	}
}

func (c *Client) appAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.appToken != nil && c.appToken.Expiry.After(time.Now().Add(time.Minute)) {
		return c.appToken.AccessToken, nil
	}
	tok, err := c.app.Token(ctx)
	if err != nil {
		return "", err
	}
	c.appToken = tok
	return tok.AccessToken, nil
}

// EnsureChatSubscription creates the channel.chat.message EventSub
// subscription on the Conduit for the broadcaster. Without it Twitch routes
// nothing into the ingress shards. An already-existing subscription (409) is
// success.
func (c *Client) EnsureChatSubscription(ctx context.Context, broadcasterID, conduitID string) error {
	tok, err := c.appAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("app token: %w", err)
	}

	body, _ := json.Marshal(map[string]any{
		"type":    "channel.chat.message",
		"version": "1",
		"condition": map[string]string{
			"broadcaster_user_id": broadcasterID,
			"user_id":             broadcasterID,
		},
		"transport": map[string]string{
			"method":     "conduit",
			"conduit_id": conduitID,
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.twitch.tv/helix/eventsub/subscriptions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusConflict {
		return nil
	}
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&msg)
	return fmt.Errorf("eventsub subscription returned %d: %s", resp.StatusCode, msg.Message)
}

func splitScopes(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func (c *Client) LoginURL(state string) string {
	return c.login.AuthCodeURL(state)
}

func (c *Client) BotConsentURL(state string) string {
	return c.bot.AuthCodeURL(state)
}

func (c *Client) ExchangeLogin(ctx context.Context, code string) (*oauth2.Token, error) {
	return c.login.Exchange(ctx, code)
}

func (c *Client) ExchangeBot(ctx context.Context, code string) (*oauth2.Token, error) {
	return c.bot.Exchange(ctx, code)
}

// FetchUser resolves the token's owner via Helix.
func (c *Client) FetchUser(ctx context.Context, tok *oauth2.Token) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/users", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Client-Id", c.clientID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("helix /users returned %d", resp.StatusCode)
	}

	var body struct {
		Data []UserInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Data) == 0 {
		return nil, fmt.Errorf("helix /users returned no user")
	}
	return &body.Data[0], nil
}
