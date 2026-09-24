// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	maxIdleConnections        = 256
	maxIdleConnectionsPerHost = 192
)

var ErrNoUserToken = errors.New("no bot user token configured")

type Client struct {
	http         *http.Client
	clientID     string
	app          *Source
	user         *Source
	broadcasters *BroadcasterTokens
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

func (c *Client) ClientID() string { return c.clientID }

func (c *Client) Warmup(ctx context.Context) error {
	appErr := c.warmupSource(ctx, c.app, "/helix/streams?first=1")
	if appErr != nil {
		appErr = fmt.Errorf("app token warmup: %w", appErr)
	}
	return errors.Join(appErr, c.warmupBotToken(ctx))
}

func (c *Client) warmupBotToken(ctx context.Context) error {
	if c.user == nil {
		return nil
	}
	if err := c.warmupSource(ctx, c.user, "/helix/users"); err != nil {
		return fmt.Errorf("bot token warmup: %w", err)
	}
	return nil
}

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

const clientTimeout = 10 * time.Second

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

func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

func (c *Client) SetTransport(rt http.RoundTripper) {
	c.http = &http.Client{Transport: rt, Timeout: clientTimeout}
}

type Identity int

const (
	IdentityAuto Identity = iota
	IdentityApp
	IdentityBot
	IdentityBroadcaster
)

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

func ResolveIdentity(id Identity, endpoint string) Identity {
	if id != IdentityAuto {
		return id
	}
	if isUserScoped(endpoint) {
		return IdentityBot
	}
	return IdentityApp
}

type HelixCall struct {
	Method   string
	Endpoint string
	Body     []byte
}

func getCall(endpoint string) HelixCall {
	return HelixCall{Method: http.MethodGet, Endpoint: endpoint}
}

func (c *Client) Do(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	return c.request(ctx, c.app, HelixCall{Method: method, Endpoint: endpoint, Body: body})
}

const helixPrefix = "/helix/"

var userScopedRoutes = map[string]struct{}{
	"moderation":         {},
	"chat/chatters":      {},
	"channels/followers": {},
}

func isUserScoped(endpoint string) bool {
	service, resource, ok := helixRoute(endpoint)
	if !ok {
		return false
	}
	if _, found := userScopedRoutes[service]; found {
		return true
	}
	_, found := userScopedRoutes[service+"/"+resource]
	return found
}

func helixRoute(endpoint string) (service, resource string, ok bool) {
	path := endpoint
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	rest, found := strings.CutPrefix(path, helixPrefix)
	if !found {
		return "", "", false
	}
	service, rest, found = strings.Cut(rest, "/")
	if !found {
		return "", "", false
	}
	resource, _, _ = strings.Cut(rest, "/")
	return service, resource, true
}

func (c *Client) sourceFor(endpoint string) *Source {
	if isUserScoped(endpoint) {
		return c.user
	}
	return c.app
}

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

func (c *Client) Execute(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	return c.ExecuteAs(ctx, IdentityAuto, "", HelixCall{Method: method, Endpoint: endpoint, Body: body})
}

func (c *Client) ExecuteAs(ctx context.Context, id Identity, broadcasterID string, call HelixCall) (*http.Response, error) {
	src := c.sourceForIdentity(id, broadcasterID, call.Endpoint)
	if src == nil {
		return nil, ErrNoUserToken
	}
	return c.request(ctx, src, call)
}

type helixStream struct {
	Type        string    `json:"type"`
	StartedAt   time.Time `json:"started_at"`
	Title       string    `json:"title"`
	GameName    string    `json:"game_name"`
	ViewerCount int       `json:"viewer_count"`
}

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

func (c *Client) IsStreamLive(ctx context.Context, broadcasterID string) (bool, error) {
	_, live, err := c.getStream(ctx, broadcasterID)
	return live, err
}

func (c *Client) StreamStartedAt(ctx context.Context, broadcasterID string) (time.Time, bool, error) {
	stream, live, err := c.getStream(ctx, broadcasterID)
	if err != nil || !live {
		return time.Time{}, false, err
	}
	return stream.StartedAt, true, nil
}

type StreamDetails struct {
	Title       string
	GameName    string
	ViewerCount int
	StartedAt   time.Time
}

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

type helixUser struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

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

func (c *Client) UserIDByLogin(ctx context.Context, login string) (string, error) {
	user, _, err := c.getUser(ctx, "login="+url.QueryEscape(login))
	return user.ID, err
}

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

func (c *Client) AppTokenExpiresIn() time.Duration {
	return c.app.ExpiresIn()
}

func (c *Client) HasUserToken() bool {
	return c.user != nil
}

func (c *Client) request(ctx context.Context, src *Source, call HelixCall) (*http.Response, error) {

	res, err := c.do(ctx, src, call)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusUnauthorized {
		return res, nil
	}

	unauthorizedBody, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()
	if isMissingScope(unauthorizedBody) {
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

	seg := newrelic.StartExternalSegment(newrelic.FromContext(ctx), req)
	res, err := c.http.Do(req)
	seg.Response = res
	seg.End()
	return res, err
}

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
