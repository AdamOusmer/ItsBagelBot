// Package rpc reads cross-service data over NATS request-reply, mirroring the
// contract the Elixir ingress uses. The dashboard never opens another
// service's schema.
package rpc

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
)

type Broadcaster struct {
	nc      *nats.Conn
	subject string
}

func NewBroadcaster(nc *nats.Conn, subject string) *Broadcaster {
	return &Broadcaster{nc: nc, subject: subject}
}

// Tier returns "premium" or "standard". Unknown broadcasters and RPC failures
// degrade to "standard", matching the ingress behavior.
func (b *Broadcaster) Tier(broadcasterID string) string {
	req, _ := json.Marshal(map[string]string{"broadcaster_id": broadcasterID})
	msg, err := b.nc.Request(b.subject, req, 2*time.Second)
	if err != nil {
		return "standard"
	}
	var reply struct {
		Tier string `json:"tier"`
	}
	if json.Unmarshal(msg.Data, &reply) == nil && reply.Tier == "premium" {
		return "premium"
	}
	return "standard"
}
