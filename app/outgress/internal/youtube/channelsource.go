// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package youtube

import (
	"context"
	"errors"
)

// Client's TokenSource is channel-agnostic (one Token(ctx) per call), but the
// users service leases credentials PER CHANNEL. The bridge is the context: a
// handler stamps the acting broadcaster onto ctx via WithChannel, and
// LeasedSource turns that into the right lease at mint time. Keeping the key
// here (unexported, constructed only through WithChannel) means no other
// layer can spoof or misread the acting channel.

type channelKey struct{}

// WithChannel records the YouTube channel id the surrounding API calls act
// as. Handlers call it once before any client method; without it LeasedSource
// refuses rather than guessing a credential.
func WithChannel(ctx context.Context, channelID string) context.Context {
	return context.WithValue(ctx, channelKey{}, channelID)
}

func channelFromContext(ctx context.Context) string {
	id, _ := ctx.Value(channelKey{}).(string)
	return id
}

// ErrNoChannelInContext is returned by LeasedSource when a call reached the
// client without an acting channel stamped on its context: a wiring bug (a
// handler that skipped WithChannel), never a runtime condition, so failing
// loud beats silently sending under the wrong project credential.
var ErrNoChannelInContext = errors.New("youtube: no acting channel in context for leased token")

// LeasedSource adapts per-channel LeasedTokens to Client's channel-agnostic
// TokenSource.
//
// It deliberately does not implement TokenInvalidator: Invalidate has no
// channel argument, so a 401 could only drop every cached lease fleet-wide.
// Instead a rejected send fails as ErrAuth and drops; the next action for
// that channel leases afresh once past refreshMargin, and the users-service
// side heals the lease when it re-mints.
type LeasedSource struct {
	leased *LeasedTokens
}

func NewLeasedSource(leased *LeasedTokens) *LeasedSource {
	return &LeasedSource{leased: leased}
}

func (s *LeasedSource) Token(ctx context.Context) (string, error) {
	channelID := channelFromContext(ctx)
	if channelID == "" {
		return "", ErrNoChannelInContext
	}
	return s.leased.Token(ctx, channelID)
}
