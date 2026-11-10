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

func Publish(nc *nats.Conn, prefix, scope, broadcasterID string) error {
	return PublishKeys(nc, prefix, scope, broadcasterID)
}

func PublishKeys(nc *nats.Conn, prefix, scope, broadcasterID string, keys ...string) error {
	return PublishEvent(nc, prefix+"."+scope, DTO{BroadcasterID: broadcasterID, Keys: keys})
}

func PublishEvent(nc *nats.Conn, subject string, payload any) error {
	body, err := codec.Marshal(payload)
	if err != nil {
		return err
	}
	return nc.Publish(subject, body)
}
