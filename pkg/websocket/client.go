// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package websocket owns bare, verified native binary WebSocket reception and
// same-origin HTTPS POST transport. Application protocol and policy live above it.
package websocket

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	vendor "github.com/coder/websocket"
)

var (
	ErrEndpoint       = errors.New("websocket: invalid secure endpoint or path")
	ErrCredentials    = errors.New("websocket: invalid native authorization")
	ErrAuthentication = errors.New("websocket: origin authentication rejected")
	ErrProxyResponse  = errors.New("websocket: proxy or HTML response rejected")
	ErrRejected       = errors.New("websocket: upgrade rejected")
	ErrDial           = errors.New("websocket: secure upgrade failed")
	ErrRedirect       = errors.New("websocket: redirects disabled")
	ErrTooLarge       = errors.New("websocket: message exceeds configured bound")
	ErrMessageType    = errors.New("websocket: expected binary message")
	ErrClosed         = errors.New("websocket: client closed")
	ErrNonce          = errors.New("websocket: fresh device proof required")
)

// Authorization is produced by the native credential owner. DPoP requires a
// fresh request proof; this transport never signs, refreshes or retries it.
type Authorization struct{ Scheme, Token, Proof string }
type Reply struct {
	Status int
	Nonce  string
}

type Config struct {
	Endpoint        string
	TLSConfig       *tls.Config
	MaxMessageBytes int
	Subprotocol     string
	// DialContext is supplied by the isolated worker's pipe broker. It does not
	// change URL/TLS verification or permit redirects to another origin.
	DialContext func(context.Context, string, string) (net.Conn, error)
}
type Client struct {
	endpoint    string
	maxBytes    int
	subprotocol string
	http        *http.Client
	transport   *http.Transport
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	conns       map[*Conn]struct{}
	closed      bool
}

func NewClient(config Config) (*Client, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || !validEndpoint(endpoint) {
		return nil, ErrEndpoint
	}
	if config.MaxMessageBytes < 1 || config.MaxMessageBytes > 4<<20 {
		return nil, ErrTooLarge
	}
	if config.Subprotocol != "" && !validProtocol(config.Subprotocol) {
		return nil, ErrEndpoint
	}
	tlsConfig, err := verifiedTLS(config.TLSConfig, endpoint.Hostname())
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if config.DialContext != nil {
		transport.DialContext = config.DialContext
	}
	transport.TLSClientConfig = tlsConfig
	// WS upgrades use HTTP/1.1; ordinary HTTPS POSTs still reuse this transport.
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	transport.Protocols.SetHTTP2(false)
	tlsConfig.NextProtos = []string{"http/1.1"}
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{endpoint: config.Endpoint, maxBytes: config.MaxMessageBytes, subprotocol: config.Subprotocol, transport: transport, ctx: ctx, cancel: cancel, conns: make(map[*Conn]struct{})}
	client.http = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect }}
	return client, nil
}
func validEndpoint(endpoint *url.URL) bool {
	return endpoint != nil && endpoint.Scheme == "https" && endpoint.Hostname() != "" && endpoint.User == nil && endpoint.Path == "" && endpoint.RawQuery == "" && !endpoint.ForceQuery && endpoint.Fragment == "" && !strings.HasSuffix(endpoint.Host, ":")
}
func verifiedTLS(config *tls.Config, host string) (*tls.Config, error) {
	result := &tls.Config{MinVersion: tls.VersionTLS12}
	if config != nil {
		result = config.Clone()
	}
	if result.InsecureSkipVerify || result.ServerName != "" && !strings.EqualFold(result.ServerName, host) {
		return nil, ErrEndpoint
	}
	if result.MinVersion < tls.VersionTLS12 {
		result.MinVersion = tls.VersionTLS12
	}
	if result.MaxVersion != 0 && result.MaxVersion < result.MinVersion {
		return nil, ErrEndpoint
	}
	return result, nil
}
func (c *Client) operation(ctx context.Context, path, token string) (context.Context, func(), error) {
	if !validPath(path) {
		return nil, nil, ErrEndpoint
	}
	if token == "" || !validHeader(token, 8192) {
		return nil, nil, ErrCredentials
	}
	if c.ctx.Err() != nil {
		return nil, nil, ErrClosed
	}
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	return child, func() { stop(); cancel() }, nil
}
func validPath(path string) bool {
	parsed, err := url.Parse(path)
	if err != nil {
		return false
	}
	return len(path) > 0 && path[0] == '/' && !strings.HasPrefix(path, "//") && parsed.Host == "" && parsed.Scheme == "" && parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == ""
}
func validHeader(value string, max int) bool {
	if len(value) > max {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 33 || value[i] > 126 {
			return false
		}
	}
	return true
}
func (c *Client) Dial(ctx context.Context, path, token string) (*Conn, error) {
	conn, _, err := c.DialAuthorized(ctx, path, Authorization{Scheme: "Bearer", Token: token})
	return conn, err
}

