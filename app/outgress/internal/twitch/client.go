// Package twitch is the egress-side Helix client: token lifecycle, the
// retry-once-on-401 dance, and the one user-token lookup the workers need.
package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const apiBase = "https://api.twitch.tv"

// ErrNoUserToken marks calls that need the bot's user token when none is
// configured. Callers treat it as "cannot verify", not as a failure.
var ErrNoUserToken = errors.New("no bot user token configured")

// Client routes each Helix call to the token Twitch expects for it:
//
//   - app token (c.app): conduit EventSub management and chat sends. Conduit
//     transport is only valid under an app token, and chat sends ride the
//     app token because Twitch honors the bot's user:bot / user:write:chat
//     grant and the broadcaster's channel:bot grant when the bot is the
//     sender. Every general Helix read uses it too.
//   - bot user token (c.user): only /helix/moderation/channels, which needs
//     the bot account's user:read:moderated_channels scope. nil when the bot
//     runs without user credentials, which downgrades mod checks to non-mod.
type Client struct {
	http     *http.Client
	clientID string
	app      *Source
	user     *Source // nil when the bot runs without user credentials
}

func NewClient(clientID string, app *Source, user *Source) *Client {
	return &Client{
		http:     &http.Client{Timeout: 10 * time.Second},
		clientID: clientID,
		app:      app,
		user:     user,
	}
}

// Do executes one Helix request under the app token (chat sends, conduit
// EventSub management, general reads). The caller owns the response body.
func (c *Client) Do(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	return c.request(ctx, c.app, method, endpoint, body)
}

// IsModerator reports whether the bot account moderates broadcasterID,
// paging through the channels the bot's user token can see. Requires the
// user:read:moderated_channels scope.
func (c *Client) IsModerator(ctx context.Context, botID, broadcasterID string) (bool, error) {

	if c.user == nil {
		return false, ErrNoUserToken
	}

	after := ""
	for {
		endpoint := "/helix/moderation/channels?first=100&user_id=" + url.QueryEscape(botID)
		if after != "" {
			endpoint += "&after=" + url.QueryEscape(after)
		}

		res, err := c.request(ctx, c.user, http.MethodGet, endpoint, nil)
		if err != nil {
			return false, err
		}

		found, cursor, err := scanModeratedPage(res, broadcasterID)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
		if cursor == "" {
			return false, nil
		}
		after = cursor
	}
}

// AppTokenExpiresIn reports the remaining app token lifetime for the system
// status RPC.
func (c *Client) AppTokenExpiresIn() time.Duration {
	return c.app.ExpiresIn()
}

// HasUserToken reports whether mod verification is available.
func (c *Client) HasUserToken() bool {
	return c.user != nil
}

// request retries exactly once on 401 with a freshly minted token: a 401
// under a cached token usually means Twitch revoked it early, while a 401
// on the retry is a real credentials problem the caller has to surface.
func (c *Client) request(ctx context.Context, src *Source, method, endpoint string, body []byte) (*http.Response, error) {

	res, err := c.do(ctx, src, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusUnauthorized {
		return res, nil
	}

	drain(res)
	src.Invalidate()

	return c.do(ctx, src, method, endpoint, body)
}

func (c *Client) do(ctx context.Context, src *Source, method, endpoint string, body []byte) (*http.Response, error) {

	token, err := src.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("twitch token: %w", err)
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiBase+endpoint, reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.http.Do(req)
}

// RetryAfter converts the Ratelimit-Reset header of a 429 (unix seconds)
// into a wait duration, zero when absent or already in the past.
func RetryAfter(res *http.Response) time.Duration {

	reset, err := strconv.ParseInt(res.Header.Get("Ratelimit-Reset"), 10, 64)
	if err != nil {
		return 0
	}

	wait := time.Until(time.Unix(reset, 0))
	if wait < 0 {
		return 0
	}

	return wait
}

func scanModeratedPage(res *http.Response, broadcasterID string) (bool, string, error) {

	defer drain(res)

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return false, "", fmt.Errorf("moderated channels lookup failed: %d %s", res.StatusCode, string(body))
	}

	var page struct {
		Data []struct {
			BroadcasterID string `json:"broadcaster_id"`
		} `json:"data"`
		Pagination struct {
			Cursor string `json:"cursor"`
		} `json:"pagination"`
	}

	if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
		return false, "", err
	}

	for _, entry := range page.Data {
		if entry.BroadcasterID == broadcasterID {
			return true, "", nil
		}
	}

	return false, page.Pagination.Cursor, nil
}

func drain(res *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()
}
