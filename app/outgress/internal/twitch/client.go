// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package twitch is the egress-side Helix client: token lifecycle, the
// retry-once-on-401 dance, and the one user-token lookup the workers need.
package twitch

import (
	"ItsBagelBot/pkg/codec"
	"bytes"
	"context"
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

// ClientID exposes the app's client id for condition fields that key on it
// (the client-scoped user.authorization.* subscriptions).
func (c *Client) ClientID() string { return c.clientID }

// Warmup mints the app token and, when the bot runs with user credentials,
// the bot's user token, establishing reusable Twitch connections before queue
// consumers become ready. Both probes are deliberately tiny reads; their
// responses are drained so HTTP/1.1 transports can reuse the socket.
//
// The bot token warm exists because chat sends routed to IdentityBot
// (sourceForIdentity) pull from c.user, and nothing else ever touched it:
// the first as="bot" chat message after every pod start paid a cold mint
// against id.twitch.tv (measured 80.5ms TCP RTT from a cluster pod, ~320ms
// for the full cold TLS handshake plus request, versus 11.5ms/kept-warm for
// api.twitch.tv). That is why first chat sends measured 360-390ms end to end
// against 15-25ms for every send after: the gap was almost entirely this
// unwarmed mint.
//
// The two mints are reported independently (joined rather than
// short-circuited) so a dead bot grant's failure text survives next to the
// app token's instead of one masking the other in warmupTwitch's log line.
func (c *Client) Warmup(ctx context.Context) error {
	appErr := c.warmupSource(ctx, c.app, "/helix/streams?first=1")
	if appErr != nil {
		appErr = fmt.Errorf("app token warmup: %w", appErr)
	}
	return errors.Join(appErr, c.warmupBotToken(ctx))
}

// warmupBotToken mints the bot's user token (c.user). c.user is nil when the
// bot runs without user credentials (see botTokenSource in main.go); that is
// a legitimate configuration, not a warmup failure. GET /helix/users with no
// id/login query returns the token's own account, so this needs no bot user
// id up front and works regardless of whether TWITCH_BOT_USER_ID is set.
func (c *Client) warmupBotToken(ctx context.Context) error {
	if c.user == nil {
		return nil
	}
	if err := c.warmupSource(ctx, c.user, "/helix/users"); err != nil {
		return fmt.Errorf("bot token warmup: %w", err)
	}
	return nil
}

// warmupSource mints src's token against endpoint and drains the response so
// the connection stays poolable. Shared by the app-token and bot-token warms.
func (c *Client) warmupSource(ctx context.Context, src *Source, endpoint string) error {
	res, err := c.request(ctx, src, getCall(endpoint))
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

// clientTimeout bounds every Helix call end to end, including a slow body;
// drainResponse relies on it as the backstop for a non-terminating response.
const clientTimeout = 10 * time.Second

// helixIdleConnTimeout / helixH2ReadIdleTimeout / helixH2PingTimeout keep the
// pooled connection to api.twitch.tv alive across quiet chat.
//
// The transport previously inherited http.DefaultTransport's 90s
// IdleConnTimeout, which is shorter than an ordinary lull in a Twitch chat. A
// channel quiet for 90 seconds lost its pooled connection, and the next
// message paid a fresh TLS handshake to api.twitch.tv -- measured at 126ms
// from a pod on 2026-08-20 (versus ~15-25ms on a warm connection). That is a
// far more frequent mid-stream cost than token expiry, because it recurs
// every time chat goes quiet rather than once per token lifetime.
//
// Raising IdleConnTimeout alone is not enough: the far end reaps idle
// connections on its own schedule, so a connection Go still believes is
// pooled can already be dead, and the next send discovers that instead of a
// handshake. The h2 PINGs are what actually keep it warm -- they are real
// traffic on the wire, so the flow never looks idle to the remote reaper, and
// when the path does die the missing PONG closes the connection within
// ReadIdle+Ping instead of never. Pings do not defeat local idle reaping;
// only stream activity moves lastIdle.
//
// Values and the reasoning behind them are taken wholesale from
// app/gossip/internal/core/http.go, which solved this exact problem for the
// stats fetchers -- read its comments for the full derivation, including why
// this uses net/http's own HTTP2Config rather than
// golang.org/x/net/http2.ConfigureTransports (ForceAttemptHTTP2 makes the
// bundled h2 transport own TLSNextProto and keeps a back-pointer, so these
// fields are merged in per connection; reaching for x/net would install a
// second, competing h2 implementation and promote an indirect dependency to a
// direct one for no gain).
const (
	helixIdleConnTimeout   = 10 * time.Minute
	helixH2ReadIdleTimeout = 15 * time.Second
	helixH2PingTimeout     = 3 * time.Second
)

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = maxIdleConnections
	transport.MaxIdleConnsPerHost = maxIdleConnectionsPerHost
	transport.IdleConnTimeout = helixIdleConnTimeout
	transport.ForceAttemptHTTP2 = true
	transport.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: helixH2ReadIdleTimeout,
		PingTimeout:     helixH2PingTimeout,
	}
	return &http.Client{Transport: transport, Timeout: clientTimeout}
}

