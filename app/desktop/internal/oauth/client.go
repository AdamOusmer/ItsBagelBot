// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/pkg/dpop"
)

const requestTimeout = 10 * time.Second

type tokenReply = TokenReply

type tokenClient struct {
	endpoint string
	network  Network
}

func newTokenClient(c Config) (*tokenClient, error) {
	if c.Network == nil {
		return nil, ErrConfiguration
	}
	return &tokenClient{endpoint: c.TokenEndpoint + TokenPath, network: c.Network}, nil
}

func (c *tokenClient) exchange(ctx context.Context, key *dpop.Key, form url.Values) (tokenReply, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	form.Set("client_id", ClientID)
	nonce := ""
	for attempt := 0; attempt < 3; attempt++ {
		reply, next, err := c.request(ctx, key, form, nonce)
		if reply.Error == "use_dpop_nonce" && next != "" {
			nonce = next
			continue
		}
		if errors.Is(err, ErrUnavailable) && ctx.Err() == nil {
			continue
		}
		return reply, err
	}
	return tokenReply{}, ErrProtocol
}

func (c *tokenClient) request(ctx context.Context, key *dpop.Key, form url.Values, nonce string) (tokenReply, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	proof, err := key.Proof(http.MethodPost, c.endpoint, "", nonce)
	if err != nil {
		return tokenReply{}, "", ErrProtocol
	}
	response, err := c.network.Do(ctx, NetworkRequest{Endpoint: c.endpoint, Proof: proof, Form: form.Encode()})
	if err != nil {
		return tokenReply{}, "", err
	}
	return response.Token, response.Nonce, tokenResponseError(response.Status, response.Token)
}

func tokenResponseError(status int, reply tokenReply) error {
	if status == http.StatusOK {
		if !validTokenReply(reply) {
			return ErrProtocol
		}
		return nil
	}
	if status == http.StatusBadRequest && reply.Error == "invalid_grant" {
		return ErrAuthentication
	}
	if status == http.StatusBadRequest && reply.Error == "use_dpop_nonce" {
		return ErrProtocol
	}
	return ErrUnavailable
}

func validTokenReply(r tokenReply) bool {
	return r.Type == "DPoP" && validSecret(r.Access, 8192) && validSecret(r.Refresh, 1024) && r.Expires >= 60 && r.Expires <= 7*24*3600 && validSecret(r.Grant, 128) && validSecret(r.Device, 128) && r.Error == ""
}

func validSecret(value string, maximum int) bool {
	if len(value) == 0 || len(value) > maximum {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 33 || value[i] > 126 {
			return false
		}
	}
	return true
}

func (c *tokenClient) revoke(ctx context.Context, key *dpop.Key, access string) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	endpoint := strings.TrimSuffix(c.endpoint, TokenPath) + RevokePath
	nonce := ""
	for attempt := 0; attempt < 2; attempt++ {
		status, next, err := c.revokeRequest(ctx, key, endpoint, access, nonce)
		if err != nil {
			return err
		}
		if status == http.StatusNoContent {
			return nil
		}
		if status != http.StatusBadRequest || next == "" {
			return ErrUnavailable
		}
		nonce = next
	}
	return ErrProtocol
}

func (c *tokenClient) revokeRequest(ctx context.Context, key *dpop.Key, endpoint, access, nonce string) (int, string, error) {
	proof, err := key.Proof(http.MethodPost, endpoint, access, nonce)
	if err != nil {
		return 0, "", ErrProtocol
	}
	response, err := c.network.Do(ctx, NetworkRequest{Endpoint: endpoint, Proof: proof, AccessToken: access, Revoke: true})
	if err != nil {
		return 0, "", err
	}
	return response.Status, response.Nonce, nil
}
