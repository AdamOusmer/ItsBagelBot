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

const keyRPCTimeout = 2 * time.Second

// Replies carry plaintext secrets: never cache, log or project them.
type KeyClient[Req, Rep any] struct {
	nc       *nats.Conn
	subject  string
	label    string
	replyErr func(Rep) string
}

type keyClientConfig[Rep any] struct {
	NC       *nats.Conn
	Subject  string
	Label    string
	ReplyErr func(Rep) string
}

func newKeyClient[Req, Rep any](cfg keyClientConfig[Rep]) *KeyClient[Req, Rep] {
	return &KeyClient[Req, Rep]{nc: cfg.NC, subject: cfg.Subject, label: cfg.Label, replyErr: cfg.ReplyErr}
}

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
