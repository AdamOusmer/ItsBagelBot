// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrNoRefreshToken = errors.New("no refresh token available")

type TokenError struct {
	Status int
	Body   string
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("token request failed: %d %s", e.Status, e.Body)
}

func (e *TokenError) PermanentAuth() bool {
	switch e.Status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return true
	default:
		return false
	}
}

func GrantDead(err error) bool {
	if errors.Is(err, ErrNoUserToken) || errors.Is(err, ErrNoRefreshToken) {
		return true
	}

	var te *TokenError
	if errors.As(err, &te) {
		return te.PermanentAuth()
	}
	return false
}
