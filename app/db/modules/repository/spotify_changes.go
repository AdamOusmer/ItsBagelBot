// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strings"
)

func (s *SpotifyCreds) tokenChanges(ctx context.Context, id SpotifyUserID, grant SpotifyGrant) (bool, error) {
	row, err := s.tokenRow(ctx, id)
	if errors.Is(err, ErrNoSpotifyToken) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	prior, err := s.tokenFromRow(row)
	if err != nil {
		return true, nil
	}
	scopesChanged := grant.Scopes != nil && row.Scopes != strings.Join(grant.Scopes, " ")
	return prior != grant.RefreshToken || row.RefreshFailedAt != nil || scopesChanged, nil
}

func (s *SpotifyCreds) appChanges(ctx context.Context, id SpotifyUserID, app SpotifyApp) (bool, error) {
	row, err := s.tokenRow(ctx, id)
	if errors.Is(err, ErrNoSpotifyToken) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	prior, err := s.appFromRow(row)
	return prior != app || err != nil, nil
}
