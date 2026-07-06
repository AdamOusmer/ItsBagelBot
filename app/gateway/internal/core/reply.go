package core

import (
	"context"
	"errors"
	"time"
)

// FriendlyUpstream maps an upstream failure onto a user-facing reply message.
// negative reports whether the failure may be negatively cached: a missing
// player (400/404) stays missing for a while, but a rate-limit denial (429)
// or a key-permission problem (403) must be retried on the next request.
// An infrastructure failure returns "" and should be propagated.
func FriendlyUpstream(err error) (msg string, negative bool) {
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		return "", false
	}
	switch ue.Status {
	case 400, 404:
		if ue.Message != "" {
			return ue.Message, true
		}
		return "player not found", true
	case 403:
		// The API key lacks the permission (or is locked). A config problem,
		// not a player problem — say so, and never pin it to a player's key.
		return "stats lookup not permitted right now", false
	case 429:
		return "stats service is busy, try again in a minute", false
	}
	return "", false
}

// BuildReply is the one build function every byte-flow endpoint hands to
// CachedBytes: fetch produces the typed success reply, errReply shapes the
// endpoint's reply-with-Error for a friendly failure. Successes are stored for
// ttl, negatively cacheable failures (player not found) for negativeTTL, and
// non-cacheable friendly failures (rate limited, key permissions) answer with
// ttl zero so the next request retries. Infrastructure failures propagate.
func BuildReply(ctx context.Context, ttl, negativeTTL time.Duration, fetch func(context.Context) (any, error), errReply func(msg string) any) ([]byte, time.Duration, error) {
	v, err := fetch(ctx)
	if err != nil {
		msg, negative := FriendlyUpstream(err)
		if msg == "" {
			return nil, 0, err
		}
		b, merr := MarshalReply(errReply(msg))
		if merr != nil {
			return nil, 0, merr
		}
		if !negative {
			return b, 0, nil
		}
		return b, negativeTTL, nil
	}
	b, merr := MarshalReply(v)
	if merr != nil {
		return nil, 0, merr
	}
	return b, ttl, nil
}
