// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"

	vendor "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	ErrAuthentication    = errors.New("grpc: authentication rejected")
	ErrProxyResponse     = errors.New("grpc: endpoint returned a non-RPC response")
	ErrResourceExhausted = errors.New("grpc: RPC resource exhausted")
	ErrTooLarge          = errors.New("grpc: message exceeds configured limit")
	ErrEndpoint          = errors.New("grpc: endpoint must be an HTTPS authority")
	ErrCredentials       = errors.New("grpc: invalid native credentials")
	ErrMethod            = errors.New("grpc: invalid RPC method")
)

type Config struct {
	Endpoint        string
	MaxMessageBytes int
	TLSConfig       *tls.Config
}

// Credentials are supplied by a native credential owner. They are carried only
// in authenticated RPC metadata, never endpoint URLs or serialized messages.
type Credentials struct {
	Token string
}

// Client owns a reusable, verified TLS HTTP/2 channel. RPC retry, session
// binding, account state and migration policy remain in the calling supervisor.
type Client struct {
	channel  *vendor.ClientConn
	maxBytes int
}

// NewClient is lazy: success validates configuration, not connectivity or
// authentication. Callers bound attempts with contexts supplied to RPC methods.
func NewClient(config Config) (*Client, error) {
	endpoint, err := parseEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	if config.MaxMessageBytes < 1 || config.MaxMessageBytes > maxCodecBytes {
		return nil, ErrTooLarge
	}
	tlsConfig, err := clientTLS(config.TLSConfig, endpoint.Hostname())
	if err != nil {
		return nil, err
	}
	channel, err := vendor.NewClient("dns:///"+endpoint.Host,
		vendor.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		vendor.WithDisableRetry(), vendor.WithDisableServiceConfig(), vendor.WithMaxHeaderListSize(16<<10),
		vendor.WithDefaultCallOptions(vendor.ForceCodecV2(vtCodec{}), vendor.MaxCallRecvMsgSize(config.MaxMessageBytes), vendor.MaxCallSendMsgSize(config.MaxMessageBytes)))
	if err != nil {
		return nil, normalize(err)
	}
	return &Client{channel: channel, maxBytes: config.MaxMessageBytes}, nil
}

func parseEndpoint(value string) (*url.URL, error) {
	endpoint, err := url.Parse(value)
	if err != nil {
		return nil, ErrEndpoint
	}
	if !validEndpointShape(endpoint) {
		return nil, ErrEndpoint
	}
	if strings.HasSuffix(endpoint.Host, ":") {
		return nil, ErrEndpoint
	}
	if port := endpoint.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return nil, ErrEndpoint
		}
	}
	return endpoint, nil
}

func validEndpointShape(endpoint *url.URL) bool {
	return endpoint.Scheme == "https" && endpoint.Hostname() != "" && endpoint.User == nil && endpoint.Path == "" && endpoint.RawQuery == "" && endpoint.Fragment == "" && !endpoint.ForceQuery
}

