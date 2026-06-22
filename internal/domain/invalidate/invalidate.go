// Package invalidate provides the canonical pub-sub DTO and publish helper for
// cache invalidation events. Services publish to a subject of the form
// prefix+"."+scope (e.g. "bagel.invalidate.status") with a JSON-encoded DTO.
package invalidate

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
)

// DTO is the wire payload for a cache invalidation event. Keys is an optional,
// scope-specific list of granular identifiers (e.g. the command name and its
// aliases) so a subscriber can evict exactly the affected entries instead of a
// whole section. An empty Keys means "the whole scope for this broadcaster".
type DTO struct {
	BroadcasterID string   `json:"broadcaster_id"`
	Keys          []string `json:"keys,omitempty"`
}

// Publish marshals a DTO for broadcasterID and publishes it to prefix+"."+scope.
func Publish(nc *nats.Conn, prefix, scope, broadcasterID string) error {
	return PublishKeys(nc, prefix, scope, broadcasterID)
}

// PublishKeys is Publish with a granular key list. Subscribers that understand
// Keys evict only those entries; others fall back to TTL or whole-scope drop.
func PublishKeys(nc *nats.Conn, prefix, scope, broadcasterID string, keys ...string) error {
	body, err := json.Marshal(DTO{BroadcasterID: broadcasterID, Keys: keys})
	if err != nil {
		return err
	}
	return nc.Publish(prefix+"."+scope, body)
}
