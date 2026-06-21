// Package invalidate provides the canonical pub-sub DTO and publish helper for
// cache invalidation events. Services publish to a subject of the form
// prefix+"."+scope (e.g. "bagel.invalidate.status") with a JSON-encoded DTO.
package invalidate

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
)

// DTO is the wire payload for a cache invalidation event.
type DTO struct {
	BroadcasterID string `json:"broadcaster_id"`
}

// Publish marshals a DTO for broadcasterID and publishes it to prefix+"."+scope.
func Publish(nc *nats.Conn, prefix, scope, broadcasterID string) error {
	body, err := json.Marshal(DTO{BroadcasterID: broadcasterID})
	if err != nil {
		return err
	}
	return nc.Publish(prefix+"."+scope, body)
}
