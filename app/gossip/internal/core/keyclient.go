// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

// keyRPCTimeout bounds every internal secret-resolver lookup gossip makes
// (Govee keys, Spotify credentials and their rotation, custom urlfetch keys).
// It is short and shared because the reasoning is the same for all of them:
// the answering service reads its own database plus one Unpack with no
// upstream hop, and the caller's own handler budget (a 3s endpoint budget for
// urlfetch, a generous redemption budget for a color reward) carries the rest
// — the key resolve must never be the tail that eats it. Three separate 2s
// constants said this three times; a per-RPC value can come back the day one
// of these grows an upstream hop, with a measurement to justify it.
const keyRPCTimeout = 2 * time.Second

// KeyClient is the one internal secret-resolver RPC shape gossip's providers
// share: one request/reply round trip on a fixed subject, a fixed timeout, and
// the reply's in-band Error field turned into a Go error. The three resolvers
// it backs were byte-identical apart from their wire types and the noun in
// their error strings.
//
// Rejected: a shared reply interface with an Err() method. The reply types
// live in internal/domain/rpc/* and are plain wire DTOs; a Go constraint
// cannot require a FIELD, so satisfying one would mean adding a method to
// three contract packages purely to serve this caller. The one-line accessor
// stays here instead.
type KeyClient[Req, Rep any] struct {
	nc      *nats.Conn
	subject string
	// label is the noun both error strings are built from: "<label> rpc: %w"
	// for a transport failure, "<label>: %s" for one the service reported.
	label    string
	replyErr func(Rep) string
}

// keyClientConfig wires one KeyClient. A struct rather than four positional
// arguments: two of them are strings that would read identically at the call
// site if swapped, and a silently swapped subject/label pair produces working
// code that reports the wrong service in every error it logs.
type keyClientConfig[Rep any] struct {
	NC       *nats.Conn
	Subject  string
	Label    string
	ReplyErr func(Rep) string
}

func newKeyClient[Req, Rep any](cfg keyClientConfig[Rep]) *KeyClient[Req, Rep] {
	return &KeyClient[Req, Rep]{nc: cfg.NC, subject: cfg.Subject, label: cfg.Label, replyErr: cfg.ReplyErr}
}

// Call performs the round trip. A transport failure and a service-reported
// failure are both errors; the caller never sees a half-answer, so a reply
// carrying an in-band error comes back as the zero value.
func (c *KeyClient[Req, Rep]) Call(ctx context.Context, req Req) (Rep, error) {
	var zero Rep
	reply, err := bus.RequestJSONTimeout[Rep](ctx, c.nc, c.subject, req, keyRPCTimeout)
	if err != nil {
		return zero, fmt.Errorf("%s rpc: %w", c.label, err)
	}
	if msg := c.replyErr(reply); msg != "" {
		return zero, fmt.Errorf("%s: %s", c.label, msg)
	}
	return reply, nil
}
