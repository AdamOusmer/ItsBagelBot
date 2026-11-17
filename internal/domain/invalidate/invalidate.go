// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package invalidate

import (
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

type DTO struct {
	BroadcasterID string   `json:"broadcaster_id"`
	Keys          []string `json:"keys,omitempty"`
}

func PublishEvent(nc *nats.Conn, subject string, payload any) error {
	body, err := codec.Marshal(payload)
	if err != nil {
		return err
	}
	return nc.Publish(subject, body)
}
