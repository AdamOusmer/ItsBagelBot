// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/dpop"
)

const maxTokenReply = 16 << 10

// WorkerNetworkConfig is trusted worker bootstrap data. DialContext is required
// so production can use only inherited, broker-connected TCP pipes. Tests opt
// into their own local dialer explicitly. The OS profile permits public system
// trust roots; credential and user-file access remain restricted.
type WorkerNetworkConfig struct {
	Endpoint    string
	TLSConfig   *tls.Config
	DialContext func(context.Context, string, string) (net.Conn, error)
}

type WorkerNetwork struct {
	endpoint  string
	http      *http.Client
	transport *http.Transport
}

func NewWorkerNetwork(config WorkerNetworkConfig) (*WorkerNetwork, error) {
	if !authority(config.Endpoint) || config.DialContext == nil {
		return nil, ErrConfiguration
	}
	settings, err := workerTLS(config)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: config.DialContext, TLSClientConfig: settings, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 16 << 10, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 90 * time.Second, ForceAttemptHTTP2: true}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrProtocol }}
	return &WorkerNetwork{endpoint: config.Endpoint, http: client, transport: transport}, nil
}

func workerTLS(config WorkerNetworkConfig) (*tls.Config, error) {
	settings, err := verifiedTLS(config.TLSConfig, config.Endpoint)
	if err != nil {
		return nil, err
	}
	if settings.RootCAs == nil {
		settings.RootCAs, err = x509.SystemCertPool()
		if err != nil {
			return nil, ErrConfiguration
		}
	}
	return settings, nil
}

func (w *WorkerNetwork) Do(ctx context.Context, request NetworkRequest) (NetworkResponse, error) {
	if !w.validRequest(request) {
		return NetworkResponse{}, ErrConfiguration
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, request.Endpoint, strings.NewReader(request.Form))
	if err != nil {
		return NetworkResponse{}, ErrConfiguration
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("DPoP", request.Proof)
	if request.Revoke {
		r.Header.Set("Authorization", "DPoP "+request.AccessToken)
	}
	response, err := w.http.Do(r)
	if err != nil {
		return NetworkResponse{}, ErrUnavailable
	}
	defer response.Body.Close()
	if request.Revoke && response.StatusCode == http.StatusNoContent {
		return NetworkResponse{Status: response.StatusCode}, nil
	}
	return DecodeTokenResponse(response)
}

func (w *WorkerNetwork) validRequest(r NetworkRequest) bool {
	if !validSecret(r.Proof, dpop.MaxProofBytes) || len(r.Form) > 4096 {
		return false
	}
	if r.Revoke {
		return r.Endpoint == w.endpoint+RevokePath && r.Form == "" && validSecret(r.AccessToken, 8192)
	}
	return r.Endpoint == w.endpoint+TokenPath && r.AccessToken == ""
}

func (w *WorkerNetwork) Close() error { w.transport.CloseIdleConnections(); return nil }

// DecodeTokenResponse belongs to the network worker. It parses standard OAuth
// JSON, returns only bounded typed fields and discards bodies on every failure.
func DecodeTokenResponse(r *http.Response) (NetworkResponse, error) {
	if r.Header.Get("Cf-Mitigated") != "" || !jsonContentType(r.Header.Get("Content-Type")) {
		return NetworkResponse{}, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxTokenReply+1))
	if err != nil || len(body) > maxTokenReply {
		return NetworkResponse{}, ErrProtocol
	}
	var token TokenReply
	if codec.UnmarshalStrict(body, &token) != nil {
		return NetworkResponse{}, ErrProtocol
	}
	nonce := r.Header.Get("DPoP-Nonce")
	if !validNonceHeader(r.Header, nonce) {
		return NetworkResponse{}, ErrProtocol
	}
	return typedTokenResponse(r.StatusCode, nonce, token)
}

func typedTokenResponse(status int, nonce string, token TokenReply) (NetworkResponse, error) {
	if status == http.StatusOK {
		if !validTokenReply(token) {
			return NetworkResponse{}, ErrProtocol
		}
		return NetworkResponse{Status: status, Token: token}, nil
	}
	if !knownOAuthError(token.Error) {
		return NetworkResponse{}, ErrProtocol
	}
	if status != http.StatusBadRequest || token.Error != "use_dpop_nonce" {
		nonce = ""
	}
	return NetworkResponse{Status: status, Nonce: nonce, Token: TokenReply{Error: token.Error}}, nil
}

func knownOAuthError(value string) bool {
	switch value {
	case "invalid_request", "invalid_client", "unsupported_grant_type", "invalid_grant", "use_dpop_nonce", "server_error", "temporarily_unavailable":
		return true
	default:
		return false
	}
}

func jsonContentType(value string) bool {
	media, _, err := mime.ParseMediaType(value)
	return err == nil && media == "application/json"
}

func validNonceHeader(header http.Header, nonce string) bool {
	return len(header.Values("DPoP-Nonce")) <= 1 && (nonce == "" || validSecret(nonce, 512))
}
