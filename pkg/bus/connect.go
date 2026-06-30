package bus

import (
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/pkg/env"
)

// The fleet runs two NATS planes on two credential sets:
//
//   - the RPC + cache-invalidate plane (core request/reply), authenticated with
//     the service's own per-service account via NATS_RPC_USER / NATS_RPC_PASSWORD;
//   - the durable JetStream event plane, authenticated with the shared BUS
//     account via NATS_USER / NATS_PASSWORD.
//
// Both planes prefer the node-local leaf and fall back to the hub. Leaf
// priority is enforced in code (ordered server list + DontRandomize), not just
// by which URL a manifest happens to set, so a reconnect always retries the
// leaf first before spilling to the hub.

// JSDomain is the JetStream domain the fleet's streams live in. Clients dial the
// leaf (whose own JetStream domain is "leaf"), so every JetStream context must be
// domain-qualified to reach the authoritative hub streams.
func JSDomain() string { return env.Get("NATS_JS_DOMAIN", env.Get("NODE_NAME", "")) }

// serverList returns the ordered NATS endpoint list, leaf first then hub, as the
// comma-joined string nats.Connect parses into an ordered server pool. override
// (the url a caller passes through, e.g. NATS_URL / NATS_RPC_URL) is used as the
// leaf endpoint when the explicit NATS_LEAF_URL/NATS_HUB_URL split is absent, so
// local development and pre-migration manifests keep working against a single
// server.
func serverList(override string) string {
	leaf := env.Get("NATS_LEAF_URL", "")
	hub := env.Get("NATS_HUB_URL", "")

	// No leaf/hub split configured: honor whatever single endpoint the caller
	// passed (local dev points this at 127.0.0.1).
	if leaf == "" && hub == "" {
		return override
	}
	if leaf == "" {
		leaf = override
	}
	if hub == "" || hub == leaf {
		return leaf
	}
	return leaf + "," + hub
}

// baseOptions are shared by every connection the fleet opens, core or
// JetStream: endless reconnects, an ordered (leaf-first) server pool that is
// never shuffled, a client name for monitoring, and the supplied credentials.
// Local development runs against an open server, so empty credentials are fine;
// the broker is the one enforcing them.
func baseOptions(name, user, pass string) []nats.Option {
	opts := []nats.Option{
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectBufSize(8 * 1024 * 1024), // 8 MB buffer so publishes during a reconnect are not lost
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.Timeout(15 * time.Second),
		nats.RetryOnFailedConnect(true),
		// Honor serverList order on the initial dial and on every reconnect; the
		// default shuffles the pool and would let a client pin the hub.
		nats.DontRandomize(),
	}

	if name != "" {
		opts = append(opts, nats.Name(name))
	}

	if user != "" {
		opts = append(opts, nats.UserInfo(user, pass))
	}

	// Kubernetes may temporarily place this connection on a fallback leaf. Once
	// the same-node leaf is stably healthy, recycle only that displaced
	// connection so topology-aware Service routing can return it locally.
	if option := leafFailbackOption(); option != nil {
		opts = append(opts, option)
	}

	return opts
}

// rpcOptions authenticate the per-service account on the RPC plane. The creds
// fall back to NATS_USER/NATS_PASSWORD so local dev and the phased rollout (RPC
// creds not yet provisioned) keep working against the shared user.
func rpcOptions(name string) []nats.Option {
	user := env.Get("NATS_RPC_USER", env.Get("NATS_USER", ""))
	pass := env.Get("NATS_RPC_PASSWORD", env.Get("NATS_PASSWORD", ""))
	return baseOptions(name, user, pass)
}

// busOptions authenticate the shared BUS account on the JetStream plane.
func busOptions(name string) []nats.Option {
	return baseOptions(name, env.Get("NATS_USER", ""), env.Get("NATS_PASSWORD", ""))
}

// Connect opens a core NATS connection for request-reply RPC and ephemeral
// subscriptions on the per-service account, leaf-first. name identifies the
// service in NATS monitoring.
func Connect(url string, name string) (*nats.Conn, error) {
	return nats.Connect(serverList(url), rpcOptions(name)...)
}

// jsDomainOption is the JetStream connect option that targets the hub domain
// over the leaf link. Exposed as a slice so callers can splice it into a
// JetStreamConfig.ConnectOptions / nc.JetStream call.
func jsDomainOption() []nats.JSOpt {
	return []nats.JSOpt{nats.Domain(JSDomain())}
}

// RPCURL returns the core NATS endpoint used for request/reply traffic. It
// intentionally falls back to the durable bus URL so local development and old
// deployments keep working, while production can point RPC at a node-local leaf
// without moving JetStream publisher/subscriber traffic.
func RPCURL(busURL string) string {
	return env.Get("NATS_RPC_URL", busURL)
}

// busURL resolves the JetStream-plane endpoint list (leaf-first) from the url a
// caller threads through (typically NATS_URL).
func busURL(url string) string {
	return serverList(url)
}
