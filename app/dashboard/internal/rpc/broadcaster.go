// Package rpc reads cross-service data over NATS request-reply, mirroring the
// contract the Elixir ingress uses. The dashboard never opens another
// service's schema.
package rpc

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// tierEntry is a cached result from one Tier() RPC call.
type tierEntry struct {
	value string
	until time.Time
}

type Broadcaster struct {
	nc      *nats.Conn
	subject string

	// cacheMu guards tierCache. Tier() is called on every page render for
	// each broadcaster visible on the page, so a short TTL cache (7s) prevents
	// repeated 2s-blocking RPCs for the same broadcaster within a render burst.
	cacheMu   sync.Mutex
	tierCache map[string]tierEntry
}

func NewBroadcaster(nc *nats.Conn, subject string) *Broadcaster {
	return &Broadcaster{
		nc:        nc,
		subject:   subject,
		tierCache: make(map[string]tierEntry),
	}
}

// Tier returns "premium" or "standard". Unknown broadcasters and RPC failures
// degrade to "standard", matching the ingress behavior.
// Results are cached per broadcaster ID for 7 seconds to avoid a blocking
// NATS RPC on every render fragment.
func (b *Broadcaster) Tier(broadcasterID string) string {
	b.cacheMu.Lock()
	if e, ok := b.tierCache[broadcasterID]; ok && time.Now().Before(e.until) {
		v := e.value
		b.cacheMu.Unlock()
		return v
	}
	b.cacheMu.Unlock()

	// RPC outside the lock so callers for different broadcaster IDs run
	// concurrently. Two concurrent callers for the same ID may both issue an
	// RPC; that is acceptable — they will both store identical results.
	req, _ := json.Marshal(map[string]string{"broadcaster_id": broadcasterID})
	msg, err := b.nc.Request(b.subject, req, 2*time.Second)

	tier := "standard"
	if err == nil {
		var reply struct {
			Tier string `json:"tier"`
		}
		if json.Unmarshal(msg.Data, &reply) == nil && reply.Tier == "premium" {
			tier = "premium"
		}
	}

	b.cacheMu.Lock()
	b.tierCache[broadcasterID] = tierEntry{value: tier, until: time.Now().Add(7 * time.Second)}
	b.cacheMu.Unlock()

	return tier
}
