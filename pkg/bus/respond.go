// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

// Respond marshals v as JSON and sends it as the reply to msg. Existing
// handlers may ignore the returned error, but new shared RPC helpers log it so
// responder-side failures do not disappear silently.
func Respond(msg *nats.Msg, v any) error {
	body, err := marshalResponse(v)
	if err != nil {
		return err
	}
	return sendResponse(msg, body)
}

func marshalResponse(v any) ([]byte, error) { return codec.Marshal(v) }

func sendResponse(msg *nats.Msg, body []byte) error { return msg.Respond(body) }
