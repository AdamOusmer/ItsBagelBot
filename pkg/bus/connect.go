package bus

import (
	"crypto/tls"
	"crypto/x509"
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
// The RPC + cache plane prefers the node-local leaf: an ordered server list plus
// DontRandomize means a reconnect always retries the leaf first before spilling
// to the hub. The JetStream plane instead dials the hub directly (busURL reads
// NATS_HUB_URL): the durable streams live on the hub, so routing JetStream
// through the leaf is only an extra forwarding hop (the leaf runs no JetStream).
// This mirrors the console lib's rpc/bus split in
// console/shared/lib/server/nats.ts.

// JSDomain is the JetStream domain the fleet's streams live in. Clients dial the
// leaf (whose own JetStream domain is "leaf"), so every JetStream context must be
// domain-qualified to reach the authoritative hub streams.
func JSDomain() string { return env.Get("NATS_JS_DOMAIN", "hub") }

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
		// 32 MB buffer so publishes during a reconnect are not lost. Raised from
		// 8 MB for the 150k firehose: a hub roll (R1 memory stream) can briefly
		// disconnect the async publisher, and at 150k/s an 8 MB buffer fills in
		// well under a second, dropping events the dedup window can't recover.
		nats.ReconnectBufSize(32 * 1024 * 1024),
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.Timeout(15 * time.Second),
		nats.RetryOnFailedConnect(true),
		// Honor serverList order on the initial dial and on every reconnect; the
		// default shuffles the pool and would let a client pin the hub.
		nats.DontRandomize(),
		// Keep reconnecting through authorization errors. The default aborts the
		// connection for good after two consecutive auth failures against the
		// same server, which permanently strands a pod when the broker's account
		// env lags a credential rotation (readyz stays 503 but healthz keeps the
		// container alive, so it never restarts on its own).
		nats.IgnoreAuthErrorAbort(),
	}

	// Verify the broker's TLS server cert against the fleet CA when one is
	// configured (see tlsSecureOption), the wire encryption now that NATS is out of
	// the Linkerd mesh.
	if option := tlsSecureOption(); option != nil {
		opts = append(opts, option)
	}

	if name != "" {
		opts = append(opts, nats.Name(name))
	}

	if user != "" {
		opts = append(opts, nats.UserInfo(user, pass))
	}

	return opts
}

// tlsSecureOption returns a nats.Secure option that verifies the broker's server
// cert against the fleet CA (NATS_CA_PEM, distributed by trust-manager as the
// fleet-ca ConfigMap), or nil when no CA is configured — local dev against a
// plaintext server stays plaintext. Server-auth only: the client still
// authenticates with its bcrypt user/password, not a client cert.
func tlsSecureOption() nats.Option {
	caPEM := env.Get("NATS_CA_PEM", "")
	if caPEM == "" {
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil
	}
	return nats.Secure(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
}

// rpcOptions authenticate the per-service account on the RPC plane. The creds
// fall back to NATS_USER/NATS_PASSWORD so local dev and the phased rollout (RPC
// creds not yet provisioned) keep working against the shared user.
func rpcOptions(name string) []nats.Option {
	user := env.Get("NATS_RPC_USER", env.Get("NATS_USER", ""))
	pass := env.Get("NATS_RPC_PASSWORD", env.Get("NATS_PASSWORD", ""))
	opts := baseOptions(name, user, pass)
	// Leaf failback applies ONLY to the RPC plane, which is leaf-first: recycle a
	// connection displaced onto a fallback leaf back to the node-local leaf once it
	// recovers. The BUS plane (busOptions) dials the hub directly (NATS_HUB_URL),
	// whose server_name is "nats-N", never "<node>--…" — failback would treat it as
	// permanently displaced and ForceReconnect it every interval (~90s), churning
	// the JetStream consumers. That is what broke outgress -> Twitch delivery after
	// the BUS plane moved hub-direct.
	if option := leafFailbackOption(); option != nil {
		opts = append(opts, option)
	}
	return opts
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

// busURL resolves the JetStream-plane endpoint. The durable streams live on the
// hub, so for JetStream the node-local leaf is only an extra forwarding hop:
// dial the hub directly when NATS_HUB_URL is set (mirroring busServerList in
// console/shared/lib/server/nats.ts). Falls back to the leaf-first serverList
// when no hub is configured (local dev / single-endpoint deploys). RPC stays
// leaf-first via RPCURL/serverList.
func busURL(url string) string {
	if hub := env.Get("NATS_HUB_URL", ""); hub != "" {
		return hub
	}
	return serverList(url)
}
