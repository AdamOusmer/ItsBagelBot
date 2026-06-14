// Package twitch wraps the single OAuth flow the admin tool runs: the
// bot-account consent. The operator authenticates the bot's own Twitch account
// from a tailnet browser, and the resulting user token is persisted (over the
// users-service token RPC) so the fleet can act as the bot.
//
// This mirrors the dashboard's twitch package. The admin tool makes exactly
// one direct Helix call, FetchUser, to resolve the token's owner during the
// handshake; everything else in the fleet rides the outgress lane.
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
	bot      *oauth2.Config
}

type UserInfo struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
}

// New builds a bot-consent client. RedirectURL is baseURL + the admin's bot
// callback path, so it must equal the redirect registered in the Twitch app
// console (the admin's tailnet origin).
func New(clientID, clientSecret, baseURL, botScopes string) *Client {
	return &Client{
		clientID: clientID,
		bot: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     endpoints.Twitch,
			RedirectURL:  baseURL + "/auth/bot/callback",
			Scopes:       splitScopes(botScopes),
		},
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

// BotConsentURL is the top-level URL the operator's browser is redirected to
// at id.twitch.tv to grant the bot scopes.
func (c *Client) BotConsentURL(state string) string {
	return c.bot.AuthCodeURL(state)
}

// ExchangeBot trades the authorization code for the bot account's user token.
func (c *Client) ExchangeBot(ctx context.Context, code string) (*oauth2.Token, error) {
	return c.bot.Exchange(ctx, code)
}

// FetchUser resolves the token's owner via Helix. This is the one direct
// Twitch call here: it authenticates the just-issued user token, so it belongs
// to the OAuth handshake rather than the egress path.
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