// CloseIdleConnections releases pooled Twitch connections during shutdown.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

// SetTransport swaps the underlying HTTP transport, keeping the client's
// overall timeout. It exists for tests and benchmarks that must run the full
// request path without the network; wiring never calls it.
func (c *Client) SetTransport(rt http.RoundTripper) {
	c.http = &http.Client{Transport: rt, Timeout: clientTimeout}
}

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

// HelixCall is one Helix HTTP request: the method, the endpoint (path plus
// query string), and an optional JSON body.
type HelixCall struct {
	Method   string
	Endpoint string
	Body     []byte
}

// getCall builds a bodyless GET for one endpoint.
func getCall(endpoint string) HelixCall {
	return HelixCall{Method: http.MethodGet, Endpoint: endpoint}
}

// Do executes one Helix request under the app token (chat sends, conduit
// EventSub management, general reads). The caller owns the response body.
func (c *Client) Do(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	return c.request(ctx, c.app, HelixCall{Method: method, Endpoint: endpoint, Body: body})
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
	return c.ExecuteAs(ctx, IdentityAuto, "", HelixCall{Method: method, Endpoint: endpoint, Body: body})
}

// ExecuteAs runs a generic enqueued Helix job under the requested identity (or
// endpoint-based routing for IdentityAuto), with the same retry-once-on-401
// dance as Do. A user/broadcaster identity with no token available returns
// ErrNoUserToken so the caller surfaces it instead of 401-looping.
func (c *Client) ExecuteAs(ctx context.Context, id Identity, broadcasterID string, call HelixCall) (*http.Response, error) {
	src := c.sourceForIdentity(id, broadcasterID, call.Endpoint)
	if src == nil {
		return nil, ErrNoUserToken
	}
	return c.request(ctx, src, call)
}

// helixStream is the slice of Helix Get Streams the workers and RPC handlers
// read: the broadcast type ("live" while streaming), when the session began,
// and the title/game/viewer snapshot StreamDetails projects for the Overview
// dashboard. user_login/user_name are not decoded: every caller already knows
// its own broadcaster identity, so there is nothing here that needs them.
type helixStream struct {
	Type        string    `json:"type"`
	StartedAt   time.Time `json:"started_at"`
	Title       string    `json:"title"`
	GameName    string    `json:"game_name"`
	ViewerCount int       `json:"viewer_count"`
}

