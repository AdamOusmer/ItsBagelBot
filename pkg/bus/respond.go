package bus

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
)

// Respond marshals v as JSON and sends it as the reply to msg. Existing
// handlers may ignore the returned error, but new shared RPC helpers log it so
// responder-side failures do not disappear silently.
func Respond(msg *nats.Msg, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return msg.Respond(body)
}
