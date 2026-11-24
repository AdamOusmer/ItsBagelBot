// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"errors"
	"io"
	"net/http"

	"ItsBagelBot/pkg/codec"
)

const validateEndpoint = "https://id.twitch.tv/oauth2/validate"

func (c *Client) ValidateScopes(ctx context.Context, broadcasterID string, token StoredToken) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, validateEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth "+token.AccessToken)
	res, err := c.performAttempt(ctx, req, req.URL.Path)
	if err != nil {
		return nil, err
	}
	defer drain(res)
	if res.StatusCode != http.StatusOK {
		return nil, &StatusError{Status: res.StatusCode, Op: "validate token"}
	}

	var grant struct {
		UserID string   `json:"user_id"`
		Scopes []string `json:"scopes"`
	}
	if err := codec.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&grant); err != nil {
		return nil, err
	}
	if grant.UserID != broadcasterID {
		return nil, errors.New("validated token does not belong to the broadcaster")
	}
	return grant.Scopes, nil
}