func (c *Client) DialAuthorized(ctx context.Context, path string, auth Authorization) (*Conn, Reply, error) {
	headers, err := authorizationHeaders(auth)
	if err != nil {
		return nil, Reply{}, err
	}
	child, cancel, err := c.operation(ctx, path, auth.Token)
	if err != nil {
		return nil, Reply{}, err
	}
	endpoint := "wss" + strings.TrimPrefix(c.endpoint, "https") + path
	raw, response, err := vendor.Dial(child, endpoint, &vendor.DialOptions{HTTPClient: c.http, HTTPHeader: headers, CompressionMode: vendor.CompressionDisabled, Subprotocols: offeredProtocol(c.subprotocol)})
	reply := responseReply(response)
	if err != nil {
		closed := c.ctx.Err() != nil
		cancel()
		failure := dialError(ctx, response, err)
		if closed {
			return nil, reply, ErrClosed
		}
		if ctx.Err() == nil && nonceChallenge(auth, reply, response) {
			return nil, reply, ErrNonce
		}
		return nil, reply, failure
	}
	if c.subprotocol != "" && raw.Subprotocol() != c.subprotocol {
		cancel()
		_ = raw.CloseNow()
		return nil, reply, ErrRejected
	}
	raw.SetReadLimit(int64(c.maxBytes))
	conn := &Conn{raw: raw, client: c, cancel: cancel, maxBytes: c.maxBytes}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = raw.CloseNow()
		cancel()
		return nil, reply, ErrClosed
	}
	c.conns[conn] = struct{}{}
	c.mu.Unlock()
	return conn, reply, nil
}

func authorizationHeaders(auth Authorization) (http.Header, error) {
	if !validHeader(auth.Token, 8192) {
		return nil, ErrCredentials
	}
	h := http.Header{"Authorization": []string{auth.Scheme + " " + auth.Token}}
	switch auth.Scheme {
	case "Bearer":
		if auth.Proof != "" {
			return nil, ErrCredentials
		}
	case "DPoP":
		if !validHeader(auth.Proof, 4096) || auth.Proof == "" {
			return nil, ErrCredentials
		}
		h.Set("DPoP", auth.Proof)
	default:
		return nil, ErrCredentials
	}
	return h, nil
}
func responseReply(response *http.Response) Reply {
	if response == nil {
		return Reply{}
	}
	result := Reply{Status: response.StatusCode}
	nonces := response.Header.Values("DPoP-Nonce")
	if len(nonces) == 1 && nonces[0] != "" && validHeader(nonces[0], 512) {
		result.Nonce = nonces[0]
	}
	return result
}
func nonceChallenge(auth Authorization, reply Reply, response *http.Response) bool {
	return auth.Scheme == "DPoP" && reply.Nonce != "" && (reply.Status == 400 || reply.Status == 401) && responseRejection(response) != ErrProxyResponse && requestsNonce(response.Header)
}
func requestsNonce(headers http.Header) bool {
	values := headers.Values("WWW-Authenticate")
	if len(values) != 1 || len(values[0]) > 1024 {
		return false
	}
	scheme, parameters, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "DPoP") {
		return false
	}
	// Authentication parameters use comma separators; MIME parses the same
	// token/quoted-string syntax using semicolons. Only error is consumed.
	_, params, err := mime.ParseMediaType("DPoP; " + strings.ReplaceAll(parameters, ",", ";"))
	return err == nil && params["error"] == "use_dpop_nonce"
}
func dialError(ctx context.Context, response *http.Response, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrRedirect) {
		return ErrRedirect
	}
	if response == nil {
		return ErrDial
	}
	if response.Body != nil {
		_ = response.Body.Close()
	}
	if rejection := responseRejection(response); rejection != nil {
		return rejection
	}
	return ErrRejected
}
func responseRejection(response *http.Response) error {
	if response.Header.Get("Cf-Mitigated") != "" || strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return ErrProxyResponse
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ErrAuthentication
	}
	return nil
}

