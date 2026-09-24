// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"errors"
	"time"
)

type Pin uint8

const (
	PinNone Pin = iota
	PinNegative
	PinThrottle
)

const ThrottleTTL = 20 * time.Second

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
		return "stats lookup not permitted right now", PinNone
	case 429:
		return friendly429(ue)
	}
	return "", PinNone
}

func friendly429(ue *UpstreamError) (string, Pin) {
	if ue.LocalDeny {
		return "stats commands are busy right now, try again in a few seconds", PinNone
	}
	return "stats provider is rate limiting us, try again in a minute", PinThrottle
}

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

func BuildReply(ctx context.Context, ttl, negativeTTL time.Duration, fetch func(context.Context) (any, error), errReply func(msg string) any) ([]byte, time.Duration, *UpstreamError, error) {
	return BuildReplyWithMapper(ctx, ttl, negativeTTL, fetch, errReply, FriendlyUpstream)
}

func BuildReplyWithMapper(ctx context.Context, ttl, negativeTTL time.Duration, fetch func(context.Context) (any, error), errReply func(msg string) any, mapper func(error) (string, Pin)) ([]byte, time.Duration, *UpstreamError, error) {
	v, err := fetch(ctx)
	if err != nil {
		if mapper == nil {
			mapper = FriendlyUpstream
		}
		return buildErrReply(err, negativeTTL, errReply, mapper)
	}
	b, merr := MarshalReply(v)
	if merr != nil {
		return nil, 0, nil, merr
	}
	return b, ttl, nil, nil
}

func buildErrReply(err error, negativeTTL time.Duration, errReply func(msg string) any, mapper func(error) (string, Pin)) ([]byte, time.Duration, *UpstreamError, error) {
	msg, pin := mapper(err)
	if msg == "" {
		return nil, 0, nil, err
	}
	b, merr := MarshalReply(errReply(msg))
	if merr != nil {
		return nil, 0, nil, merr
	}
	var ue *UpstreamError
	errors.As(err, &ue)
	return b, honorRetryAfter(pin, pinTTL(pin, negativeTTL), ue), ue, nil
}

func honorRetryAfter(pin Pin, pinDur time.Duration, ue *UpstreamError) time.Duration {
	if pin != PinThrottle || ue == nil {
		return pinDur
	}
	if ue.RetryAfter > pinDur {
		return ue.RetryAfter
	}
	return pinDur
}
