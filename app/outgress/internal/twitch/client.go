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
	"strings"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"
)

const apiBase = "https://api.twitch.tv"

const (
	// One outgress pod can run many request goroutines. HTTP/1.1's default of
	// two idle connections per host causes needless connection churn after a
	// burst; HTTP/2 simply multiplexes over fewer connections.
	maxIdleConnections        = 256
	maxIdleConnectionsPerHost = 192
)

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
//   - bot user token (c.user): endpoints that read or act in the bot's
//     moderator/user context, which the app token cannot satisfy:
//     /helix/moderation/* (e.g. moderated channels, bans), /helix/chat/chatters
//     (moderator:read:chatters) and /helix/channels/followers
//     (moderator:read:followers). nil when the bot runs without user
//     credentials, which downgrades these to "cannot verify".
//
// Execute routes a generic enqueued job to whichever of these its endpoint
// needs; Do is the explicit app-token path the EventSub calls use directly.
type Client struct {
	http         *http.Client
	clientID     string
	app          *Source
	user         *Source            // nil when the bot runs without user credentials
	broadcasters *BroadcasterTokens // per-channel user tokens, nil if unconfigured
}

func NewClient(clientID string, app, user *Source, broadcasters *BroadcasterTokens) *Client {
	return &Client{
		http:         newHTTPClient(),
		clientID:     clientID,
		app:          app,
		user:         user,
		broadcasters: broadcasters,
	}
}

// Warmup mints the app token and establishes a reusable Twitch connection
// before queue consumers become ready. The read-only request is deliberately
// tiny; its response is drained so HTTP/1.1 transports can reuse the socket.
func (c *Client) Warmup(ctx context.Context) error {
	res, err := c.request(ctx, c.app, http.MethodGet, "/helix/streams?first=1", nil)
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return &StatusError{Status: res.StatusCode, Body: string(body)}
	}
	return nil
}

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = maxIdleConnections
	transport.MaxIdleConnsPerHost = maxIdleConnectionsPerHost
	transport.ForceAttemptHTTP2 = true
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

// CloseIdleConnections releases pooled Twitch connections during shutdown.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

// Identity names whose token a job runs under. IdentityAuto keeps the
// endpoint-based routing (sourceFor); the rest are explicit producer choices
// carried on the wire as the message "as" field.
type Identity int

const (
	IdentityAuto        Identity = iota // route by endpoint (default)
	IdentityApp                         // app token
	IdentityBot                         // the bot account's user token
	IdentityBroadcaster                 // the target channel's own user token
)

// ParseIdentity maps the wire "as" field to an Identity. "user" is an alias for
// "broadcaster"; anything unknown (including "") falls back to auto routing.
func ParseIdentity(s string) Identity {
	switch s {
	case "app":
		return IdentityApp
	case "bot":
		return IdentityBot
	case "broadcaster", "user":
		return IdentityBroadcaster
	default:
		return IdentityAuto
	}
}

// ResolveIdentity returns the token bucket identity used for a request. An
// explicit wire identity wins; automatic routing mirrors sourceFor so rate
// limiting and token selection cannot disagree.
func ResolveIdentity(id Identity, endpoint string) Identity {
	if id != IdentityAuto {
		return id
	}
	path := endpoint
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	for _, prefix := range userScopedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return IdentityBot
		}
	}
	return IdentityApp
}

// Do executes one Helix request under the app token (chat sends, conduit
// EventSub management, general reads). The caller owns the response body.
func (c *Client) Do(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	return c.request(ctx, c.app, method, endpoint, body)
}

// userScopedPrefixes are Helix path prefixes that must run under the bot's USER
// token rather than the app token, because they read or act in a moderator/user
// context the app token cannot satisfy. Cloud-bot chat sends are intentionally
// absent: Twitch requires the app token for the Chat Bot badge.
var userScopedPrefixes = []string{
	"/helix/moderation/",        // moderated channels, bans, etc.
	"/helix/chat/chatters",      // moderator:read:chatters
	"/helix/channels/followers", // moderator:read:followers
}

