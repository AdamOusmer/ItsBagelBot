package core

import (
	"context"
	"errors"
	"time"
)

// Pin classifies how long a friendly upstream failure may be cached.
type Pin uint8

const (
	// PinNone must retry on the very next request: our own bucket denial (a
	// token refills within seconds and retrying costs no upstream call) and
	// key-permission problems (fixed out of band, must heal instantly).
	PinNone Pin = iota
	// PinNegative is a stable absence (missing player): cached for the
	// endpoint's negativeTTL.
	PinNegative
	// PinThrottle is an upstream throttle (the provider or its CDN answered
	// 429): cached briefly so a chat burst answers from cache instead of
	// re-hitting an upstream that is already telling us to back off —
	// hammering it just keeps the throttle window open.
	PinThrottle
)

// ThrottleTTL is the PinThrottle cool-down. Long enough to absorb a command
// burst, short enough that recovery is never minutes away.
const ThrottleTTL = 20 * time.Second

// FriendlyUpstream maps an upstream failure onto a user-facing reply message
// and its Pin class. An infrastructure failure returns "" and should be
// propagated.
func FriendlyUpstream(err error) (msg string, pin Pin) {
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		return "", PinNone
	}
	switch ue.Status {
	case 400, 404:
		if ue.Message != "" {
			return ue.Message, PinNegative
		}
		return "player not found", PinNegative
	case 403:
		// The API key lacks the permission (or is locked). A config problem,
		// not a player problem — say so, and never pin it to a player's key.
		return "stats lookup not permitted right now", PinNone
	case 429:
		return friendly429(ue)
	}
	return "", PinNone
}

// friendly429 tells the two rate-limit origins apart. They demand different
// texts and different caching: our own bucket refills in seconds and denies
// before any upstream call, so retrying is free; an upstream 429 means the
// provider (or Cloudflare in front of it) is throttling our address, and only
// backing off ends that.
func friendly429(ue *UpstreamError) (string, Pin) {
	if ue.LocalDeny {
		return "stats commands are busy right now, try again in a few seconds", PinNone
	}
	return "stats provider is rate limiting us, try again in a minute", PinThrottle
}

// pinTTL maps a Pin class onto the store TTL for one endpoint.
func pinTTL(pin Pin, negativeTTL time.Duration) time.Duration {
	switch pin {
	case PinNegative:
		return negativeTTL
	case PinThrottle:
		return ThrottleTTL
	default:
		return 0
	}
}

// BuildReply is the one build function every byte-flow endpoint hands to
// CachedBytes: fetch produces the typed success reply, errReply shapes the
// endpoint's reply-with-Error for a friendly failure. Successes are stored for
// ttl and friendly failures for their Pin class's TTL (zero answers without
// caching, so the next request retries). Infrastructure failures propagate.
//
// friendly reports the upstream failure the reply was shaped from (nil for a
// success), so the caller can log what the upstream actually said — the reply
// itself deliberately carries only the friendly text.
func BuildReply(ctx context.Context, ttl, negativeTTL time.Duration, fetch func(context.Context) (any, error), errReply func(msg string) any) ([]byte, time.Duration, *UpstreamError, error) {
	v, err := fetch(ctx)
	if err != nil {
		return buildErrReply(err, negativeTTL, errReply)
	}
	b, merr := MarshalReply(v)
	if merr != nil {
		return nil, 0, nil, merr
	}
	return b, ttl, nil, nil
}

// buildErrReply shapes one fetch failure: a friendly failure becomes the
// endpoint's error reply with its Pin TTL, anything else propagates.
func buildErrReply(err error, negativeTTL time.Duration, errReply func(msg string) any) ([]byte, time.Duration, *UpstreamError, error) {
	msg, pin := FriendlyUpstream(err)
	if msg == "" {
		return nil, 0, nil, err
	}
	b, merr := MarshalReply(errReply(msg))
	if merr != nil {
		return nil, 0, nil, merr
	}
	var ue *UpstreamError
	errors.As(err, &ue)
	return b, pinTTL(pin, negativeTTL), ue, nil
}