// getStream fetches broadcasterID's current stream via Helix Get Streams under
// the app token. live folds Twitch's two offline signals — no stream object at
// all, or one whose broadcast type is not "live" — into the single flag
// callers actually branch on. The caller does not own a response body (it is
// consumed here).
func (c *Client) getStream(ctx context.Context, broadcasterID string) (helixStream, bool, error) {
	res, err := c.request(ctx, c.app, getCall("/helix/streams?user_id="+url.QueryEscape(broadcasterID)))
	if err != nil {
		return helixStream{}, false, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return helixStream{}, false, &StatusError{Status: res.StatusCode, Body: string(body)}
	}

	var payload struct {
		Data []helixStream `json:"data"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return helixStream{}, false, err
	}
	if len(payload.Data) == 0 {
		return helixStream{}, false, nil
	}
	stream := payload.Data[0]
	return stream, stream.Type == "live", nil
}

// IsStreamLive reports whether broadcasterID is currently live, via Helix Get
// Streams under the app token.
func (c *Client) IsStreamLive(ctx context.Context, broadcasterID string) (bool, error) {
	_, live, err := c.getStream(ctx, broadcasterID)
	return live, err
}

// StreamStartedAt reports whether broadcasterID is currently live and, when it
// is, when the current stream session began. The pair comes from one Get
// Streams call so !uptime's live check and its clock cannot disagree.
func (c *Client) StreamStartedAt(ctx context.Context, broadcasterID string) (time.Time, bool, error) {
	stream, live, err := c.getStream(ctx, broadcasterID)
	if err != nil || !live {
		return time.Time{}, false, err
	}
	return stream.StartedAt, true, nil
}

// StreamDetails is the Get Streams snapshot the Overview dashboard projects
// as per-stream metadata: title/game/viewer count as Twitch reports them at
// the moment of the call, plus when the session began.
type StreamDetails struct {
	Title       string
	GameName    string
	ViewerCount int
	StartedAt   time.Time
}

// StreamDetails reports broadcasterID's live state and, when live, the
// title/game/viewer snapshot from that same Get Streams call. It exists so a
// caller that needs both the live flag and the metadata (the stream_status
// job) pays for one Helix call instead of IsStreamLive plus a second lookup;
// callers that only need the flag should keep using IsStreamLive.
func (c *Client) StreamDetails(ctx context.Context, broadcasterID string) (StreamDetails, bool, error) {
	stream, live, err := c.getStream(ctx, broadcasterID)
	if err != nil || !live {
		return StreamDetails{}, live, err
	}
	return StreamDetails{
		Title:       stream.Title,
		GameName:    stream.GameName,
		ViewerCount: stream.ViewerCount,
		StartedAt:   stream.StartedAt,
	}, true, nil
}

// helixUser is the slice of Helix Get Users the workers read: the account's
// numeric id and its creation time.
type helixUser struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// getUser runs one Get Users query ("id=..." or "login=...") under the app
// token and returns the first match. found is false when Twitch returns no
// user; the caller decides whether that is an error for its purpose.
func (c *Client) getUser(ctx context.Context, query string) (helixUser, bool, error) {
	res, err := c.request(ctx, c.app, getCall("/helix/users?"+query))
	if err != nil {
		return helixUser{}, false, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return helixUser{}, false, &StatusError{Status: res.StatusCode, Body: string(body)}
	}

	var payload struct {
		Data []helixUser `json:"data"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return helixUser{}, false, err
	}
	if len(payload.Data) == 0 {
		return helixUser{}, false, nil
	}
	return payload.Data[0], true, nil
}

// UserIDByLogin resolves a Twitch login to its numeric user id via Helix Get
// Users under the app token. Returns ("", nil) when no such user exists.
func (c *Client) UserIDByLogin(ctx context.Context, login string) (string, error) {
	user, _, err := c.getUser(ctx, "login="+url.QueryEscape(login))
	return user.ID, err
}

// UserCreatedAt returns a Twitch user's id and account creation time, looked up
// by id when provided, otherwise by login. found is false when no such user
// exists. It rides the app token, like every other general Helix read.
func (c *Client) UserCreatedAt(ctx context.Context, targetID, targetLogin string) (id string, createdAt time.Time, found bool, err error) {
	q := url.Values{}
	if targetID != "" {
		q.Set("id", targetID)
	} else {
		q.Set("login", targetLogin)
	}
	user, found, err := c.getUser(ctx, q.Encode())
	return user.ID, user.CreatedAt, found, err
}

// FollowedAt returns when targetID followed broadcasterID. found is false when
// the Twitch user exists but does not currently follow the channel. The call
// uses the bot's moderator-scoped user token.
func (c *Client) FollowedAt(ctx context.Context, broadcasterID, targetID string) (time.Time, bool, error) {
	q := url.Values{}
	q.Set("broadcaster_id", broadcasterID)
	q.Set("user_id", targetID)
	res, err := c.ExecuteAs(ctx, IdentityBot, broadcasterID, getCall("/helix/channels/followers?"+q.Encode()))
	if err != nil {
		return time.Time{}, false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return time.Time{}, false, &StatusError{Status: res.StatusCode, Body: string(body)}
	}
	var payload struct {
		Data []struct {
			FollowedAt time.Time `json:"followed_at"`
		} `json:"data"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return time.Time{}, false, err
	}
	if len(payload.Data) == 0 {
		return time.Time{}, false, nil
	}
	return payload.Data[0].FollowedAt, true, nil
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

		res, err := c.request(ctx, c.user, getCall(endpoint))
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
func (c *Client) request(ctx context.Context, src *Source, call HelixCall) (*http.Response, error) {

	res, err := c.do(ctx, src, call)
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

	return c.do(ctx, src, call)
}

func isMissingScope(body []byte) bool {
	return bytes.Contains(bytes.ToLower(body), []byte("missing scope"))
}

func (c *Client) do(ctx context.Context, src *Source, call HelixCall) (*http.Response, error) {

	token, err := src.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("twitch token: %w", err)
	}

	var reader io.Reader
	if len(call.Body) > 0 {
		reader = bytes.NewReader(call.Body)
	}

	req, err := http.NewRequestWithContext(ctx, call.Method, apiBase+call.Endpoint, reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	if len(call.Body) > 0 {
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

	if err := codec.NewDecoder(res.Body).Decode(&page); err != nil {
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