// Post sends a bounded body to this client's configured HTTPS origin. The caller
// interprets the returned status (normally 204). Reply bodies are never exposed.
func (c *Client) Post(ctx context.Context, path, token, contentType string, body []byte) (int, error) {
	reply, err := c.PostAuthorized(ctx, path, Authorization{Scheme: "Bearer", Token: token}, contentType, body)
	return reply.Status, err
}

func (c *Client) PostAuthorized(ctx context.Context, path string, auth Authorization, contentType string, body []byte) (Reply, error) {
	headers, err := authorizationHeaders(auth)
	if err != nil {
		return Reply{}, err
	}
	if len(body) > c.maxBytes {
		return Reply{}, ErrTooLarge
	}
	if !validHeader(contentType, 128) {
		return Reply{}, ErrEndpoint
	}
	child, cancel, err := c.operation(ctx, path, auth.Token)
	if err != nil {
		return Reply{}, err
	}
	defer cancel()
	request, err := http.NewRequestWithContext(child, http.MethodPost, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return Reply{}, ErrEndpoint
	}
	request.Header = headers
	request.Header.Set("Content-Type", contentType)
	response, err := c.http.Do(request)
	if err != nil {
		if c.ctx.Err() != nil {
			return Reply{}, ErrClosed
		}
		return Reply{}, postError(ctx, err)
	}
	defer response.Body.Close()
	reply := responseReply(response)
	if nonceChallenge(auth, reply, response) {
		return reply, ErrNonce
	}
	if rejection := responseRejection(response); rejection != nil {
		return reply, rejection
	}
	// A control status is enough; draining at most 4 KiB bounds hostile replies.
	_, _ = io.CopyN(io.Discard, response.Body, 4<<10)
	return reply, nil
}
func postError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrRedirect) {
		return ErrRedirect
	}
	return ErrDial
}
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conns := c.conns
	c.conns = nil
	c.mu.Unlock()
	c.cancel()
	for conn := range conns {
		_ = conn.Close()
	}
	c.transport.CloseIdleConnections()
	return nil
}

type Conn struct {
	raw      *vendor.Conn
	client   *Client
	cancel   func()
	maxBytes int
	probe    [1]byte
	once     sync.Once
}

func (c *Conn) Read(ctx context.Context, buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, ErrTooLarge
	}
	kind, reader, err := c.raw.Reader(ctx)
	if err != nil {
		return 0, readError(err)
	}
	if kind != vendor.MessageBinary {
		_ = c.Close()
		return 0, ErrMessageType
	}
	if len(buffer) > c.maxBytes {
		buffer = buffer[:c.maxBytes]
	}
	n, err := c.readMessage(reader, buffer)
	if err != nil {
		_ = c.Close()
	}
	return n, readError(err)
}
func (c *Conn) readMessage(reader io.Reader, buffer []byte) (int, error) {
	total := 0
	for total < len(buffer) {
		n, err := reader.Read(buffer[total:])
		total += n
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
	n, err := reader.Read(c.probe[:])
	if n != 0 {
		return total, ErrTooLarge
	}
	if err == io.EOF {
		return total, nil
	}
	if err != nil {
		return total, err
	}
	return total, io.ErrNoProgress
}
func readError(err error) error {
	switch vendor.CloseStatus(err) {
	case vendor.StatusMessageTooBig:
		return ErrTooLarge
	case vendor.StatusNormalClosure, vendor.StatusGoingAway:
		return io.EOF
	default:
		return err
	}
}
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		c.cancel()
		err = c.raw.CloseNow()
		c.client.mu.Lock()
		delete(c.client.conns, c)
		c.client.mu.Unlock()
	})
	return err
}

func offeredProtocol(protocol string) []string {
	if protocol == "" {
		return nil
	}
	return []string{protocol}
}
func validProtocol(value string) bool {
	if len(value) > 128 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !protocolByte(value[i]) {
			return false
		}
	}
	return true
}
func protocolByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
}
