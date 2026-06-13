// Package twitch wraps the two OAuth flows the dashboard runs against Twitch:
// a minimal identity login, and a separate consent carrying the bot scopes
// when a broadcaster enables the bot. Keeping them separate keeps the login
// consent screen clean and only asks for elevated scopes when needed.
//
// The dashboard does not call Helix itself: EventSub management rides the
// outgress system lane, so every Helix request in the fleet pays the same
// rate-limit bucket.
package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

type Client struct {
	clientID string
	login    *oauth2.Config
	bot      *oauth2.Config
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
	}
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

// FetchUser resolves the token's owner via Helix. This is the one direct
// Twitch call left here: it authenticates the just-issued user token, so it
// belongs to the OAuth handshake rather than the egress path.
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