// sourceFor picks the token an endpoint needs: the bot user token for the
// moderator/user-scoped reads above, the app token for everything else.
func (c *Client) sourceFor(endpoint string) *Source {
	path := endpoint
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	for _, p := range userScopedPrefixes {
		if strings.HasPrefix(path, p) {
			return c.user
		}
	}
	return c.app
}

// sourceForIdentity resolves the token a job runs under. An explicit identity
// wins; IdentityAuto falls back to endpoint-based routing.
func (c *Client) sourceForIdentity(id Identity, broadcasterID, endpoint string) *Source {
	switch ResolveIdentity(id, endpoint) {
	case IdentityApp:
		return c.app
	case IdentityBot:
		return c.user
	case IdentityBroadcaster:
		return c.broadcasters.Get(broadcasterID)
	default:
		return c.app
	}
}

// Execute runs a generic enqueued Helix job under the token its endpoint
// requires (endpoint-based routing). Equivalent to ExecuteAs with IdentityAuto.
func (c *Client) Execute(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	return c.ExecuteAs(ctx, IdentityAuto, "", method, endpoint, body)
}

// ExecuteAs runs a generic enqueued Helix job under the requested identity (or
// endpoint-based routing for IdentityAuto), with the same retry-once-on-401
// dance as Do. A user/broadcaster identity with no token available returns
// ErrNoUserToken so the caller surfaces it instead of 401-looping.
func (c *Client) ExecuteAs(ctx context.Context, id Identity, broadcasterID, method, endpoint string, body []byte) (*http.Response, error) {
	src := c.sourceForIdentity(id, broadcasterID, endpoint)
	if src == nil {
		return nil, ErrNoUserToken
	}
	return c.request(ctx, src, method, endpoint, body)
}

// IsStreamLive reports whether broadcasterID is currently live, via Helix Get
// Streams under the app token. Get Streams only returns a stream object for live
// channels, so a non-empty data array means live. The caller does not own a
// response body (it is consumed here).
func (c *Client) IsStreamLive(ctx context.Context, broadcasterID string) (bool, error) {
	res, err := c.request(ctx, c.app, http.MethodGet, "/helix/streams?user_id="+url.QueryEscape(broadcasterID), nil)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return false, &StatusError{Status: res.StatusCode, Body: string(body)}
	}

	var payload struct {
		Data []struct {
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return false, err
	}
	for _, s := range payload.Data {
		if s.Type == "live" {
			return true, nil
		}
	}
	return false, nil
}

// UserIDByLogin resolves a Twitch login to its numeric user id via Helix Get
// Users under the app token. Returns ("", nil) when no such user exists.
func (c *Client) UserIDByLogin(ctx context.Context, login string) (string, error) {
	res, err := c.request(ctx, c.app, http.MethodGet, "/helix/users?login="+url.QueryEscape(login), nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return "", &StatusError{Status: res.StatusCode, Body: string(body)}
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return "", err
	}
	if len(payload.Data) == 0 {
		return "", nil
	}
	return payload.Data[0].ID, nil
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

	// Refreshing cannot add a missing OAuth scope. Preserve the response body
	// for the caller and return immediately instead of paying token-store RPC +
	// OAuth refresh + a guaranteed second 401.
	unauthorizedBody, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()
	if isMissingScope(unauthorizedBody) {
		// Do not retry inline, but discard the cached grant so a later background
		// check reloads a token the operator may have re-authorized in the store.
		src.Invalidate()
		res.Body = io.NopCloser(bytes.NewReader(unauthorizedBody))
		res.ContentLength = int64(len(unauthorizedBody))
		return res, nil
	}

	src.Invalidate()

	return c.do(ctx, src, method, endpoint, body)
}

func isMissingScope(body []byte) bool {
	return bytes.Contains(bytes.ToLower(body), []byte("missing scope"))
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

	// Record the Helix call as an external segment so its duration is isolated
	// from in-process work and (faceted by the node.region/node.name attributes
	// the worker sets on the transaction) tells node latency apart from code
	// latency. StartExternalSegment finds the transaction on the request context
	// and is a no-op when none is present, so non-instrumented callers pay
	// nothing.
	seg := newrelic.StartExternalSegment(newrelic.FromContext(ctx), req)
	res, err := c.http.Do(req)
	seg.Response = res
	seg.End()
	return res, err
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
