// Package rpc reads the ingress shard state over NATS request-reply,
// mirroring the contract of Ingress.AdminRpc. The admin tool never talks to
// the database or to Twitch; the ingress fleet is its single source of truth.
package rpc

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
)

// Snapshot is the JSON document Ingress.AdminRpc replies with. Any ingress
// replica may answer; Reporter says which one did.
type Snapshot struct {
	GeneratedAt    time.Time `json:"generated_at"`
	Reporter       string    `json:"reporter"`
	Nodes          []string  `json:"nodes"`
	ShardCount     int       `json:"shard_count"`
	ConduitManager Manager   `json:"conduit_manager"`
	Shards         []Shard   `json:"shards"`
}

// Manager describes the cluster-singleton conduit reconciler.
type Manager struct {
	State     string `json:"state"`
	Node      string `json:"node"`
	ConduitID string `json:"conduit_id"`
}

// Shard is one EventSub WebSocket shard. State is one of: connected,
// migrating, binding, connecting, backoff, unregistered, unresponsive.
type Shard struct {
	ShardID           int        `json:"shard_id"`
	State             string     `json:"state"`
	Node              string     `json:"node"`
	SessionID         string     `json:"session_id"`
	Bound             bool       `json:"bound"`
	HandshakeInFlight bool       `json:"handshake_in_flight"`
	KeepaliveMs       int        `json:"keepalive_ms"`
	Attempts          int        `json:"attempts"`
	BoundAt           *time.Time `json:"bound_at"`
	LastFrameAt       *time.Time `json:"last_frame_at"`
}

type Ingress struct {
	nc      *nats.Conn
	subject string
}

func NewIngress(nc *nats.Conn, subject string) *Ingress {
	return &Ingress{nc: nc, subject: subject}
}

// Shards asks the ingress fleet for a live snapshot. The ingress side caps
// per-shard calls at 2s, so 5s comfortably covers a full sweep.
func (i *Ingress) Shards() (*Snapshot, error) {
	msg, err := i.nc.Request(i.subject, nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(msg.Data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