func clientTLS(config *tls.Config, host string) (*tls.Config, error) {
	result := &tls.Config{MinVersion: tls.VersionTLS12}
	if config != nil {
		result = config.Clone()
	}
	if result.InsecureSkipVerify {
		return nil, ErrEndpoint
	}
	if result.ServerName != "" && !strings.EqualFold(result.ServerName, host) {
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

func rpcContext(ctx context.Context, method string, auth Credentials) (context.Context, error) {
	if !validMethod(method) {
		return nil, ErrMethod
	}
	if auth.Token == "" || !validHeader(auth.Token, 8192) {
		return nil, ErrCredentials
	}
	// Replace outgoing metadata instead of inheriting duplicate Authorization or
	// arbitrary application headers from another account's context.
	headers := metadata.Pairs("authorization", "Bearer "+auth.Token)
	return metadata.NewOutgoingContext(ctx, headers), nil
}
func validHeader(value string, limit int) bool {
	if len(value) > limit {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 33 || value[i] > 126 {
			return false
		}
	}
	return true
}
func validMethod(method string) bool {
	if len(method) < 4 || len(method) > 1024 || method[0] != '/' || strings.Count(method, "/") != 2 {
		return false
	}
	separator := strings.LastIndexByte(method, '/')
	if separator <= 1 || separator >= len(method)-1 {
		return false
	}
	for i := 0; i < len(method); i++ {
		if !methodByte(method[i]) {
			return false
		}
	}
	return true
}

func methodByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '/'
}

func (c *Client) Invoke(ctx context.Context, method string, auth Credentials, request, response Message) error {
	ctx, err := rpcContext(ctx, method, auth)
	if err != nil {
		return err
	}
	if err := c.checkRequest(request); err != nil {
		return err
	}
	if _, err := messageOf(response); err != nil {
		return err
	}
	var header, trailer metadata.MD
	err = c.channel.Invoke(ctx, method, request, response, vendor.Header(&header), vendor.Trailer(&trailer))
	return normalizeRPC(err, header, trailer)
}

// Listen sends exactly one typed request and immediately closes the request
// direction. Stream exposes receive and cancellation only.
func (c *Client) Listen(ctx context.Context, method string, auth Credentials, request Message) (*Stream, error) {
	ownerDone := ctx.Done()
	ctx, err := rpcContext(ctx, method, auth)
	if err != nil {
		return nil, err
	}
	if err := c.checkRequest(request); err != nil {
		return nil, err
	}
	child, cancel := context.WithCancel(ctx)
	stream, err := c.channel.NewStream(child, &vendor.StreamDesc{ServerStreams: true}, method)
	if err == nil {
		err = stream.SendMsg(request)
	}
	if err == nil {
		err = stream.CloseSend()
	}
	if err != nil {
		result := normalizeStreamError(err, stream)
		cancel()
		return nil, result
	}
	return &Stream{stream: stream, cancel: cancel, ownerDone: ownerDone}, nil
}

func (c *Client) checkRequest(value Message) error {
	size, err := messageSize(value)
	if errors.Is(err, ErrMessageTooLarge) {
		return ErrTooLarge
	}
	if err != nil {
		return err
	}
	if size > c.maxBytes {
		return ErrTooLarge
	}
	return nil
}

func (c *Client) Close() error { return c.channel.Close() }

// Stream owns one server-streaming RPC, independently of the client's channel.
// Recv has one owner. Its decoded message contains safe, owned strings/bytes;
// reuse/reset messages only once downstream consumers finish with their fields.
type Stream struct {
	stream    vendor.ClientStream
	cancel    context.CancelFunc
	ownerDone <-chan struct{}
	once      sync.Once
}

func (s *Stream) Recv(ctx context.Context, message Message) error {
	if _, err := messageOf(message); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return err
	}
	if ctx.Done() != s.ownerDone {
		stop := context.AfterFunc(ctx, func() { _ = s.Close() })
		defer stop()
	}
	err := s.stream.RecvMsg(message)
	if err == nil {
		return nil
	}
	if callerErr := ctx.Err(); callerErr != nil {
		_ = s.Close()
		return callerErr
	}
	result := normalizeStreamError(err, s.stream)
	_ = s.Close()
	return result
}
func (s *Stream) Close() error { s.once.Do(s.cancel); return nil }

// Failed response classification inspects metadata only after failure. HTTP
// gateways can map a challenge's HTTP 403 to PermissionDenied without speaking
// gRPC. Only a verified RPC content-type may invalidate account credentials.
func normalizeStreamError(err error, stream vendor.ClientStream) error {
	if stream == nil || !needsRPCProof(status.Code(err)) {
		return normalize(err)
	}
	header, _ := stream.Header()
	return normalizeRPC(err, header, stream.Trailer())
}

func needsRPCProof(code codes.Code) bool {
	switch code {
	case codes.Unauthenticated, codes.PermissionDenied, codes.Unknown, codes.Internal, codes.Unimplemented:
		return true
	default:
		return false
	}
}

func hasRPCContentType(headers metadata.MD) bool {
	const base = "application/grpc"
	for _, value := range headers.Get("content-type") {
		if value == base {
			return true
		}
		if strings.HasPrefix(value, base+"+") || strings.HasPrefix(value, base+";") {
			return true
		}
	}
	return false
}

func normalize(err error) error { return normalizeRPC(err, nil, nil) }

func normalizeRPC(err error, header, trailer metadata.MD) error {
	if err == nil {
		return nil
	}
	rpc := hasRPCContentType(header) || hasRPCContentType(trailer)
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return normalizeAuthentication(rpc)
	case codes.Unknown, codes.Internal, codes.Unimplemented:
		if !rpc {
			return ErrProxyResponse
		}
		return err
	case codes.ResourceExhausted:
		return ErrResourceExhausted
	case codes.Canceled:
		return context.Canceled
	case codes.DeadlineExceeded:
		return context.DeadlineExceeded
	default:
		return err
	}
}

func normalizeAuthentication(rpc bool) error {
	if rpc {
		return ErrAuthentication
	}
	return ErrProxyResponse
}
