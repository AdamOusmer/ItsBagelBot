// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

func Respond(msg *nats.Msg, v any) error {
	body, err := marshalResponse(v)
	if err != nil {
		return err
	}
	return msg.Respond(body)
}

func marshalResponse(v any) ([]byte, error) { return codec.FastMarshal(v) }

func sendResponse(msg *nats.Msg, body []byte) error { return msg.Respond(body) }
